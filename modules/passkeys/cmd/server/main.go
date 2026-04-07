package main

import (
	"flag"
	"log"

	"github.com/aegion/aegion/internal/platform/moduleserver"
)

func main() {
	listenAddr := flag.String("listen", moduleserver.EnvOrDefault("AEGION_PASSKEYS_HTTP_LISTEN_ADDR", "0.0.0.0:9004"), "HTTP listen address")
	flag.Parse()

	err := moduleserver.Run(moduleserver.Config{
		Module:       "passkeys",
		Version:      "0.1.0",
		ListenAddr:   *listenAddr,
		Capabilities: []string{"webauthn_passwordless"},
		Routes:       []string{"/self-service/passkeys/*", "/api/v1/passkeys/*"},
		GRPCServices: []string{"passkeys.PasskeyEngine"},
		EventSubscriptions: []string{
			"session.created",
			"identity.deleted",
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
