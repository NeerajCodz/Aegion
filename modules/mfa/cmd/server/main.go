package main

import (
	"flag"
	"log"

	"github.com/aegion/aegion/internal/platform/moduleserver"
)

const (
	listenAddrEnv = "AEGION_MFA_HTTP_LISTEN_ADDR"
	defaultListen = "0.0.0.0:9003"
	moduleVersion = "0.1.0"
)

var runModuleServer = moduleserver.Run

func defaultListenAddr() string {
	return moduleserver.EnvOrDefault(listenAddrEnv, defaultListen)
}

func moduleConfig(listenAddr string) moduleserver.Config {
	return moduleserver.Config{
		Module:       "mfa",
		Version:      moduleVersion,
		ListenAddr:   listenAddr,
		Capabilities: []string{"totp", "webauthn", "sms", "backup_codes"},
		Routes:       []string{"/self-service/mfa/*", "/api/v1/mfa/*"},
		GRPCServices: []string{"mfa.MFAEngine"},
		EventSubscriptions: []string{
			"session.created",
			"identity.updated",
			"identity.deleted",
		},
	}
}

func main() {
	listenAddr := flag.String("listen", defaultListenAddr(), "HTTP listen address")
	flag.Parse()

	err := runModuleServer(moduleConfig(*listenAddr))
	if err != nil {
		log.Fatal(err)
	}
}
