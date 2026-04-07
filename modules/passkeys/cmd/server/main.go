package main

import (
	"flag"
	"log"

	"github.com/aegion/aegion/internal/platform/moduleserver"
)

const (
	listenAddrEnv = "AEGION_PASSKEYS_HTTP_LISTEN_ADDR"
	defaultListen = "0.0.0.0:9004"
	moduleVersion = "0.1.0"
)

var runModuleServer = moduleserver.Run

func defaultListenAddr() string {
	return moduleserver.EnvOrDefault(listenAddrEnv, defaultListen)
}

func moduleConfig(listenAddr string) moduleserver.Config {
	return moduleserver.Config{
		Module:       "passkeys",
		Version:      moduleVersion,
		ListenAddr:   listenAddr,
		Capabilities: []string{"webauthn_passwordless"},
		Routes:       []string{"/self-service/passkeys/*", "/api/v1/passkeys/*"},
		GRPCServices: []string{"passkeys.PasskeyEngine"},
		EventSubscriptions: []string{
			"session.created",
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
