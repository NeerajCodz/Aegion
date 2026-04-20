package main

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/aegion/aegion/internal/platform/moduleserver"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDefaultListenAddr(t *testing.T) {
	t.Setenv(listenAddrEnv, "")
	if got := defaultListenAddr(); got != defaultListen {
		t.Fatalf("expected default listen addr %q, got %q", defaultListen, got)
	}

	t.Setenv(listenAddrEnv, "127.0.0.1:19008")
	if got := defaultListenAddr(); got != "127.0.0.1:19008" {
		t.Fatalf("expected env listen addr override, got %q", got)
	}
}

func TestModuleConfig(t *testing.T) {
	registerCalled := false
	cfg := moduleConfig("127.0.0.1:9008", func(mux *http.ServeMux) {
		registerCalled = true
		mux.HandleFunc("/oauth2/introspect", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
	})
	if cfg.Module != "introspection" || cfg.Version != moduleVersion || cfg.ListenAddr != "127.0.0.1:9008" {
		t.Fatalf("unexpected module config header: %+v", cfg)
	}
	if len(cfg.Capabilities) != 2 || cfg.Capabilities[0] != "token_introspection" || cfg.Capabilities[1] != "session_lookup" {
		t.Fatalf("unexpected capabilities: %#v", cfg.Capabilities)
	}
	if len(cfg.Routes) != 2 || cfg.Routes[0] != "/oauth2/introspect" || cfg.Routes[1] != "/api/v1/introspection/*" {
		t.Fatalf("unexpected routes: %#v", cfg.Routes)
	}
	if len(cfg.GRPCServices) != 1 || cfg.GRPCServices[0] != "introspection.IntrospectionService" {
		t.Fatalf("unexpected grpc services: %#v", cfg.GRPCServices)
	}
	if len(cfg.EventSubscriptions) != 3 || cfg.EventSubscriptions[0] != "session.created" || cfg.EventSubscriptions[1] != "session.revoked" || cfg.EventSubscriptions[2] != "identity.updated" {
		t.Fatalf("unexpected event subscriptions: %#v", cfg.EventSubscriptions)
	}

	mux := http.NewServeMux()
	cfg.RegisterHTTPRoutes(mux)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/oauth2/introspect", nil)
	mux.ServeHTTP(rec, req)
	if !registerCalled || rec.Code != http.StatusNoContent {
		t.Fatalf("expected registered introspection route to be served, got status=%d called=%v", rec.Code, registerCalled)
	}
}

func TestOAuth2Issuer(t *testing.T) {
	t.Setenv(issuerEnv, "")
	t.Setenv(legacyIssuerEnv, "")
	if got := oauth2Issuer(); got != defaultIssuer {
		t.Fatalf("expected default issuer %q, got %q", defaultIssuer, got)
	}

	t.Setenv(legacyIssuerEnv, "https://issuer.example.com/")
	if got := oauth2Issuer(); got != "https://issuer.example.com" {
		t.Fatalf("expected trimmed legacy issuer, got %q", got)
	}

	t.Setenv(issuerEnv, "https://introspection.example.com/")
	if got := oauth2Issuer(); got != "https://introspection.example.com" {
		t.Fatalf("expected trimmed override issuer, got %q", got)
	}
}

func TestMainInvokesRunModuleServer(t *testing.T) {
	origRun := runModuleServer
	origBuildRuntime := buildRuntimeHook
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	t.Cleanup(func() {
		runModuleServer = origRun
		buildRuntimeHook = origBuildRuntime
		os.Args = origArgs
		flag.CommandLine = origFlagSet
	})

	var captured moduleserver.Config
	buildRuntimeHook = func(ctx context.Context) (*moduleRuntime, error) {
		return &moduleRuntime{
			registerHTTPRoutes: func(mux *http.ServeMux) {
				mux.HandleFunc("/oauth2/introspect", func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusNoContent)
				})
			},
			cleanup: func() {},
		}, nil
	}
	runModuleServer = func(cfg moduleserver.Config) error {
		captured = cfg
		return nil
	}

	os.Args = []string{"introspection-server", "-listen", "127.0.0.1:19008"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	main()

	if captured.Module != "introspection" || captured.ListenAddr != "127.0.0.1:19008" || captured.RegisterHTTPRoutes == nil {
		t.Fatalf("main did not pass expected config: %+v", captured)
	}
}

func TestBuildRuntimeRequiresDBURL(t *testing.T) {
	t.Setenv(dbURLEnv, "")
	t.Setenv(legacyDBURLEnv, "")

	runtime, err := buildRuntime(context.Background())
	if err == nil {
		t.Fatal("expected error when no introspection database URL is configured")
	}
	if runtime != nil {
		t.Fatalf("expected nil runtime, got %+v", runtime)
	}
}

func TestBuildRuntimeRejectsInvalidDBURL(t *testing.T) {
	t.Setenv(dbURLEnv, "://bad-url")
	t.Setenv(legacyDBURLEnv, "")

	runtime, err := buildRuntime(context.Background())
	if err == nil {
		t.Fatal("expected parse error for invalid database URL")
	}
	if runtime != nil {
		t.Fatalf("expected nil runtime, got %+v", runtime)
	}
}

func TestBuildRuntimePingFailure(t *testing.T) {
	t.Setenv(dbURLEnv, "postgres://user:pass@127.0.0.1:5432/aegion?sslmode=disable")
	t.Setenv(legacyDBURLEnv, "")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	runtime, err := buildRuntime(ctx)
	if err == nil {
		t.Fatal("expected ping failure with cancelled context")
	}
	if runtime != nil {
		t.Fatalf("expected nil runtime, got %+v", runtime)
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
		os.Args = []string{"introspection-server"}
		flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
		buildRuntimeHook = func(context.Context) (*moduleRuntime, error) { return nil, errors.New("runtime failed") }
		expectFatal(main, "runtime failed")
	})

	t.Run("server error", func(t *testing.T) {
		os.Args = []string{"introspection-server"}
		flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
		buildRuntimeHook = func(context.Context) (*moduleRuntime, error) {
			return &moduleRuntime{registerHTTPRoutes: func(*http.ServeMux) {}, cleanup: func() {}}, nil
		}
		runModuleServer = func(moduleserver.Config) error { return errors.New("server failed") }
		expectFatal(main, "server failed")
	})
}

func TestBuildRuntimeHookedBranches(t *testing.T) {
	origNewPool := newPoolWithConfigHook
	origPing := poolPingHook
	origClose := poolCloseHook
	t.Cleanup(func() {
		newPoolWithConfigHook = origNewPool
		poolPingHook = origPing
		poolCloseHook = origClose
	})

	t.Setenv(dbURLEnv, "postgres://user:pass@localhost:5432/aegion?sslmode=disable")
	t.Setenv(legacyDBURLEnv, "")

	t.Run("new pool error", func(t *testing.T) {
		newPoolWithConfigHook = func(context.Context, *pgxpool.Config) (*pgxpool.Pool, error) {
			return nil, errors.New("new pool failed")
		}
		_, err := buildRuntime(context.Background())
		if err == nil || err.Error() != "new pool failed" {
			t.Fatalf("buildRuntime(new pool error) = %v", err)
		}
	})

	t.Run("success cleanup", func(t *testing.T) {
		closed := false
		newPoolWithConfigHook = func(context.Context, *pgxpool.Config) (*pgxpool.Pool, error) { return nil, nil }
		poolPingHook = func(context.Context, *pgxpool.Pool) error { return nil }
		poolCloseHook = func(*pgxpool.Pool) { closed = true }
		runtime, err := buildRuntime(context.Background())
		if err != nil || runtime == nil || runtime.registerHTTPRoutes == nil || runtime.cleanup == nil {
			t.Fatalf("buildRuntime(success) runtime=%v err=%v", runtime, err)
		}
		runtime.cleanup()
		if !closed {
			t.Fatal("expected cleanup to close pool")
		}
	})
}
