package main

import (
	"flag"
	"log"

	"github.com/aegion/aegion/internal/platform/moduleserver"
)

func main() {
	listenAddr := flag.String("listen", moduleserver.EnvOrDefault("AEGION_SOCIAL_HTTP_LISTEN_ADDR", "0.0.0.0:9006"), "HTTP listen address")
	flag.Parse()

	err := moduleserver.Run(moduleserver.Config{
		Module:       "social",
		Version:      "0.1.0",
		ListenAddr:   *listenAddr,
		Capabilities: []string{"oauth2_social_login"},
		Routes:       []string{"/self-service/social/*", "/api/v1/social/*"},
		GRPCServices: []string{"social.SocialEngine"},
		EventSubscriptions: []string{
			"identity.created",
			"identity.updated",
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
