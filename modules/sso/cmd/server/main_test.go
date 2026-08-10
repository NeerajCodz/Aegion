package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"flag"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aegion/aegion/internal/platform/moduleserver"
	"github.com/aegion/aegion/modules/sso/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

func configureRuntimeEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv(stateSecretEnv, strings.Repeat("s", minSecretBytes))
	t.Setenv(coreGRPCAddrEnv, "core.internal:9443")
	t.Setenv(moduleCredentialFileEnv, "/mounted/module-credential")
	t.Setenv(moduleTLSCertFileEnv, "/mounted/tls.crt")
	t.Setenv(moduleTLSKeyFileEnv, "/mounted/tls.key")
	t.Setenv(moduleCACertFileEnv, "/mounted/ca.crt")

	secretPath := filepath.Join(t.TempDir(), "identity-signing-secret")
	if err := os.WriteFile(secretPath, []byte(strings.Repeat("i", minSecretBytes)), 0o600); err != nil {
		t.Fatalf("write identity signing secret: %v", err)
	}
	t.Setenv(identitySigningSecretFileEnv, secretPath)
}

func TestDefaultListenAddr(t *testing.T) {
	t.Setenv(listenAddrEnv, "")
	if got := defaultListenAddr(); got != defaultListen {
		t.Fatalf("expected default listen addr %q, got %q", defaultListen, got)
	}
}

func TestModuleConfig(t *testing.T) {
	ready := func(context.Context) error { return nil }
	cfg := moduleConfig("127.0.0.1:9007", func(*http.ServeMux) {}, ready)
	if cfg.Module != "sso" || cfg.Version != moduleVersion || cfg.ListenAddr != "127.0.0.1:9007" {
		t.Fatalf("unexpected module config: %+v", cfg)
	}
	if len(cfg.Capabilities) != 3 || cfg.Capabilities[1] != "connection_registry" {
		t.Fatalf("unexpected capabilities: %#v", cfg.Capabilities)
	}
	if cfg.Readiness == nil || len(cfg.EventSubscriptions) != 0 || len(cfg.GRPCServices) != 0 {
		t.Fatalf("metadata must only describe installed runtime behavior: %+v", cfg)
	}
	if got, want := cfg.Routes, []string{
		"/api/v1/sso/connections",
		"/api/v1/sso/resolve-domain",
		"/api/v1/sso/admin/connections",
		"/api/v1/sso/admin/connections/{slug}",
		"/self-service/sso/{connection}/start",
		"/self-service/sso/{connection}/callback",
	}; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("routes=%v, want %v", got, want)
	}
}

func TestBuildRuntimeRejectsMissingRequiredConfiguration(t *testing.T) {
	t.Setenv(stateSecretEnv, "")
	runtime, err := buildRuntime(context.Background())
	if err == nil || runtime != nil || !strings.Contains(err.Error(), "SSO state secret") {
		t.Fatalf("buildRuntime() runtime=%v err=%v, want missing state-secret error", runtime, err)
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
			readiness:          func(context.Context) error { return nil },
			cleanup:            func() {},
		}, nil
	}
	os.Args = []string{"sso-server", "-listen", "127.0.0.1:19007"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	main()
	if captured.Module != "sso" || captured.ListenAddr != "127.0.0.1:19007" || captured.Readiness == nil {
		t.Fatalf("unexpected config passed to run: %+v", captured)
	}
}

func TestBuildRepositoryRejectsInvalidDBURL(t *testing.T) {
	t.Setenv(dbURLEnv, "://bad-url")
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
	configureRuntimeEnvironment(t)
	t.Setenv(dbURLEnv, "://bad-url")
	runtime, err := buildRuntime(context.Background())
	if err == nil || runtime != nil {
		t.Fatalf("buildRuntime(parse error) runtime=%v err=%v", runtime, err)
	}
}

func TestBuildRepositoryRejectsMissingDBURL(t *testing.T) {
	t.Setenv(dbURLEnv, "")
	repo, cleanup, err := buildRepository(context.Background())
	if err == nil || repo != nil || cleanup != nil {
		t.Fatalf("buildRepository(no db) repo=%v cleanupNil=%t err=%v", repo, cleanup == nil, err)
	}
}

func TestDeriveStateSecret(t *testing.T) {
	t.Setenv(stateSecretEnv, "")
	if got, err := deriveStateSecret(); err == nil || got != nil {
		t.Fatalf("deriveStateSecret() secret=%x err=%v, want missing-secret error", got, err)
	}

	raw := strings.Repeat("s", minSecretBytes)
	t.Setenv(stateSecretEnv, raw)
	want := sha256.Sum256([]byte(raw))
	got, err := deriveStateSecret()
	if err != nil || len(got) != sha256.Size || string(got) != string(want[:]) {
		t.Fatalf("unexpected derived secret: %x, %v", got, err)
	}
}

func TestReadMountedSecret(t *testing.T) {
	if got, err := readMountedSecret(identitySigningSecretFileEnv); err == nil || got != nil {
		t.Fatalf("readMountedSecret(missing) secret=%x err=%v", got, err)
	}

	secretPath := filepath.Join(t.TempDir(), "identity-signing-secret")
	raw := strings.Repeat("i", minSecretBytes)
	if err := os.WriteFile(secretPath, []byte("\n"+raw+"\n"), 0o600); err != nil {
		t.Fatalf("write identity signing secret: %v", err)
	}
	t.Setenv(identitySigningSecretFileEnv, secretPath)
	got, err := readMountedSecret(identitySigningSecretFileEnv)
	if err != nil || string(got) != raw {
		t.Fatalf("readMountedSecret() secret=%q err=%v", got, err)
	}
}

func TestDatabaseReadiness(t *testing.T) {
	origPing := poolPingHook
	origSchemaCheck := checkSSOSchemaHook
	t.Cleanup(func() {
		poolPingHook = origPing
		checkSSOSchemaHook = origSchemaCheck
	})

	checkSSOSchemaHook = func(context.Context, *pgxpool.Pool) error { return nil }
	poolPingHook = func(context.Context, *pgxpool.Pool) error { return nil }
	if err := databaseReadiness(nil)(context.Background()); err != nil {
		t.Fatalf("databaseReadiness() error = %v", err)
	}

	poolPingHook = func(context.Context, *pgxpool.Pool) error { return errors.New("database unavailable") }
	if err := databaseReadiness(nil)(context.Background()); err == nil || !strings.Contains(err.Error(), "ping sso database") {
		t.Fatalf("databaseReadiness(ping failure) error = %v", err)
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
