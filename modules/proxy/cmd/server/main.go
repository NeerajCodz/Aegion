package main

import (
	"flag"
	"log"

	"github.com/aegion/aegion/internal/platform/moduleserver"
)

func main() {
	listenAddr := flag.String("listen", moduleserver.EnvOrDefault("AEGION_PROXY_HTTP_LISTEN_ADDR", "0.0.0.0:9009"), "HTTP listen address")
	flag.Parse()

	err := moduleserver.Run(moduleserver.Config{
		Module:       "proxy",
		Version:      "0.1.0",
		ListenAddr:   *listenAddr,
		Capabilities: []string{"authz_proxy", "policy_enforcement"},
		Routes:       []string{"/proxy/*", "/api/v1/proxy/*"},
		GRPCServices: []string{"proxy.PolicyProxy"},
		EventSubscriptions: []string{
			"policy.updated",
			"identity.updated",
			"session.created",
			"session.revoked",
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
