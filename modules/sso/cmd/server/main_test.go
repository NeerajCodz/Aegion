package main

import (
	"context"
	"crypto/sha256"
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

func TestBuildRepositoryRejectsInvalidDBURL(t *testing.T) {
	t.Setenv(dbURLEnv, "://bad-url")
	t.Setenv(legacyDBURLEnv, "")

	repo, cleanup, err := buildRepository(context.Background())
	if err == nil {
		t.Fatal("expected parse error for invalid SSO database URL")
	}
	if repo != nil || cleanup != nil {
		t.Fatalf("expected nil repo/cleanup on parse error, got repo=%v cleanupNil=%t", repo, cleanup == nil)
	}
}

func TestBuildRepositoryPingFailure(t *testing.T) {
	t.Setenv(dbURLEnv, "postgres://user:pass@127.0.0.1:5432/aegion?sslmode=disable")
	t.Setenv(legacyDBURLEnv, "")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	repo, cleanup, err := buildRepository(ctx)
	if err == nil {
		t.Fatal("expected ping failure with canceled context")
	}
	if repo != nil || cleanup != nil {
		t.Fatalf("expected nil repo/cleanup on ping failure, got repo=%v cleanupNil=%t", repo, cleanup == nil)
	}
}

func TestDeriveStateSecret(t *testing.T) {
	t.Setenv(stateSecretEnv, "")
	t.Setenv(legacyStateEnv, "")
	if got := deriveStateSecret(); got != nil {
		t.Fatalf("expected nil secret when envs are empty, got %x", got)
	}

	t.Setenv(legacyStateEnv, "legacy-secret")
	wantLegacy := sha256.Sum256([]byte("legacy-secret"))
	if got := deriveStateSecret(); len(got) != sha256.Size || string(got) != string(wantLegacy[:]) {
		t.Fatalf("unexpected legacy-derived secret: %x", got)
	}

	t.Setenv(stateSecretEnv, "primary-secret")
	wantPrimary := sha256.Sum256([]byte("primary-secret"))
	if got := deriveStateSecret(); len(got) != sha256.Size || string(got) != string(wantPrimary[:]) {
		t.Fatalf("unexpected primary-derived secret: %x", got)
	}
}
