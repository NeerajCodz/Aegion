package main

import (
	"flag"
	"log"

	"github.com/aegion/aegion/internal/platform/moduleserver"
)

const (
	listenAddrEnv = "AEGION_SSO_HTTP_LISTEN_ADDR"
	defaultListen = "0.0.0.0:9007"
	moduleVersion = "0.1.0"
)

var runModuleServer = moduleserver.Run

func defaultListenAddr() string {
	return moduleserver.EnvOrDefault(listenAddrEnv, defaultListen)
}

func moduleConfig(listenAddr string) moduleserver.Config {
	return moduleserver.Config{
		Module:       "sso",
		Version:      moduleVersion,
		ListenAddr:   listenAddr,
		Capabilities: []string{"saml", "scim"},
		Routes:       []string{"/self-service/sso/*", "/api/v1/sso/*", "/scim/v2/*"},
		GRPCServices: []string{"sso.SSOEngine"},
		EventSubscriptions: []string{
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
