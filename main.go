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

type RewriteRule struct {
	Regexp      *regexp.Regexp
	Replacement string
	Code        int
}

type Config struct {
	Domains map[string][]RewriteRule `json:"domains"`
	Addr    string                   `json:"addr"`
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
	decoder.DisallowUnknownFields()

	err = decoder.Decode(&config)
	if err != nil {
		log.Fatal(err)
	}

	domainRegex := regexp.MustCompile(`^(?:[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)

	var problems []string

	for origin, domains := range config.Domains {
		if !domainRegex.MatchString(origin) {
			problems = append(problems, fmt.Sprintf("Invalid domain %s. Keys must be valid fully qualified DNS domain names.", origin))
		}

		for _, domain := range domains {
			if domain.Code < 300 || domain.Code > 399 {
				problems = append(problems, fmt.Sprintf("Invalid redirect code for domain %s. Code must be between 300 and 399 inclusive.", origin))
			}

			if !strings.HasPrefix(domain.Replacement, "http://") && !strings.HasPrefix(domain.Replacement, "https://") {
				problems = append(problems, fmt.Sprintf("Invalid replacement for domain %s. Destination must begin with 'http://' or 'https://'.", origin))
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
	requestUri := r.URL.String()
	for origin, domains := range config.Domains {
		if r.Host == origin {
			for _, domain := range domains {
				if !domain.Regexp.MatchString(requestUri) {
					continue
				}
				http.Redirect(w, r, domain.Regexp.ReplaceAllString(requestUri, domain.Replacement), domain.Code)
				return
			}
		}
	}

	// TODO: make this just close the connection, not even return an HTTP response
	w.WriteHeader(http.StatusUnauthorized)
}
