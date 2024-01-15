package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
)

type Domain struct {
	Origin      string `json:"origin"`
	Destination string `json:"destination"`
	Code        int    `json:"code"`
	AppendPath  bool   `json:"append_path"`
}

type Config struct {
	Domains []Domain `json:"domains"`
	Addr    string   `json:"addr"`
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

	for _, domain := range config.Domains {
		if domain.Code < 300 || domain.Code > 399 {
			log.Fatalf("Invalid code for domain %s. Code must be between 300 and 399 inclusive.", domain.Origin)
		}

		if !strings.HasPrefix(domain.Destination, "http://") && !strings.HasPrefix(domain.Destination, "https://") {
			log.Fatalf("Invalid destination for domain %s. Destination must begin with 'http://' or 'https://'.", domain.Origin)
		}

		if !isValidDomain(domain.Origin) {
			log.Fatalf("Invalid origin for domain %s. Origin must be a valid fully qualified DNS domain name.", domain.Origin)
		}
	}
}

func isValidDomain(domain string) bool {
	// Regular expression pattern for validating a fully qualified DNS domain name
	pattern := `^(?:[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`

	// Compile the regular expression pattern
	regex := regexp.MustCompile(pattern)

	// Match the domain against the regular expression
	return regex.MatchString(domain)
}

func redirectHandler(w http.ResponseWriter, r *http.Request) {
	for _, domain := range config.Domains {
		if r.Host == domain.Origin {
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
