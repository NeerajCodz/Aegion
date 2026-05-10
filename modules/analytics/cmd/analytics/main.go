package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/aegion/aegion/internal/platform/moduleserver"
)

var version = "dev"

func main() {
	configPath := flag.String("config", "", "path to analytics configuration")
	flag.Parse()

	listenAddr := moduleserver.EnvOrDefault("AEGION_ANALYTICS_LISTEN_ADDR", "0.0.0.0:8080")
	if err := moduleserver.Run(moduleserver.Config{
		Module:       "analytics",
		Version:      version,
		ListenAddr:   listenAddr,
		Capabilities: []string{"events", "dashboards", "retention", "webhooks"},
		Routes: []string{
			"/health",
			"/ready",
			"/meta",
		},
		GRPCServices: []string{"analytics.v1.AnalyticsService"},
	}); err != nil {
		if *configPath != "" {
			err = fmt.Errorf("analytics startup failed with config %s: %w", *configPath, err)
		}
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
