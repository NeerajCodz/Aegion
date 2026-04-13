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

	t.Setenv(listenAddrEnv, "127.0.0.1:19006")
	if got := defaultListenAddr(); got != "127.0.0.1:19006" {
		t.Fatalf("expected env listen addr override, got %q", got)
	}
}

func TestModuleConfig(t *testing.T) {
	cfg := moduleConfig("127.0.0.1:9006", nil)
	if cfg.Module != "social" || cfg.Version != moduleVersion || cfg.ListenAddr != "127.0.0.1:9006" {
		t.Fatalf("unexpected module config header: %+v", cfg)
	}
	if len(cfg.Capabilities) != 1 || cfg.Capabilities[0] != "oauth2_social_login" {
		t.Fatalf("unexpected capabilities: %#v", cfg.Capabilities)
	}
	if len(cfg.Routes) != 2 || cfg.Routes[0] != "/self-service/social/*" || cfg.Routes[1] != "/api/v1/social/*" {
		t.Fatalf("unexpected routes: %#v", cfg.Routes)
	}
	if len(cfg.GRPCServices) != 1 || cfg.GRPCServices[0] != "social.SocialEngine" {
		t.Fatalf("unexpected grpc services: %#v", cfg.GRPCServices)
	}
	if len(cfg.EventSubscriptions) != 2 || cfg.EventSubscriptions[0] != "identity.created" || cfg.EventSubscriptions[1] != "identity.updated" {
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

	os.Args = []string{"social-server", "-listen", "127.0.0.1:19006"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	main()

	if captured.Module != "social" || captured.ListenAddr != "127.0.0.1:19006" {
		t.Fatalf("main did not pass expected config: %+v", captured)
	}
	if captured.RegisterHTTPRoutes == nil {
		t.Fatal("expected social http route registrar to be set")
	}
}
