package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
)

type Domain struct {
	Regexp      string `json:"regexp"`
	Replacement string `json:"replacement"`
	Code        int    `json:"code"`
}

type Config struct {
	Domains map[string]Domain `json:"domains"`
	Addr    string            `json:"addr"`
}

var config Config

func main() {
	configFile := flag.String("c", "config.json", "path to the config file")
	flag.Parse()

	loadConfig(*configFile)

	http.HandleFunc("/", redirectHandler)
	log.Fatal(http.ListenAndServe(config.Addr, nil))
}

func loadConfig(filename string) {
	file, err := os.Open(filename)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	err = decoder.Decode(&config)
	if err != nil {
		log.Fatal(err)
	}

	domainRegex := regexp.MustCompile(`^(?:[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)

	var problems []string

	for origin, domain := range config.Domains {
		if domain.Code < 300 || domain.Code > 399 {
			problems = append(problems, fmt.Sprintf("Invalid code for domain %s. Code must be between 300 and 399 inclusive.", origin))
		}

		if !strings.HasPrefix(domain.Replacement, "http://") && !strings.HasPrefix(domain.Replacement, "https://") {
			problems = append(problems, fmt.Sprintf("Invalid destination for domain %s. Destination must begin with 'http://' or 'https://'.", origin))
		}

		if !domainRegex.MatchString(origin) {
			problems = append(problems, fmt.Sprintf("Invalid origin for domain %s. Origin must be a valid fully qualified DNS domain name.", origin))
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
	for origin, domain := range config.Domains {
		if r.Host == origin {
			redirectURL := domain.Destination
			if r.URL.RawQuery != "" {
				if strings.Contains(domain.Destination, "?") {
					redirectURL += "&" + r.URL.RawQuery
				} else {
					redirectURL += "?" + r.URL.RawQuery
				}
			}
			http.Redirect(w, r, redirectURL, domain.Code)
			return
		}
	}

	// TODO: make this just close the connection, not even return an HTTP response
	w.WriteHeader(http.StatusUnauthorized)
}
