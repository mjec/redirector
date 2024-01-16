package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	"github.com/mjec/redirector/configuration"
	"github.com/mjec/redirector/server"
)

func main() {
	configFile := flag.String("c", "config.json", "path to the config file")
	flag.Parse()

	log.Printf("Loading config from %s", *configFile)
	file, err := os.Open(*configFile)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	config := &configuration.Config{}
	if problems := configuration.LoadConfig(file, config); len(problems) > 0 {
		for _, problem := range problems {
			log.Println(problem)
		}
		log.Fatal("Configuration contains errors. Please fix the problems and try again.")
	}

	http.HandleFunc("/", server.MakeHandler(config))

	log.Printf("Listening on %s", config.ListenAddress)
	log.Fatal(http.ListenAndServe(config.ListenAddress, nil))
}
