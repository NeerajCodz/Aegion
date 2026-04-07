package main

import (
	"flag"
	"log"

	"github.com/aegion/aegion/internal/platform/moduleserver"
)

func main() {
	listenAddr := flag.String("listen", moduleserver.EnvOrDefault("AEGION_SSO_HTTP_LISTEN_ADDR", "0.0.0.0:9007"), "HTTP listen address")
	flag.Parse()

	err := moduleserver.Run(moduleserver.Config{
		Module:       "sso",
		Version:      "0.1.0",
		ListenAddr:   *listenAddr,
		Capabilities: []string{"saml", "scim"},
		Routes:       []string{"/self-service/sso/*", "/api/v1/sso/*", "/scim/v2/*"},
		GRPCServices: []string{"sso.SSOEngine"},
		EventSubscriptions: []string{
			"identity.updated",
			"identity.deleted",
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
