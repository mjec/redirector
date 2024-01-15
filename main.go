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
	Regexp      *regexp.Regexp
	Replacement string
	Code        int
}

type Config struct {
	RewriteRules  map[string][]Rule `json:"rewrites"`
	ListenAddress string            `json:"listen_address"`
}

var config Config

func (r *Rule) UnmarshalJSON(data []byte) error {
	var temp struct {
		Regexp      string `json:"regexp"`
		Replacement string `json:"replacement"`
		Code        int    `json:"code"`
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

	return nil
}

func main() {
	configFile := flag.String("c", "config.json", "path to the config file")
	flag.Parse()

	loadConfig(*configFile)

	http.HandleFunc("/", redirectHandler)
	log.Fatal(http.ListenAndServe(config.ListenAddress, nil))
}

func loadConfig(filename string) {
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

	domainRegex := regexp.MustCompile(`^(?:[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)
	replacementRegex := regexp.MustCompile(`\$(\d+)`)

	var problems []string

	for origin, domains := range config.RewriteRules {
		if !domainRegex.MatchString(origin) {
			problems = append(problems, fmt.Sprintf("Invalid domain %s. Keys must be valid fully qualified DNS domain names.", origin))
		}

		for _, rewriteRule := range domains {
			if rewriteRule.Code < 300 || rewriteRule.Code > 399 {
				problems = append(problems, fmt.Sprintf("Invalid redirect code for domain %s. Code must be between 300 and 399 inclusive.", origin))
			}

			if !strings.HasPrefix(rewriteRule.Replacement, "http://") && !strings.HasPrefix(rewriteRule.Replacement, "https://") {
				problems = append(problems, fmt.Sprintf("Invalid replacement for domain %s. Destination must begin with 'http://' or 'https://'.", origin))
			}

			matches := replacementRegex.FindAllString(strings.ReplaceAll(rewriteRule.Replacement, "$$", ""), -1)
			for _, match := range matches {
				if replacement, err := strconv.ParseInt(match, 10, 0); err != nil {
					problems = append(problems, fmt.Sprintf("Invalid replacement '%s' (only numbered replacements are supported): %v", rewriteRule.Replacement, err))
				} else if int(replacement) < 0 || int(replacement) > rewriteRule.Regexp.NumSubexp() {
					problems = append(problems, fmt.Sprintf("Invalid replacement '%s': replacement group $%d does not exist", rewriteRule.Replacement, replacement))
				}
			}

		}
	}

	if len(problems) > 0 {
		for _, problem := range problems {
			log.Println(problem)
		}
		log.Fatal("Configuration contains errors. Please fix the problems and try again.")
	}
}

func redirectHandler(w http.ResponseWriter, r *http.Request) {
	for origin, domains := range config.RewriteRules {
		if r.Host == origin {
			for _, domain := range domains {
				if !domain.Regexp.MatchString(r.RequestURI) {
					continue
				}
				http.Redirect(w, r, domain.Regexp.ReplaceAllString(r.RequestURI, domain.Replacement), domain.Code)
				return
			}
		}
	}

	conn, _, err := http.NewResponseController(w).Hijack()
	if err != nil {
		return
	}
	conn.Close()
}
