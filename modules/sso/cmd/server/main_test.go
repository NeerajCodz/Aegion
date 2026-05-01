package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"flag"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/aegion/aegion/internal/platform/moduleserver"
	"github.com/aegion/aegion/modules/sso/store"
	"github.com/jackc/pgx/v5/pgxpool"
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

func TestBuildRuntimePropagatesRepositoryError(t *testing.T) {
	t.Setenv(dbURLEnv, "://bad-url")
	t.Setenv(legacyDBURLEnv, "")
	runtime, err := buildRuntime(context.Background())
	if err == nil || runtime != nil {
		t.Fatalf("buildRuntime(parse error) runtime=%v err=%v", runtime, err)
	}
}

func TestBuildRepositoryNoDBURLReturnsMemoryStore(t *testing.T) {
	t.Setenv(dbURLEnv, "")
	t.Setenv(legacyDBURLEnv, "")
	repo, cleanup, err := buildRepository(context.Background())
	if err != nil || repo == nil || cleanup == nil {
		t.Fatalf("buildRepository(no db) repo=%v cleanupNil=%t err=%v", repo, cleanup == nil, err)
	}
	cleanup()
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

func TestMainFatalBranches(t *testing.T) {
	origRun := runModuleServer
	origBuild := buildRuntimeHook
	origFatal := logFatal
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	t.Cleanup(func() {
		runModuleServer = origRun
		buildRuntimeHook = origBuild
		logFatal = origFatal
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	})

	expectFatal := func(fn func(), want string) {
		t.Helper()
		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("expected fatal panic")
			}
			got, ok := r.(error)
			if !ok || !strings.Contains(got.Error(), want) {
				t.Fatalf("fatal panic=%v, want contains %q", r, want)
			}
		}()
		fn()
	}

	logFatal = func(v ...any) { panic(v[0]) }

	t.Run("runtime error", func(t *testing.T) {
		os.Args = []string{"sso-server"}
		flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
		buildRuntimeHook = func(context.Context) (*moduleRuntime, error) { return nil, errors.New("runtime failed") }
		expectFatal(main, "runtime failed")
	})

	t.Run("server error", func(t *testing.T) {
		os.Args = []string{"sso-server"}
		flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
		buildRuntimeHook = func(context.Context) (*moduleRuntime, error) {
			return &moduleRuntime{registerHTTPRoutes: func(*http.ServeMux) {}, cleanup: func() {}}, nil
		}
		runModuleServer = func(moduleserver.Config) error { return errors.New("server failed") }
		expectFatal(main, "server failed")
	})
}

func TestBuildRepositoryHookedBranches(t *testing.T) {
	origNewPool := newPoolWithConfigHook
	origPing := poolPingHook
	origClose := poolCloseHook
	origNewRepo := newPostgresRepoHook
	t.Cleanup(func() {
		newPoolWithConfigHook = origNewPool
		poolPingHook = origPing
		poolCloseHook = origClose
		newPostgresRepoHook = origNewRepo
	})

	t.Setenv(dbURLEnv, "postgres://user:pass@localhost:5432/aegion?sslmode=disable")
	t.Setenv(legacyDBURLEnv, "")

	t.Run("new pool error", func(t *testing.T) {
		newPoolWithConfigHook = func(context.Context, *pgxpool.Config) (*pgxpool.Pool, error) {
			return nil, errors.New("new pool failed")
		}
		_, _, err := buildRepository(context.Background())
		if err == nil || err.Error() != "new pool failed" {
			t.Fatalf("buildRepository(new pool error) = %v", err)
		}
	})

	t.Run("new repository error", func(t *testing.T) {
		newPoolWithConfigHook = func(context.Context, *pgxpool.Config) (*pgxpool.Pool, error) { return nil, nil }
		poolPingHook = func(context.Context, *pgxpool.Pool) error { return nil }
		poolCloseHook = func(*pgxpool.Pool) {}
		newPostgresRepoHook = func(*pgxpool.Pool) (*store.PostgresStore, error) { return nil, errors.New("repo failed") }
		_, _, err := buildRepository(context.Background())
		if err == nil || err.Error() != "repo failed" {
			t.Fatalf("buildRepository(new repository error) = %v", err)
		}
	})

	t.Run("success cleanup", func(t *testing.T) {
		closed := false
		newPoolWithConfigHook = func(context.Context, *pgxpool.Config) (*pgxpool.Pool, error) { return nil, nil }
		poolPingHook = func(context.Context, *pgxpool.Pool) error { return nil }
		poolCloseHook = func(*pgxpool.Pool) { closed = true }
		newPostgresRepoHook = func(*pgxpool.Pool) (*store.PostgresStore, error) { return &store.PostgresStore{}, nil }
		repo, cleanup, err := buildRepository(context.Background())
		if err != nil || repo == nil || cleanup == nil {
			t.Fatalf("buildRepository(success) repo=%v cleanupNil=%t err=%v", repo, cleanup == nil, err)
		}
		cleanup()
		if !closed {
			t.Fatal("expected cleanup to close pool")
		}
	})
}
