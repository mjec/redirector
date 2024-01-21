package server

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/mjec/redirector/configuration"
	"github.com/prometheus/client_golang/prometheus"
)

type contextKey int

const (
	configFromContext contextKey = iota
	metricsFromContext
)

type Metrics struct {
	InFlightRequests prometheus.Gauge
	TotalRequests    *prometheus.CounterVec
	HandlerDuration  *prometheus.HistogramVec
}

func MakeHandler(config *configuration.Config, metrics *Metrics) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), configFromContext, config)
		ctx = context.WithValue(ctx, metricsFromContext, metrics)
		handler(w, r.WithContext(ctx))
	}
}

func handler(w http.ResponseWriter, r *http.Request) {
	config := r.Context().Value(configFromContext).(*configuration.Config)
	if config == nil {
		panic("Config missing from context: this is a bug")
	}

	metrics := r.Context().Value(metricsFromContext).(*Metrics)
	if metrics == nil {
		panic("Metrics collector missing from context: this is a bug")
	}

	// To be set using setMetricsLabels()
	metricLabels := prometheus.Labels{}

	// InFlightRequests has no labels because we want to measure the total number of requests in flight,
	// and we'd have to wait too long to figure out the label values.
	metrics.InFlightRequests.Inc()
	defer metrics.InFlightRequests.Dec()

	timer := prometheus.NewTimer(prometheus.ObserverFunc(func(v float64) {
		metrics.HandlerDuration.With(metricLabels).Observe(v)
	}))
	defer timer.ObserveDuration()

	defer func() { metrics.TotalRequests.With(metricLabels).Inc() }()

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

				setMetricsLabels(metricLabels, origin, index, r.Method, rule.Code)

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

	setMetricsLabels(metricLabels, defaultResponseSource, -1, r.Method, defaultResponse.Code)

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

func setMetricsLabels(l prometheus.Labels, domain string, rule_index int, method string, code int) {
	l["domain"] = domain
	if rule_index < 0 {
		l["rule_index"] = "default"
	} else {
		l["rule_index"] = strconv.FormatInt(int64(rule_index), 10)
	}
	l["method"] = method
	l["code"] = strconv.FormatInt(int64(code), 10)
}
