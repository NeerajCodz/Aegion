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

	t.Setenv(listenAddrEnv, "127.0.0.1:19004")
	if got := defaultListenAddr(); got != "127.0.0.1:19004" {
		t.Fatalf("expected env listen addr override, got %q", got)
	}
}

func TestModuleConfig(t *testing.T) {
	cfg := moduleConfig("127.0.0.1:9004")
	if cfg.Module != "passkeys" || cfg.Version != moduleVersion || cfg.ListenAddr != "127.0.0.1:9004" {
		t.Fatalf("unexpected module config header: %+v", cfg)
	}
	if len(cfg.Capabilities) != 1 || cfg.Capabilities[0] != "webauthn_passwordless" {
		t.Fatalf("unexpected capabilities: %#v", cfg.Capabilities)
	}
	if len(cfg.Routes) != 2 || cfg.Routes[0] != "/self-service/passkeys/*" || cfg.Routes[1] != "/api/v1/passkeys/*" {
		t.Fatalf("unexpected routes: %#v", cfg.Routes)
	}
	if len(cfg.GRPCServices) != 1 || cfg.GRPCServices[0] != "passkeys.PasskeyEngine" {
		t.Fatalf("unexpected grpc services: %#v", cfg.GRPCServices)
	}
	if len(cfg.EventSubscriptions) != 2 || cfg.EventSubscriptions[0] != "session.created" || cfg.EventSubscriptions[1] != "identity.deleted" {
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

	os.Args = []string{"passkeys-server", "-listen", "127.0.0.1:19004"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	main()

	if captured.Module != "passkeys" || captured.ListenAddr != "127.0.0.1:19004" {
		t.Fatalf("main did not pass expected config: %+v", captured)
	}
}
