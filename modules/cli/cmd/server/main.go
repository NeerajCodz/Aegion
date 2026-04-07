package main

import (
	"flag"
	"log"

	"github.com/aegion/aegion/internal/platform/moduleserver"
)

const (
	listenAddrEnv = "AEGION_CLI_HTTP_LISTEN_ADDR"
	defaultListen = "0.0.0.0:9010"
	moduleVersion = "0.1.0"
)

var runModuleServer = moduleserver.Run

func defaultListenAddr() string {
	return moduleserver.EnvOrDefault(listenAddrEnv, defaultListen)
}

func moduleConfig(listenAddr string) moduleserver.Config {
	return moduleserver.Config{
		Module:       "cli",
		Version:      moduleVersion,
		ListenAddr:   listenAddr,
		Capabilities: []string{"automation", "ops_interface"},
		Routes:       []string{"/api/v1/cli/*"},
		GRPCServices: []string{"cli.CommandGateway"},
		EventSubscriptions: []string{
			"system.health",
			"policy.updated",
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
