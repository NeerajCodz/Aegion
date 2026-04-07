package main

import (
	"flag"
	"log"

	"github.com/aegion/aegion/internal/platform/moduleserver"
)

const (
	listenAddrEnv = "AEGION_SOCIAL_HTTP_LISTEN_ADDR"
	defaultListen = "0.0.0.0:9006"
	moduleVersion = "0.1.0"
)

var runModuleServer = moduleserver.Run

func defaultListenAddr() string {
	return moduleserver.EnvOrDefault(listenAddrEnv, defaultListen)
}

func moduleConfig(listenAddr string) moduleserver.Config {
	return moduleserver.Config{
		Module:       "social",
		Version:      moduleVersion,
		ListenAddr:   listenAddr,
		Capabilities: []string{"oauth2_social_login"},
		Routes:       []string{"/self-service/social/*", "/api/v1/social/*"},
		GRPCServices: []string{"social.SocialEngine"},
		EventSubscriptions: []string{
			"identity.created",
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
