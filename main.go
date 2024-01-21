package main

import (
	"net/http"
	"os"

	"log/slog"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/mjec/redirector/configuration"
	"github.com/mjec/redirector/server"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	configFilePath := os.Getenv("REDIRECTOR_CONFIG")
	if configFilePath == "" {
		configFilePath = "config.json"
	}

	logger.Info("Loading config", "file", configFilePath)
	file, err := os.Open(configFilePath)
	if err != nil {
		logger.Error("Unable to open configuration file", "file", configFilePath, "error", err)
		os.Exit(1)
	}
	defer file.Close()

	config := &configuration.Config{}
	if problems := configuration.LoadConfig(file, config); len(problems) > 0 {
		logger.Error("Unable to start due to errors in configuration", "error_count", len(problems))
		for _, problem := range problems {
			logger.Error("Configuration error", "error", problem)
		}
		os.Exit(1)
	}

	metrics := &server.Metrics{
		InFlightRequests: prometheus.NewGauge(prometheus.GaugeOpts{Name: "in_flight_requests", Help: "A gauge of requests currently being served"}),
		TotalRequests:    prometheus.NewCounterVec(prometheus.CounterOpts{Name: "requests_total", Help: "A counter for requests"}, []string{"domain", "rule_index", "method", "code"}),
		HandlerDuration:  prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "request_duration_seconds", Help: "A histogram of latencies for requests"}, []string{"domain", "rule_index", "method", "code"}),
	}
	prometheus.MustRegister(metrics.InFlightRequests)
	prometheus.MustRegister(metrics.TotalRequests)
	prometheus.MustRegister(metrics.HandlerDuration)

	go func() {
		http.Handle(config.MetricsPath, promhttp.Handler())
		http.ListenAndServe(config.MetricsAddress, nil)
	}()
	logger.Info("Listening for prometheus connections", "address", config.MetricsAddress, "path", config.MetricsPath)

	http.HandleFunc("/", http.HandlerFunc(server.MakeHandler(config, metrics)))
	logger.Info("Listening for remote connections", "address", config.ListenAddress)
	err = http.ListenAndServe(config.ListenAddress, nil)
	if err != nil {
		logger.Error("Server shut down", "error", err)
		os.Exit(1)
	}
}
