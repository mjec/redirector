package server

import (
	"context"
	"log"
	"net/http"
	"strings"

	"github.com/mjec/redirector/configuration"
)

type contextKey int

const (
	configFromContext contextKey = iota
)

func MakeHandler(config *configuration.Config) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		handler(w, r.WithContext(context.WithValue(r.Context(), configFromContext, config)))
	}
}

func handler(w http.ResponseWriter, r *http.Request) {
	config := r.Context().Value(configFromContext).(*configuration.Config)
	if config == nil {
		panic("Config missing from context: this is a bug")
	}

	defaultResponse := config.DefaultResponse
	requestUri := r.URL.RequestURI()

	for origin, domain := range config.Domains {
		if strings.EqualFold(r.Host, origin) || (domain.MatchSubdomains && strings.HasSuffix(strings.ToLower(r.Host), "."+origin)) {
			for _, rule := range domain.RewriteRules {
				if !rule.Regexp.MatchString(requestUri) {
					continue
				}
				if rule.LogHits {
					log.Printf("%s %s %s %s", r.RemoteAddr, r.Method, r.Host, requestUri)
				}
				http.Redirect(w, r, rule.Regexp.ReplaceAllString(requestUri, rule.Replacement), rule.Code)
				return
			}

			if domain.DefaultResponse != nil {
				defaultResponse = domain.DefaultResponse
			}
			break
		}
	}

	if defaultResponse.LogHits {
		log.Printf("%s %s %s %s", r.RemoteAddr, r.Method, r.Host, requestUri)
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
