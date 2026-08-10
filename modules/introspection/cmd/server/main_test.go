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
	"github.com/aegion/aegion/modules/oauth2/service/token"
	"github.com/jackc/pgx/v5/pgxpool"
)

type introspectionServiceFunc func(context.Context, *token.IntrospectionRequest) (*token.IntrospectionResponse, error)

func (f introspectionServiceFunc) IntrospectToken(ctx context.Context, req *token.IntrospectionRequest) (*token.IntrospectionResponse, error) {
	return f(ctx, req)
}

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

func TestClientAuthorizationBoundary(t *testing.T) {
	t.Run("conceals unauthorized token ownership", func(t *testing.T) {
		boundary := clientAuthorizationBoundary{next: introspectionServiceFunc(func(context.Context, *token.IntrospectionRequest) (*token.IntrospectionResponse, error) {
			return nil, token.ErrUnauthorizedClient
		})}
		if _, err := boundary.IntrospectToken(context.Background(), &token.IntrospectionRequest{}); !errors.Is(err, token.ErrInvalidClient) {
			t.Fatalf("ownership denial = %v, want invalid client", err)
		}
	})

	t.Run("preserves successful introspection", func(t *testing.T) {
		want := &token.IntrospectionResponse{Active: true}
		boundary := clientAuthorizationBoundary{next: introspectionServiceFunc(func(context.Context, *token.IntrospectionRequest) (*token.IntrospectionResponse, error) {
			return want, nil
		})}
		got, err := boundary.IntrospectToken(context.Background(), &token.IntrospectionRequest{})
		if err != nil || got != want {
			t.Fatalf("successful introspection = %#v, %v", got, err)
		}
	})
}

func TestModuleConfig(t *testing.T) {
	registerCalled := false
	cfg := moduleConfig("127.0.0.1:9008", func(mux *http.ServeMux) {
		registerCalled = true
		mux.HandleFunc(introspectionRoute, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
	}, func(context.Context) error { return nil })
	if cfg.Module != "introspection" || cfg.Version != moduleVersion || cfg.ListenAddr != "127.0.0.1:9008" {
		t.Fatalf("unexpected module config header: %+v", cfg)
	}
	if len(cfg.Capabilities) != 1 || cfg.Capabilities[0] != "token_introspection" {
		t.Fatalf("unexpected capabilities: %#v", cfg.Capabilities)
	}
	if len(cfg.Routes) != 1 || cfg.Routes[0] != introspectionRoute {
		t.Fatalf("unexpected routes: %#v", cfg.Routes)
	}
	if len(cfg.EventSubscriptions) != 0 || len(cfg.GRPCServices) != 0 {
		t.Fatalf("unexpected unimplemented metadata: events=%#v grpc=%#v", cfg.EventSubscriptions, cfg.GRPCServices)
	}

	mux := http.NewServeMux()
	cfg.RegisterHTTPRoutes(mux)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, introspectionRoute, nil)
	mux.ServeHTTP(rec, req)
	if !registerCalled || rec.Code != http.StatusNoContent {
		t.Fatalf("expected registered introspection route to be served, got status=%d called=%v", rec.Code, registerCalled)
	}
}

func TestOAuth2Issuer(t *testing.T) {
	t.Setenv(issuerEnv, "")
	if _, err := oauth2Issuer(); err == nil {
		t.Fatal("expected missing issuer configuration to fail")
	}

	t.Setenv(issuerEnv, "https://issuer.example.com/")
	issuer, err := oauth2Issuer()
	if err != nil || issuer != "https://issuer.example.com" {
		t.Fatalf("oauth2Issuer() = %q, %v", issuer, err)
	}

	t.Setenv(environmentEnv, "production")
	t.Setenv(issuerEnv, "http://localhost:8083")
	if _, err := oauth2Issuer(); err == nil {
		t.Fatal("expected production HTTP issuer to fail")
	}

	t.Setenv(environmentEnv, "development")
	issuer, err = oauth2Issuer()
	if err != nil || issuer != "http://localhost:8083" {
		t.Fatalf("oauth2Issuer() development loopback = %q, %v", issuer, err)
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
				mux.HandleFunc(introspectionRoute, func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusNoContent)
				})
			},
			readiness: func(context.Context) error { return nil },
			cleanup:   func() {},
		}, nil
	}
	runModuleServer = func(cfg moduleserver.Config) error {
		captured = cfg
		return nil
	}

	os.Args = []string{"introspection-server", "-listen", "127.0.0.1:19008"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	main()

	if captured.Module != "introspection" || captured.ListenAddr != "127.0.0.1:19008" || captured.RegisterHTTPRoutes == nil || captured.Readiness == nil {
		t.Fatalf("main did not pass expected config: %+v", captured)
	}
}

func TestBuildRuntimeRequiresDBURL(t *testing.T) {
	t.Setenv(dbURLEnv, "")

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

	runtime, err := buildRuntime(context.Background())
	if err == nil {
		t.Fatal("expected parse error for invalid database URL")
	}
	if runtime != nil {
		t.Fatalf("expected nil runtime, got %+v", runtime)
	}
}

func TestBuildRuntimePingFailure(t *testing.T) {
	t.Setenv(environmentEnv, "test")
	t.Setenv(dbURLEnv, "postgres://user:pass@127.0.0.1:5432/aegion?sslmode=disable")
	t.Setenv(issuerEnv, "https://issuer.example.com")

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

func TestDependencyConfigurationValidation(t *testing.T) {
	t.Setenv(environmentEnv, "production")
	if err := validateDatabaseURL("postgres://user:secret@db.example.com/aegion?sslmode=verify-full"); err != nil {
		t.Fatalf("verify-full database URL rejected: %v", err)
	}
	if err := validateDatabaseURL("postgres://user:secret@db.example.com/aegion?sslmode=disable"); err == nil {
		t.Fatal("expected non-verifying production database URL to fail")
	}

	t.Setenv(environmentEnv, "development")
	if err := validateDatabaseURL("postgres://user:secret@localhost/aegion?sslmode=disable"); err != nil {
		t.Fatalf("development loopback database URL rejected: %v", err)
	}
	if err := validateDatabaseURL("postgres://user:secret@db.example.com/aegion?sslmode=disable"); err == nil {
		t.Fatal("expected remote insecure development database URL to fail")
	}
}

func TestBoundJSONIntrospectionHandler(t *testing.T) {
	called := false
	bounded := boundJSONIntrospectionHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if _, ok := r.Context().Deadline(); !ok {
			t.Error("expected bounded request context")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	t.Run("requires JSON", func(t *testing.T) {
		called = false
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, introspectionRoute, strings.NewReader("token=x"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		bounded.ServeHTTP(rec, req)
		if called || rec.Code != http.StatusUnsupportedMediaType {
			t.Fatalf("non-JSON request status=%d called=%v", rec.Code, called)
		}
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("expected no-store error response, got %#v", rec.Header())
		}
	})

	t.Run("bounds body", func(t *testing.T) {
		called = false
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, introspectionRoute, strings.NewReader(strings.Repeat("x", maxIntrospectionRequestBytes+1)))
		req.Header.Set("Content-Type", "application/json")
		bounded.ServeHTTP(rec, req)
		if called || rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("oversized request status=%d called=%v", rec.Code, called)
		}
	})

	t.Run("forwards bounded JSON", func(t *testing.T) {
		called = false
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, introspectionRoute, strings.NewReader(`{"token":"opaque"}`))
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		bounded.ServeHTTP(rec, req)
		if !called || rec.Code != http.StatusNoContent {
			t.Fatalf("JSON request status=%d called=%v", rec.Code, called)
		}
	})
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
			return &moduleRuntime{registerHTTPRoutes: func(*http.ServeMux) {}, readiness: func(context.Context) error { return nil }, cleanup: func() {}}, nil
		}
		runModuleServer = func(moduleserver.Config) error { return errors.New("server failed") }
		expectFatal(main, "server failed")
	})
}

func TestBuildRuntimeHookedBranches(t *testing.T) {
	origNewPool := newPoolWithConfigHook
	origPing := poolPingHook
	origSchemaCheck := schemaCheckHook
	origClose := poolCloseHook
	t.Cleanup(func() {
		newPoolWithConfigHook = origNewPool
		poolPingHook = origPing
		schemaCheckHook = origSchemaCheck
		poolCloseHook = origClose
	})

	t.Setenv(environmentEnv, "test")
	t.Setenv(dbURLEnv, "postgres://user:pass@localhost:5432/aegion?sslmode=disable")
	t.Setenv(issuerEnv, "https://issuer.example.com")

	t.Run("new pool error", func(t *testing.T) {
		newPoolWithConfigHook = func(context.Context, *pgxpool.Config) (*pgxpool.Pool, error) {
			return nil, errors.New("new pool failed")
		}
		_, err := buildRuntime(context.Background())
		if err == nil || !strings.Contains(err.Error(), "new pool failed") {
			t.Fatalf("buildRuntime(new pool error) = %v", err)
		}
	})

	t.Run("schema failure closes pool", func(t *testing.T) {
		closed := false
		newPoolWithConfigHook = func(context.Context, *pgxpool.Config) (*pgxpool.Pool, error) { return nil, nil }
		poolPingHook = func(context.Context, *pgxpool.Pool) error { return nil }
		schemaCheckHook = func(context.Context, *pgxpool.Pool) error { return errors.New("schema missing") }
		poolCloseHook = func(*pgxpool.Pool) { closed = true }

		runtime, err := buildRuntime(context.Background())
		if err == nil || runtime != nil || !strings.Contains(err.Error(), "schema missing") {
			t.Fatalf("buildRuntime(schema failure) runtime=%v err=%v", runtime, err)
		}
		if !closed {
			t.Fatal("expected schema startup failure to close the database pool")
		}
	})

	t.Run("success cleanup", func(t *testing.T) {
		closed := false
		newPoolWithConfigHook = func(context.Context, *pgxpool.Config) (*pgxpool.Pool, error) { return nil, nil }
		poolPingHook = func(context.Context, *pgxpool.Pool) error { return nil }
		schemaCheckHook = func(context.Context, *pgxpool.Pool) error { return nil }
		poolCloseHook = func(*pgxpool.Pool) { closed = true }
		runtime, err := buildRuntime(context.Background())
		if err != nil || runtime == nil || runtime.registerHTTPRoutes == nil || runtime.readiness == nil || runtime.cleanup == nil {
			t.Fatalf("buildRuntime(success) runtime=%v err=%v", runtime, err)
		}

		mux := http.NewServeMux()
		runtime.registerHTTPRoutes(mux)
		legacy := httptest.NewRecorder()
		mux.ServeHTTP(legacy, httptest.NewRequest(http.MethodPost, "/oauth2/introspect", nil))
		if legacy.Code != http.StatusNotFound {
			t.Fatalf("legacy public introspection route unexpectedly exposed: status=%d", legacy.Code)
		}
		internal := httptest.NewRecorder()
		internalRequest := httptest.NewRequest(http.MethodPost, introspectionRoute, strings.NewReader(`{"token":"opaque"}`))
		mux.ServeHTTP(internal, internalRequest)
		if internal.Code != http.StatusUnsupportedMediaType {
			t.Fatalf("internal route did not enforce JSON content type: status=%d", internal.Code)
		}

		schemaCheckHook = func(context.Context, *pgxpool.Pool) error { return errors.New("schema unavailable") }
		if err := runtime.readiness(context.Background()); err == nil || !strings.Contains(err.Error(), "schema unavailable") {
			t.Fatalf("readiness did not report schema dependency failure: %v", err)
		}
		schemaCheckHook = func(context.Context, *pgxpool.Pool) error { return nil }
		if err := runtime.readiness(context.Background()); err != nil {
			t.Fatalf("readiness unexpectedly failed: %v", err)
		}
		runtime.cleanup()
		if !closed {
			t.Fatal("expected cleanup to close pool")
		}
	})
}
