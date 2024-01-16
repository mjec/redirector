package main

import (
	"net/http"
	"os"

	"log/slog"

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

	http.HandleFunc("/", server.MakeHandler(config))

	logger.Info("Listening for remote connections", "address", config.ListenAddress)
	err = http.ListenAndServe(config.ListenAddress, nil)
	if err != nil {
		logger.Error("Server shut down", "error", err)
		os.Exit(1)
	}
}
