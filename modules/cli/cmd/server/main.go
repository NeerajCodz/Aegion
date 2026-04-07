package main

import (
	"flag"
	"log"

	"github.com/aegion/aegion/internal/platform/moduleserver"
)

func main() {
	listenAddr := flag.String("listen", moduleserver.EnvOrDefault("AEGION_CLI_HTTP_LISTEN_ADDR", "0.0.0.0:9010"), "HTTP listen address")
	flag.Parse()

	err := moduleserver.Run(moduleserver.Config{
		Module:       "cli",
		Version:      "0.1.0",
		ListenAddr:   *listenAddr,
		Capabilities: []string{"automation", "ops_interface"},
		Routes:       []string{"/api/v1/cli/*"},
		GRPCServices: []string{"cli.CommandGateway"},
		EventSubscriptions: []string{
			"system.health",
			"policy.updated",
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
