package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
)

type Domain struct {
	Origin      string `json:"origin"`
	Destination string `json:"destination"`
	Code        int    `json:"code"`
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
}

func redirectHandler(w http.ResponseWriter, r *http.Request) {
	if r.Host == "www.example.com" {
		http.Redirect(w, r, "http://new-server.com", http.StatusMovedPermanently)
	} else {
		// TODO: make this just close the connection, not even return an HTTP response
		w.WriteHeader(http.StatusUnauthorized)
	}
}
