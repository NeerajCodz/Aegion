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
	"time"

	coresession "github.com/aegion/aegion/core/session"
	"github.com/aegion/aegion/internal/platform/egress"
	"github.com/aegion/aegion/internal/platform/moduleserver"
	"github.com/aegion/aegion/modules/proxy/store"
	"github.com/aegion/aegion/modules/proxy/service"
	"github.com/google/uuid"
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

func TestModuleConfigDeclaresOnlyImplementedCoreSurface(t *testing.T) {
	cfg := moduleConfig("127.0.0.1:9009", func(*http.ServeMux) {})
	if cfg.Module != "proxy" || cfg.Version != moduleVersion || cfg.ListenAddr != "127.0.0.1:9009" {
		t.Fatalf("unexpected module config header: %+v", cfg)
	}
	if got, want := cfg.Capabilities, []string{"proxy_rule_registry"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("unexpected capabilities: %#v", got)
	}
	if len(cfg.Routes) != 0 || len(cfg.GRPCServices) != 0 || len(cfg.EventSubscriptions) != 0 {
		t.Fatalf("proxy must not declare a core-public route, gRPC service, or event subscription: %+v", cfg)
	}
	if cfg.RegisterHTTPRoutes == nil {
		t.Fatal("expected internal HTTP route registration hook")
	}
}

func TestBuildRuntimeRejectsMissingDurableConfiguration(t *testing.T) {
	t.Setenv(dbURLEnv, "")

	runtime, err := buildRuntime(context.Background())
	if err == nil || runtime != nil {
		t.Fatalf("buildRuntime(missing config) runtime=%v err=%v", runtime, err)
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
			readiness:          func(context.Context) error { return nil },
			cleanup:            func() {},
		}, nil
	}

	os.Args = []string{"proxy-server", "-listen", "127.0.0.1:19009"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	main()

	if captured.Module != "proxy" || captured.ListenAddr != "127.0.0.1:19009" || captured.Readiness == nil {
		t.Fatalf("main did not pass expected config: %+v", captured)
	}
}

func TestBuildRepositoryRejectsInvalidDBURL(t *testing.T) {
	t.Setenv(dbURLEnv, "://bad-url")

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
	t.Setenv(managementTokenEnv, strings.Repeat("m", minimumSecretLength))
	t.Setenv(egressAllowedHostsEnv, "example.com")
	t.Setenv(dbURLEnv, "://bad-url")

	runtime, err := buildRuntime(context.Background())
	if err == nil || runtime != nil {
		t.Fatalf("buildRuntime(parse error) runtime=%v err=%v", runtime, err)
	}
}

func TestBuildRepositoryRejectsMissingDatabaseURL(t *testing.T) {
	t.Setenv(dbURLEnv, "")
	repo, cleanup, err := buildRepository(context.Background())
	if err == nil || repo != nil || cleanup != nil {
		t.Fatalf("buildRepository(no db) repo=%v cleanupNil=%t err=%v", repo, cleanup == nil, err)
	}
}

func TestValidatingRepositoryRejectsUnsafeUpstream(t *testing.T) {
	egressClient, err := egress.NewClient(egress.Policy{AllowedHosts: []string{"example.com"}})
	if err != nil {
		t.Fatalf("new egress client: %v", err)
	}
	repo := &validatingRepository{Repository: store.New(), egressClient: egressClient}
	_, err = repo.UpsertUpstream(context.Background(), store.Upstream{Name: "unsafe", URL: "http://example.com"})
	if err == nil {
		t.Fatal("expected HTTP upstream to be rejected")
	}
}

func TestValidatingRepositoryRejectsProtectedDataPlaneRoute(t *testing.T) {
	repo := &validatingRepository{Repository: store.New()}
	_, err := repo.UpsertRoute(context.Background(), store.Route{ID: "claim-core", Path: "/internal/*", Target: "upstream", Enabled: true})
	if !errors.Is(err, service.ErrInvalidProxyConfig) {
		t.Fatalf("expected protected path to be rejected, got %v", err)
	}
}

func TestDataPlaneAcceptsOnlyVerifiedSessionContext(t *testing.T) {
	secret := []byte(strings.Repeat("s", minimumSecretLength))
	plane := &dataPlane{sessionContextSecret: secret}
	request := httptest.NewRequest(http.MethodGet, "https://proxy.example/data", nil)
	request.Header.Set(coresession.HeaderPrefix+"Session-ID", uuid.NewString())
	if _, err := plane.withTrustedSession(request); err == nil {
		t.Fatal("expected unsigned session context to be rejected")
	}

	request = httptest.NewRequest(http.MethodGet, "https://proxy.example/data", nil)
	original := &coresession.Session{ID: uuid.New(), IdentityID: uuid.New(), AAL: coresession.AAL2}
	coresession.InjectHeaders(request, original, secret)
	trusted, err := plane.withTrustedSession(request)
	if err != nil {
		t.Fatalf("verify signed session context: %v", err)
	}
	session := coresession.FromContext(trusted.Context())
	if session == nil || session.ID != original.ID || session.IdentityID != original.IdentityID || !session.Active {
		t.Fatalf("trusted session = %#v, want verified active session", session)
	}
	if trusted.Header.Get(coresession.HeaderPrefix+"Signature") != "" {
		t.Fatal("verified context headers must not reach the configured upstream")
	}
}

func TestCompileProxyConfigRejectsProtectedPaths(t *testing.T) {
	egressClient, err := egress.NewClient(egress.Policy{AllowedHosts: []string{"example.com"}})
	if err != nil {
		t.Fatalf("new egress client: %v", err)
	}
	_, _, err = compileProxyConfig(context.Background(), &service.EffectiveConfig{Routes: []store.Route{{
		ID:      "take-core",
		Path:    "/api/v1/auth/*",
		Target:  "nope",
		Enabled: true,
	}}}, egressClient, strings.Repeat("u", minimumSecretLength))
	if err == nil || !strings.Contains(err.Error(), "protected core path") {
		t.Fatalf("compileProxyConfig protected path error = %v", err)
	}
}

func TestConfiguredReloadInterval(t *testing.T) {
	t.Setenv(configReloadIntervalEnv, "1500ms")
	got, err := configuredReloadInterval()
	if err != nil || got != 1500*time.Millisecond {
		t.Fatalf("configuredReloadInterval() = %v, %v", got, err)
	}
	t.Setenv(configReloadIntervalEnv, "100ms")
	if _, err := configuredReloadInterval(); err == nil {
		t.Fatal("expected sub-second reload interval to be rejected")
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

	t.Run("new pool error", func(t *testing.T) {
		newPoolWithConfigHook = func(context.Context, *pgxpool.Config) (*pgxpool.Pool, error) {
			return nil, errors.New("new pool failed")
		}
		_, _, err := buildRepository(context.Background())
		if err == nil || !strings.Contains(err.Error(), "new pool failed") {
			t.Fatalf("buildRepository(new pool error) = %v", err)
		}
	})

	t.Run("new repository error", func(t *testing.T) {
		newPoolWithConfigHook = func(context.Context, *pgxpool.Config) (*pgxpool.Pool, error) { return nil, nil }
		poolPingHook = func(context.Context, *pgxpool.Pool) error { return nil }
		poolCloseHook = func(*pgxpool.Pool) {}
		newPostgresRepoHook = func(*pgxpool.Pool) (*store.PostgresStore, error) { return nil, errors.New("repo failed") }
		_, _, err := buildRepository(context.Background())
		if err == nil || !strings.Contains(err.Error(), "repo failed") {
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
