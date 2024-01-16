package server

import (
	"context"
	"log/slog"
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
	defaultResponseSource := "default"
	requestUri := r.URL.RequestURI()

	for origin, domain := range config.Domains {
		if strings.EqualFold(r.Host, origin) || (domain.MatchSubdomains && strings.HasSuffix(strings.ToLower(r.Host), "."+origin)) {
			for index, rule := range domain.RewriteRules {
				if !rule.Regexp.MatchString(requestUri) {
					continue
				}
				destination := rule.Regexp.ReplaceAllString(requestUri, rule.Replacement)
				if rule.LogHits {
					slog.Default().Info(
						"Redirect",
						"remote_addr", r.RemoteAddr,
						"method", r.Method,
						"host", r.Host,
						"request_uri", requestUri,
						"user_agent", r.Header.Get("user-agent"),
						"referer", r.Header.Get("referer"),
						"rule_domain", origin,
						"rule_index", index,
						"regexp", rule.Regexp,
						"code", rule.Code,
						"destination", destination,
					)
				}
				http.Redirect(w, r, destination, rule.Code)
				return
			}

			if domain.DefaultResponse != nil {
				defaultResponse = domain.DefaultResponse
				defaultResponseSource = origin
			}
			break
		}
	}

	if defaultResponse.LogHits {
		slog.Default().Info(
			"Default response",
			"remote_addr", r.RemoteAddr,
			"method", r.Method,
			"host", r.Host,
			"request_uri", requestUri,
			"user_agent", r.Header.Get("user-agent"),
			"referer", r.Header.Get("referer"),
			"code", defaultResponse.Code,
			"source", defaultResponseSource,
		)
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
