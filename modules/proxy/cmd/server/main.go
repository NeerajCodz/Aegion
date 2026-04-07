package main

import (
	"flag"
	"log"

	"github.com/aegion/aegion/internal/platform/moduleserver"
)

const (
	listenAddrEnv = "AEGION_PROXY_HTTP_LISTEN_ADDR"
	defaultListen = "0.0.0.0:9009"
	moduleVersion = "0.1.0"
)

var runModuleServer = moduleserver.Run

func defaultListenAddr() string {
	return moduleserver.EnvOrDefault(listenAddrEnv, defaultListen)
}

func moduleConfig(listenAddr string) moduleserver.Config {
	return moduleserver.Config{
		Module:       "proxy",
		Version:      moduleVersion,
		ListenAddr:   listenAddr,
		Capabilities: []string{"authz_proxy", "policy_enforcement"},
		Routes:       []string{"/proxy/*", "/api/v1/proxy/*"},
		GRPCServices: []string{"proxy.PolicyProxy"},
		EventSubscriptions: []string{
			"policy.updated",
			"identity.updated",
			"session.created",
			"session.revoked",
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
