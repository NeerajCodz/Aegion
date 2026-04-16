package main

import (
	"context"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/aegion/aegion/internal/platform/moduleserver"
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
