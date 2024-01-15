package main

import (
	"log"
	"net/http"
)

func main() {
	http.HandleFunc("/", redirectHandler)
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func redirectHandler(w http.ResponseWriter, r *http.Request) {
	if r.Host == "www.example.com" {
		http.Redirect(w, r, "http://new-server.com", http.StatusMovedPermanently)
	} else {
		// TODO: make this just close the connection, not even return an HTTP response
		w.WriteHeader(http.StatusUnauthorized)
	}
}
