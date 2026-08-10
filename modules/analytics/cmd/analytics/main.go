package main

import (
	"flag"
	"fmt"

	"github.com/aegion/aegion/internal/platform/moduleserver"
	"github.com/aegion/aegion/internal/xlog"
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
	}); err != nil {
		msg := "analytics startup failed"
		if *configPath != "" {
			msg = fmt.Sprintf("analytics startup failed with config %s", *configPath)
		}
		xlog.Default().Fatal(msg, "error", err)
	}
}
