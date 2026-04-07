package main

import (
	"flag"
	"log"

	"github.com/aegion/aegion/internal/platform/moduleserver"
)

func main() {
	listenAddr := flag.String("listen", moduleserver.EnvOrDefault("AEGION_MFA_HTTP_LISTEN_ADDR", "0.0.0.0:9003"), "HTTP listen address")
	flag.Parse()

	err := moduleserver.Run(moduleserver.Config{
		Module:       "mfa",
		Version:      "0.1.0",
		ListenAddr:   *listenAddr,
		Capabilities: []string{"totp", "webauthn", "sms", "backup_codes"},
		Routes:       []string{"/self-service/mfa/*", "/api/v1/mfa/*"},
		GRPCServices: []string{"mfa.MFAEngine"},
		EventSubscriptions: []string{
			"session.created",
			"identity.updated",
			"identity.deleted",
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
