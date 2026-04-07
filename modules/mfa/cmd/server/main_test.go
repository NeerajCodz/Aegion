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

	t.Setenv(listenAddrEnv, "127.0.0.1:19003")
	if got := defaultListenAddr(); got != "127.0.0.1:19003" {
		t.Fatalf("expected env listen addr override, got %q", got)
	}
}

func TestModuleConfig(t *testing.T) {
	cfg := moduleConfig("127.0.0.1:9003")
	if cfg.Module != "mfa" || cfg.Version != moduleVersion || cfg.ListenAddr != "127.0.0.1:9003" {
		t.Fatalf("unexpected module config header: %+v", cfg)
	}
	if len(cfg.Capabilities) != 4 || cfg.Capabilities[0] != "totp" || cfg.Capabilities[1] != "webauthn" || cfg.Capabilities[2] != "sms" || cfg.Capabilities[3] != "backup_codes" {
		t.Fatalf("unexpected capabilities: %#v", cfg.Capabilities)
	}
	if len(cfg.Routes) != 2 || cfg.Routes[0] != "/self-service/mfa/*" || cfg.Routes[1] != "/api/v1/mfa/*" {
		t.Fatalf("unexpected routes: %#v", cfg.Routes)
	}
	if len(cfg.GRPCServices) != 1 || cfg.GRPCServices[0] != "mfa.MFAEngine" {
		t.Fatalf("unexpected grpc services: %#v", cfg.GRPCServices)
	}
	if len(cfg.EventSubscriptions) != 3 || cfg.EventSubscriptions[0] != "session.created" || cfg.EventSubscriptions[1] != "identity.updated" || cfg.EventSubscriptions[2] != "identity.deleted" {
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

	os.Args = []string{"mfa-server", "-listen", "127.0.0.1:19003"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	main()

	if captured.Module != "mfa" || captured.ListenAddr != "127.0.0.1:19003" {
		t.Fatalf("main did not pass expected config: %+v", captured)
	}
}
