package main

import (
	"context"
	"flag"
	"net/http"
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
	cfg := moduleConfig("127.0.0.1:9010", func(*http.ServeMux) {})
	if cfg.Module != "cli" || cfg.Version != moduleVersion || cfg.ListenAddr != "127.0.0.1:9010" {
		t.Fatalf("unexpected module config header: %+v", cfg)
	}
	if len(cfg.Capabilities) != 3 || cfg.Capabilities[0] != "ops_interface" {
		t.Fatalf("unexpected capabilities: %#v", cfg.Capabilities)
	}
	if len(cfg.Routes) != 1 || cfg.Routes[0] != "/api/v1/cli/*" {
		t.Fatalf("unexpected routes: %#v", cfg.Routes)
	}
	if cfg.RegisterHTTPRoutes == nil {
		t.Fatal("expected HTTP route registration hook")
	}
}

func TestBuildRuntimeDefaultsToMemoryStore(t *testing.T) {
	t.Setenv(dbURLEnv, "")
	t.Setenv(legacyDBURLEnv, "")
	t.Setenv(managementTokenEnv, "secret")

	runtime, err := buildRuntime(context.Background())
	if err != nil {
		t.Fatalf("buildRuntime returned error: %v", err)
	}
	if runtime == nil || runtime.registerHTTPRoutes == nil || runtime.cleanup == nil {
		t.Fatalf("unexpected runtime: %+v", runtime)
	}
}

func TestMainInvokesRunModuleServer(t *testing.T) {
	origRun := runModuleServer
	origBuild := buildRuntimeHook
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	t.Cleanup(func() {
		runModuleServer = origRun
		buildRuntimeHook = origBuild
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	})

	var captured moduleserver.Config
	runModuleServer = func(cfg moduleserver.Config) error {
		captured = cfg
		return nil
	}
	buildRuntimeHook = func(context.Context) (*moduleRuntime, error) {
		return &moduleRuntime{
			registerHTTPRoutes: func(*http.ServeMux) {},
			cleanup:            func() {},
		}, nil
	}

	os.Args = []string{"cli-server", "-listen", "127.0.0.1:19110"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	main()

	if captured.Module != "cli" || captured.ListenAddr != "127.0.0.1:19110" {
		t.Fatalf("main did not pass expected config: %+v", captured)
	}
}
