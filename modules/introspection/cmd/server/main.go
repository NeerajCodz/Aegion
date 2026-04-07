package main

import (
	"flag"
	"log"

	"github.com/aegion/aegion/internal/platform/moduleserver"
)

const (
	listenAddrEnv = "AEGION_INTROSPECTION_HTTP_LISTEN_ADDR"
	defaultListen = "0.0.0.0:9008"
	moduleVersion = "0.1.0"
)

var runModuleServer = moduleserver.Run

func defaultListenAddr() string {
	return moduleserver.EnvOrDefault(listenAddrEnv, defaultListen)
}

func moduleConfig(listenAddr string) moduleserver.Config {
	return moduleserver.Config{
		Module:       "introspection",
		Version:      moduleVersion,
		ListenAddr:   listenAddr,
		Capabilities: []string{"token_introspection", "session_lookup"},
		Routes:       []string{"/oauth2/introspect", "/api/v1/introspection/*"},
		GRPCServices: []string{"introspection.IntrospectionService"},
		EventSubscriptions: []string{
			"session.created",
			"session.revoked",
			"identity.updated",
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
