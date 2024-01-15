package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
)

type Rule struct {
	Regexp      *regexp.Regexp `json:"regexp"`
	Replacement string         `json:"replacement"`
	Code        int            `json:"code"`
	LogHits     bool           `json:"log_hits"`
}

type Domain struct {
	RewriteRules    []Rule           `json:"rewrites"`
	DefaultResponse *DefaultResponse `json:"default_response"`
	MatchSubdomains bool             `json:"match_subdomains"`
}

type DefaultResponse struct {
	Code    int               `json:"code"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
	LogHits bool              `json:"log_hits"`
}

type Config struct {
	ListenAddress   string            `json:"listen_address"`
	DefaultResponse *DefaultResponse  `json:"default_response"`
	Domains         map[string]Domain `json:"domains" note:"Keys must be valid fully qualified DNS domain names in ASCII lower case and punycode if required."`
}

var config Config = Config{
	ListenAddress: ":8000",
	DefaultResponse: &DefaultResponse{
		Code: http.StatusMisdirectedRequest,
		Headers: map[string]string{
			"Connection":   "close",
			"Content-Type": "text/plain",
		},
		Body: "421 Misdirected Request\n\nTarget URI does not match an origin for which the server has been configured.\n",
	},
	Domains: map[string]Domain{},
}

func (r *Rule) UnmarshalJSON(data []byte) error {
	var temp struct {
		Regexp      string `json:"regexp"`
		Replacement string `json:"replacement"`
		Code        int    `json:"code"`
		LogHits     bool   `json:"log_hits"`
	}

	err := json.Unmarshal(data, &temp)
	if err != nil {
		return err
	}

	if r.Regexp, err = regexp.Compile(temp.Regexp); err != nil {
		return err
	}

	r.Replacement = temp.Replacement
	r.Code = temp.Code
	r.LogHits = temp.LogHits

	return nil
}

func main() {
	configFile := flag.String("c", "config.json", "path to the config file")
	flag.Parse()

	loadConfig(*configFile)

	http.HandleFunc("/", redirectHandler)

	log.Printf("Listening on %s", config.ListenAddress)
	log.Fatal(http.ListenAndServe(config.ListenAddress, nil))
}

func loadConfig(filename string) {
	log.Printf("Loading config from %s", filename)
	file, err := os.Open(filename)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()

	err = decoder.Decode(&config)
	if err != nil {
		log.Fatal(err)
	}

	domainRegex := regexp.MustCompile(`^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?::\d+)?$`)
	replacementRegex := regexp.MustCompile(`\$(\w+)`)

	var problems []string

	for origin, domain := range config.Domains {
		if !domainRegex.MatchString(origin) {
			problems = append(problems, fmt.Sprintf("Invalid domain %s. Keys must be valid fully qualified DNS domain names in ASCII lowercase (in punycode if required), optionally including a port number.", origin))
		}

		for _, rewriteRule := range domain.RewriteRules {
			if rewriteRule.Code < 300 || rewriteRule.Code > 399 {
				problems = append(problems, fmt.Sprintf("Invalid redirect code for domain %s. Code must be between 300 and 399 inclusive.", origin))
			}

			if !strings.HasPrefix(rewriteRule.Replacement, "http://") && !strings.HasPrefix(rewriteRule.Replacement, "https://") {
				problems = append(problems, fmt.Sprintf("Invalid replacement for domain %s. Destination must begin with 'http://' or 'https://'.", origin))
			}

			// Drop all "$$" so we're only matching things that aren't literal "$"s in the replacement string
			matches := replacementRegex.FindAllString(strings.ReplaceAll(rewriteRule.Replacement, "$$", ""), -1)
			for _, match := range matches {
				if replacement, err := strconv.ParseInt(match[1:], 10, 0); err != nil {
					problems = append(problems, fmt.Sprintf("Invalid replacement '%s' (only numbered replacements are supported): %v", rewriteRule.Replacement, err))
				} else if int(replacement) < 0 || int(replacement) > rewriteRule.Regexp.NumSubexp() {
					problems = append(problems, fmt.Sprintf("Invalid replacement '%s': replacement group $%d does not exist", rewriteRule.Replacement, replacement))
				}
			}
		}
	}

	if config.DefaultResponse.Code != 0 && (config.DefaultResponse.Code < 200 || config.DefaultResponse.Code > 599) {
		problems = append(problems, fmt.Sprintf("Invalid default response code %d. Code must be between 200 and 599 inclusive, or 0 to close the connection immediately.", config.DefaultResponse.Code))
	}

	if len(problems) > 0 {
		for _, problem := range problems {
			log.Println(problem)
		}
		log.Fatal("Configuration contains errors. Please fix the problems and try again.")
	}
}

func redirectHandler(w http.ResponseWriter, r *http.Request) {
	defaultResponse := config.DefaultResponse

	for origin, domain := range config.Domains {
		if strings.EqualFold(r.Host, origin) || (domain.MatchSubdomains && strings.HasSuffix(strings.ToLower(r.Host), "."+origin)) {
			for _, rule := range domain.RewriteRules {
				if !rule.Regexp.MatchString(r.RequestURI) {
					continue
				}
				if rule.LogHits {
					log.Printf("%s %s %s %s", r.RemoteAddr, r.Method, r.Host, r.RequestURI)
				}
				http.Redirect(w, r, rule.Regexp.ReplaceAllString(r.RequestURI, rule.Replacement), rule.Code)
				return
			}

			if domain.DefaultResponse != nil {
				defaultResponse = domain.DefaultResponse
			}
			break
		}
	}

	if defaultResponse.LogHits {
		log.Printf("%s %s %s %s", r.RemoteAddr, r.Method, r.Host, r.RequestURI)
	}

	if defaultResponse.Code == 0 {
		conn, _, err := http.NewResponseController(w).Hijack()
		if err == nil && conn != nil {
			conn.Close()
			return
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
	} else {
		w.WriteHeader(defaultResponse.Code)
	}

	for header, value := range defaultResponse.Headers {
		w.Header().Add(header, value)
	}
	w.Write([]byte(defaultResponse.Body))
}
