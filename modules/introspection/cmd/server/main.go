package main

import (
	"flag"
	"log"

	"github.com/aegion/aegion/internal/platform/moduleserver"
)

func main() {
	listenAddr := flag.String("listen", moduleserver.EnvOrDefault("AEGION_INTROSPECTION_HTTP_LISTEN_ADDR", "0.0.0.0:9008"), "HTTP listen address")
	flag.Parse()

	err := moduleserver.Run(moduleserver.Config{
		Module:       "introspection",
		Version:      "0.1.0",
		ListenAddr:   *listenAddr,
		Capabilities: []string{"token_introspection", "session_lookup"},
		Routes:       []string{"/oauth2/introspect", "/api/v1/introspection/*"},
		GRPCServices: []string{"introspection.IntrospectionService"},
		EventSubscriptions: []string{
			"session.created",
			"session.revoked",
			"identity.updated",
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
