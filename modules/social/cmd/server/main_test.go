package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"flag"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aegion/aegion/internal/platform/moduleserver"
	"github.com/aegion/aegion/modules/social/service"
	"github.com/aegion/aegion/modules/social/store"
)

func TestDefaultListenAddr(t *testing.T) {
	t.Setenv(listenAddrEnv, "")
	if got := defaultListenAddr(); got != defaultListen {
		t.Fatalf("expected default listen addr %q, got %q", defaultListen, got)
	}

	t.Setenv(listenAddrEnv, "127.0.0.1:19006")
	if got := defaultListenAddr(); got != "127.0.0.1:19006" {
		t.Fatalf("expected env listen addr override, got %q", got)
	}
}

func TestModuleConfig(t *testing.T) {
	cfg := moduleConfig("127.0.0.1:9006", nil)
	if cfg.Module != "social" || cfg.Version != moduleVersion || cfg.ListenAddr != "127.0.0.1:9006" {
		t.Fatalf("unexpected module config header: %+v", cfg)
	}
	if len(cfg.Capabilities) != 2 || cfg.Capabilities[0] != "oauth2_social_login" || cfg.Capabilities[1] != "social_provider_registry" {
		t.Fatalf("unexpected capabilities: %#v", cfg.Capabilities)
	}
	if len(cfg.Routes) != 3 || cfg.Routes[0] != "/self-service/social/*" || cfg.Routes[1] != "/api/v1/social/*" || cfg.Routes[2] != "/api/v1/social/admin/*" {
		t.Fatalf("unexpected routes: %#v", cfg.Routes)
	}
	if len(cfg.EventSubscriptions) != 2 || cfg.EventSubscriptions[0] != "identity.created" || cfg.EventSubscriptions[1] != "identity.updated" {
		t.Fatalf("unexpected event subscriptions: %#v", cfg.EventSubscriptions)
	}
}

func TestMainInvokesRunModuleServer(t *testing.T) {
	origRun := runModuleServer
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	origLogFatal := logFatal
	t.Cleanup(func() {
		runModuleServer = origRun
		os.Args = origArgs
		flag.CommandLine = origFlagSet
		logFatal = origLogFatal
	})

	var captured moduleserver.Config
	runModuleServer = func(cfg moduleserver.Config) error {
		captured = cfg
		return nil
	}

	os.Args = []string{"social-server", "-listen", "127.0.0.1:19006"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	main()

	if captured.Module != "social" || captured.ListenAddr != "127.0.0.1:19006" {
		t.Fatalf("main did not pass expected config: %+v", captured)
	}
	if captured.RegisterHTTPRoutes == nil {
		t.Fatal("expected social http route registrar to be set")
	}
}

func TestMainLogsFatalOnRunError(t *testing.T) {
	origArgs := os.Args
	origLogFatal := logFatal
	t.Cleanup(func() {
		os.Args = origArgs
		logFatal = origLogFatal
	})

	var got string
	logFatal = func(v ...any) {
		if len(v) > 0 {
			got = v[0].(error).Error()
		}
	}
	os.Args = []string{"social-server", "-unknown-flag"}
	main()
	if !strings.Contains(got, "flag provided but not defined") {
		t.Fatalf("expected logFatal to receive parse error, got %q", got)
	}
}

type socialRuntimeStub struct {
	ensureErr error
	upsertErr error
}

func (s *socialRuntimeStub) EnsurePresetProviders(context.Context) error { return s.ensureErr }
func (s *socialRuntimeStub) ListProviders(context.Context) ([]store.Provider, error) {
	return nil, nil
}
func (s *socialRuntimeStub) StartAuth(context.Context, string, string) (*service.StartAuthResponse, error) {
	return nil, nil
}
func (s *socialRuntimeStub) CompleteAuth(context.Context, string, string, string) (*service.CallbackResult, error) {
	return nil, nil
}
func (s *socialRuntimeStub) ListConfiguredProviders(context.Context, bool) ([]store.Provider, error) {
	return nil, nil
}
func (s *socialRuntimeStub) GetProvider(context.Context, string) (*store.Provider, error) {
	return nil, nil
}
func (s *socialRuntimeStub) UpsertProvider(context.Context, service.ProviderUpsertRequest) (*store.Provider, error) {
	return nil, s.upsertErr
}
func (s *socialRuntimeStub) DeleteProvider(context.Context, string) error { return nil }

func TestRunBranches(t *testing.T) {
	origRun := runModuleServer
	origBuildRepo := buildRepositoryHook
	origNewSvc := newSocialServiceHook
	t.Cleanup(func() {
		runModuleServer = origRun
		buildRepositoryHook = origBuildRepo
		newSocialServiceHook = origNewSvc
	})

	t.Run("build repository error", func(t *testing.T) {
		buildRepositoryHook = func(context.Context) (store.Repository, func(), error) {
			return nil, nil, errors.New("repo failed")
		}
		if err := run(nil); err == nil || err.Error() != "repo failed" {
			t.Fatalf("run(build repository error) = %v", err)
		}
	})

	t.Run("flag parse error", func(t *testing.T) {
		if err := run([]string{"-unknown-flag"}); err == nil || !strings.Contains(err.Error(), "flag provided but not defined") {
			t.Fatalf("run(flag parse error) = %v", err)
		}
	})

	t.Run("ensure providers error", func(t *testing.T) {
		buildRepositoryHook = func(context.Context) (store.Repository, func(), error) {
			return store.New(), func() {}, nil
		}
		newSocialServiceHook = func(store.Repository) runtimeSocialService {
			return &socialRuntimeStub{ensureErr: errors.New("ensure failed")}
		}
		if err := run(nil); err == nil || err.Error() != "ensure failed" {
			t.Fatalf("run(ensure providers error) = %v", err)
		}
	})

	t.Run("bootstrap env provider error", func(t *testing.T) {
		t.Setenv("AEGION_SOCIAL_GOOGLE_CLIENT_ID", "client")
		t.Setenv("AEGION_SOCIAL_GOOGLE_REDIRECT_URI", "https://app.example.com/cb")
		buildRepositoryHook = func(context.Context) (store.Repository, func(), error) {
			return store.New(), func() {}, nil
		}
		newSocialServiceHook = func(store.Repository) runtimeSocialService {
			return &socialRuntimeStub{upsertErr: errors.New("upsert failed")}
		}
		if err := run(nil); err == nil || err.Error() != "bootstrap google provider: upsert failed" {
			t.Fatalf("run(bootstrap env provider error) = %v", err)
		}
	})

	t.Run("module server error", func(t *testing.T) {
		buildRepositoryHook = func(context.Context) (store.Repository, func(), error) {
			return store.New(), func() {}, nil
		}
		newSocialServiceHook = func(store.Repository) runtimeSocialService { return &socialRuntimeStub{} }
		runModuleServer = func(moduleserver.Config) error { return errors.New("server failed") }
		if err := run([]string{"-listen", "127.0.0.1:19006"}); err == nil || err.Error() != "server failed" {
			t.Fatalf("run(module server error) = %v", err)
		}
	})
}

func TestBuildRepositoryRejectsInvalidDBURL(t *testing.T) {
	t.Setenv(dbURLEnv, "://bad-url")
	t.Setenv(legacyDBURLEnv, "")

	repo, cleanup, err := buildRepository(context.Background())
	if err == nil {
		t.Fatal("expected parse error for invalid social database URL")
	}
	if repo != nil || cleanup != nil {
		t.Fatalf("expected nil repo/cleanup on parse error, got repo=%v cleanupNil=%t", repo, cleanup == nil)
	}
}

func TestBuildRepositoryPingFailure(t *testing.T) {
	t.Setenv(dbURLEnv, "postgres://user:pass@127.0.0.1:5432/aegion?sslmode=disable")
	t.Setenv(legacyDBURLEnv, "")
	t.Setenv(cipherSecretEnv, "cipher-secret")

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

func TestBuildRepositoryHookedBranches(t *testing.T) {
	origNewPool := newPoolWithConfigHook
	origPing := poolPingHook
	origClose := poolCloseHook
	origDerive := deriveCipherKeyHook
	origNewRepo := newPostgresRepoHook
	t.Cleanup(func() {
		newPoolWithConfigHook = origNewPool
		poolPingHook = origPing
		poolCloseHook = origClose
		deriveCipherKeyHook = origDerive
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

	t.Run("derive cipher key error", func(t *testing.T) {
		newPoolWithConfigHook = func(context.Context, *pgxpool.Config) (*pgxpool.Pool, error) { return nil, nil }
		poolPingHook = func(context.Context, *pgxpool.Pool) error { return nil }
		poolCloseHook = func(*pgxpool.Pool) {}
		deriveCipherKeyHook = func() ([]byte, error) { return nil, errors.New("derive failed") }
		_, _, err := buildRepository(context.Background())
		if err == nil || err.Error() != "derive failed" {
			t.Fatalf("buildRepository(derive cipher key error) = %v", err)
		}
	})

	t.Run("new postgres repo error", func(t *testing.T) {
		newPoolWithConfigHook = func(context.Context, *pgxpool.Config) (*pgxpool.Pool, error) { return nil, nil }
		poolPingHook = func(context.Context, *pgxpool.Pool) error { return nil }
		poolCloseHook = func(*pgxpool.Pool) {}
		deriveCipherKeyHook = func() ([]byte, error) { return []byte("cipher"), nil }
		newPostgresRepoHook = func(*pgxpool.Pool, []byte) (store.Repository, error) { return nil, errors.New("repo failed") }
		_, _, err := buildRepository(context.Background())
		if err == nil || err.Error() != "repo failed" {
			t.Fatalf("buildRepository(new postgres repo error) = %v", err)
		}
	})

	t.Run("success cleanup", func(t *testing.T) {
		closed := false
		newPoolWithConfigHook = func(context.Context, *pgxpool.Config) (*pgxpool.Pool, error) { return nil, nil }
		poolPingHook = func(context.Context, *pgxpool.Pool) error { return nil }
		poolCloseHook = func(*pgxpool.Pool) { closed = true }
		deriveCipherKeyHook = func() ([]byte, error) { return []byte("cipher"), nil }
		newPostgresRepoHook = func(*pgxpool.Pool, []byte) (store.Repository, error) { return store.New(), nil }
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

func TestDeriveCipherKey(t *testing.T) {
	t.Setenv(cipherSecretEnv, "")
	t.Setenv(legacyCipherEnv, "")
	if _, err := deriveCipherKey(); err == nil {
		t.Fatal("expected error when cipher secret env vars are empty")
	}

	t.Setenv(legacyCipherEnv, "legacy-cipher")
	wantLegacy := sha256.Sum256([]byte("legacy-cipher"))
	if got, err := deriveCipherKey(); err != nil || string(got) != string(wantLegacy[:]) {
		t.Fatalf("unexpected legacy cipher key err=%v key=%x", err, got)
	}

	t.Setenv(cipherSecretEnv, "primary-cipher")
	wantPrimary := sha256.Sum256([]byte("primary-cipher"))
	if got, err := deriveCipherKey(); err != nil || string(got) != string(wantPrimary[:]) {
		t.Fatalf("unexpected primary cipher key err=%v key=%x", err, got)
	}
}

func TestEnvProviderRequest(t *testing.T) {
	t.Setenv("AEGION_SOCIAL_GOOGLE_CLIENT_ID", "")
	t.Setenv("AEGION_SOCIAL_GOOGLE_REDIRECT_URI", "")
	if req := envProviderRequest("google"); req != nil {
		t.Fatalf("expected nil request with missing required envs, got %+v", req)
	}

	t.Setenv("AEGION_SOCIAL_GOOGLE_CLIENT_ID", "google-client")
	t.Setenv("AEGION_SOCIAL_GOOGLE_CLIENT_SECRET", "google-secret")
	t.Setenv("AEGION_SOCIAL_GOOGLE_REDIRECT_URI", "https://app.example.com/callback")
	req := envProviderRequest("google")
	if req == nil {
		t.Fatal("expected envProviderRequest to return request when required envs are set")
	}
	if req.Slug != "google" || req.Preset != "google" || req.ClientID != "google-client" || req.ClientSecret != "google-secret" || req.RedirectURI != "https://app.example.com/callback" {
		t.Fatalf("unexpected provider request: %+v", req)
	}
	if !req.Enabled || !req.TrustEmailVerified {
		t.Fatalf("expected provider defaults enabled/trust-email-verified, got %+v", req)
	}
}

func TestNewPostgresRepoHookDefault(t *testing.T) {
	orig := newPostgresRepoHook
	key := make([]byte, 32)
	if _, err := orig(nil, key); err == nil || err.Error() != "postgres pool is required" {
		t.Fatalf("newPostgresRepoHook(nil pool) err=%v", err)
	}
}
