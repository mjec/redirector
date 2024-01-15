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
	http.Redirect(w, r, "http://new-server.com", http.StatusMovedPermanently)
}
