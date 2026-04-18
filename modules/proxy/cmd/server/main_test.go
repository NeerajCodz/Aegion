package main

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/aegion/aegion/internal/platform/moduleserver"
	"github.com/aegion/aegion/modules/proxy/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDefaultListenAddr(t *testing.T) {
	t.Setenv(listenAddrEnv, "")
	if got := defaultListenAddr(); got != defaultListen {
		t.Fatalf("expected default listen addr %q, got %q", defaultListen, got)
	}

	t.Setenv(listenAddrEnv, "127.0.0.1:19009")
	if got := defaultListenAddr(); got != "127.0.0.1:19009" {
		t.Fatalf("expected env listen addr override, got %q", got)
	}
}

func TestModuleConfig(t *testing.T) {
	cfg := moduleConfig("127.0.0.1:9009", func(*http.ServeMux) {})
	if cfg.Module != "proxy" || cfg.Version != moduleVersion || cfg.ListenAddr != "127.0.0.1:9009" {
		t.Fatalf("unexpected module config header: %+v", cfg)
	}
	if len(cfg.Capabilities) != 3 || cfg.Capabilities[2] != "proxy_rule_registry" {
		t.Fatalf("unexpected capabilities: %#v", cfg.Capabilities)
	}
	if len(cfg.Routes) != 2 || cfg.Routes[0] != "/proxy/*" || cfg.Routes[1] != "/api/v1/proxy/*" {
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
	if runtime == nil || runtime.registerHTTPRoutes == nil {
		t.Fatal("expected runtime with HTTP routes")
	}
	if runtime.cleanup == nil {
		t.Fatal("expected cleanup function")
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
	buildRuntimeHook = func(_ context.Context) (*moduleRuntime, error) {
		return &moduleRuntime{
			registerHTTPRoutes: func(*http.ServeMux) {},
			cleanup:            func() {},
		}, nil
	}

	os.Args = []string{"proxy-server", "-listen", "127.0.0.1:19009"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	main()

	if captured.Module != "proxy" || captured.ListenAddr != "127.0.0.1:19009" {
		t.Fatalf("main did not pass expected config: %+v", captured)
	}
}

func TestBuildRepositoryRejectsInvalidDBURL(t *testing.T) {
	t.Setenv(dbURLEnv, "://bad-url")
	t.Setenv(legacyDBURLEnv, "")

	repo, cleanup, err := buildRepository(context.Background())
	if err == nil {
		t.Fatal("expected parse error for invalid proxy database URL")
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
		os.Args = []string{"proxy-server"}
		flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
		buildRuntimeHook = func(context.Context) (*moduleRuntime, error) { return nil, errors.New("runtime failed") }
		expectFatal(main, "runtime failed")
	})

	t.Run("server error", func(t *testing.T) {
		os.Args = []string{"proxy-server"}
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
