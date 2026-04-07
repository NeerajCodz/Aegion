package main

import (
	"flag"
	"os"
	"testing"

	"github.com/aegion/aegion/internal/platform/moduleserver"
)

func TestDefaultListenAddr(t *testing.T) {
	t.Setenv(listenAddrEnv, "")
	if got := defaultListenAddr(); got != defaultListen {
		t.Fatalf("expected default listen addr %q, got %q", defaultListen, got)
	}

	t.Setenv(listenAddrEnv, "127.0.0.1:19100")
	if got := defaultListenAddr(); got != "127.0.0.1:19100" {
		t.Fatalf("expected env listen addr override, got %q", got)
	}
}

func TestModuleConfig(t *testing.T) {
	cfg := moduleConfig("127.0.0.1:9010")
	if cfg.Module != "cli" || cfg.Version != moduleVersion || cfg.ListenAddr != "127.0.0.1:9010" {
		t.Fatalf("unexpected module config header: %+v", cfg)
	}
	if len(cfg.Capabilities) != 2 || cfg.Capabilities[0] != "automation" || cfg.Capabilities[1] != "ops_interface" {
		t.Fatalf("unexpected capabilities: %#v", cfg.Capabilities)
	}
	if len(cfg.Routes) != 1 || cfg.Routes[0] != "/api/v1/cli/*" {
		t.Fatalf("unexpected routes: %#v", cfg.Routes)
	}
	if len(cfg.GRPCServices) != 1 || cfg.GRPCServices[0] != "cli.CommandGateway" {
		t.Fatalf("unexpected grpc services: %#v", cfg.GRPCServices)
	}
	if len(cfg.EventSubscriptions) != 2 || cfg.EventSubscriptions[0] != "system.health" || cfg.EventSubscriptions[1] != "policy.updated" {
		t.Fatalf("unexpected event subscriptions: %#v", cfg.EventSubscriptions)
	}
}

func TestMainInvokesRunModuleServer(t *testing.T) {
	origRun := runModuleServer
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	t.Cleanup(func() {
		runModuleServer = origRun
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	})

	var captured moduleserver.Config
	runModuleServer = func(cfg moduleserver.Config) error {
		captured = cfg
		return nil
	}

	os.Args = []string{"cli-server", "-listen", "127.0.0.1:19110"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	main()

	if captured.Module != "cli" || captured.ListenAddr != "127.0.0.1:19110" {
		t.Fatalf("main did not pass expected config: %+v", captured)
	}
}
