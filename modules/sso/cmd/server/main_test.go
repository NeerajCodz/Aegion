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
}

func TestModuleConfig(t *testing.T) {
	cfg := moduleConfig("127.0.0.1:9007", func(*http.ServeMux) {})
	if cfg.Module != "sso" || cfg.Version != moduleVersion || cfg.ListenAddr != "127.0.0.1:9007" {
		t.Fatalf("unexpected module config: %+v", cfg)
	}
	if len(cfg.Capabilities) != 3 || cfg.Capabilities[1] != "connection_registry" {
		t.Fatalf("unexpected capabilities: %#v", cfg.Capabilities)
	}
}

func TestBuildRuntimeDefaultsToMemoryStore(t *testing.T) {
	t.Setenv(dbURLEnv, "")
	t.Setenv(legacyDBURLEnv, "")
	t.Setenv(managementTokenEnv, "secret")
	t.Setenv(stateSecretEnv, "super-secret")
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
		return &moduleRuntime{registerHTTPRoutes: func(*http.ServeMux) {}, cleanup: func() {}}, nil
	}
	os.Args = []string{"sso-server", "-listen", "127.0.0.1:19007"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	main()
	if captured.Module != "sso" || captured.ListenAddr != "127.0.0.1:19007" {
		t.Fatalf("unexpected config passed to run: %+v", captured)
	}
}
