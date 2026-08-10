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

const testSecret = "0123456789abcdef0123456789abcdef"

func setMountedIdentitySigningSecret(t *testing.T) {
	t.Helper()
	path := t.TempDir() + "/identity-signing-secret"
	if err := os.WriteFile(path, []byte(testSecret+"\n"), 0o600); err != nil {
		t.Fatalf("write identity signing secret: %v", err)
	}
	t.Setenv(identitySigningSecretFileEnv, path)
}

func TestDefaultListenAddr(t *testing.T) {
	t.Setenv(listenAddrEnv, "")
	if got := defaultListenAddr(); got != defaultListen {
		t.Fatalf("defaultListenAddr() = %q, want %q", got, defaultListen)
	}

	t.Setenv(listenAddrEnv, "127.0.0.1:19006")
	if got := defaultListenAddr(); got != "127.0.0.1:19006" {
		t.Fatalf("defaultListenAddr() = %q, want override", got)
	}
}

func TestModuleConfigReportsOnlyInstalledAPIs(t *testing.T) {
	cfg := moduleConfig("127.0.0.1:9006", nil)
	if cfg.Module != "social" || cfg.Version != moduleVersion || cfg.ListenAddr != "127.0.0.1:9006" {
		t.Fatalf("unexpected module config header: %+v", cfg)
	}
	if got, want := strings.Join(cfg.Capabilities, ","), "oauth2_social_login,social_provider_registry"; got != want {
		t.Fatalf("capabilities = %q, want %q", got, want)
	}
	if got, want := strings.Join(cfg.Routes, ","), "/self-service/social/*,/api/v1/social/*"; got != want {
		t.Fatalf("routes = %q, want %q", got, want)
	}
	if len(cfg.GRPCServices) != 0 {
		t.Fatalf("gRPC services = %#v, want none", cfg.GRPCServices)
	}
	if len(cfg.EventSubscriptions) != 0 {
		t.Fatalf("event subscriptions = %#v, want none", cfg.EventSubscriptions)
	}
}

func TestMainComposesDurableRuntime(t *testing.T) {
	origRun := runModuleServer
	origBuildRepo := buildRepositoryHook
	origNewSvc := newSocialServiceHook
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	origLogFatal := logFatal
	t.Cleanup(func() {
		runModuleServer = origRun
		buildRepositoryHook = origBuildRepo
		newSocialServiceHook = origNewSvc
		os.Args = origArgs
		flag.CommandLine = origFlagSet
		logFatal = origLogFatal
	})
	t.Setenv(managementTokenEnv, testSecret)
	setMountedIdentitySigningSecret(t)

	svc := &socialRuntimeStub{}
	buildRepositoryHook = func(context.Context) (store.Repository, func(), error) {
		return store.New(), func() {}, nil
	}
	newSocialServiceHook = func(store.Repository) runtimeSocialService { return svc }

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
		t.Fatal("expected social route registrar")
	}
	if captured.Readiness == nil {
		t.Fatal("expected dependency readiness check")
	}
	if err := captured.Readiness(context.Background()); err != nil {
		t.Fatalf("readiness() = %v, want nil", err)
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
		t.Fatalf("logFatal error = %q, want flag parse error", got)
	}
}

type socialRuntimeStub struct {
	ensureErr error
	listErr   error
	upsertErr error
}

func (s *socialRuntimeStub) EnsurePresetProviders(context.Context) error { return s.ensureErr }
func (s *socialRuntimeStub) ListProviders(context.Context) ([]store.Provider, error) {
	return nil, s.listErr
}
func (s *socialRuntimeStub) StartAuth(context.Context, string, string) (*service.StartAuthResponse, error) {
	return nil, nil
}
func (s *socialRuntimeStub) CompleteAuth(context.Context, string, string, string) (*service.CallbackResult, error) {
	return nil, nil
}
func (s *socialRuntimeStub) ListConfiguredProviders(context.Context, bool) ([]store.Provider, error) {
	return nil, s.listErr
}
func (s *socialRuntimeStub) GetProvider(context.Context, string) (*store.Provider, error) {
	return nil, nil
}
func (s *socialRuntimeStub) UpsertProvider(context.Context, service.ProviderUpsertRequest) (*store.Provider, error) {
	return nil, s.upsertErr
}
func (s *socialRuntimeStub) DeleteProvider(context.Context, string) error { return nil }

func TestRunFailsClosedBeforeStarting(t *testing.T) {
	origRun := runModuleServer
	origBuildRepo := buildRepositoryHook
	origNewSvc := newSocialServiceHook
	t.Cleanup(func() {
		runModuleServer = origRun
		buildRepositoryHook = origBuildRepo
		newSocialServiceHook = origNewSvc
	})
	setMountedIdentitySigningSecret(t)

	t.Run("missing management token", func(t *testing.T) {
		t.Setenv(managementTokenEnv, "")
		if err := run(nil); err == nil || !strings.Contains(err.Error(), managementTokenEnv) {
			t.Fatalf("run() = %v, want missing management token error", err)
		}
	})

	t.Run("missing mounted signing secret", func(t *testing.T) {
		t.Setenv(managementTokenEnv, testSecret)
		t.Setenv(identitySigningSecretFileEnv, "")
		if err := run(nil); err == nil || !strings.Contains(err.Error(), identitySigningSecretFileEnv) {
			t.Fatalf("run() = %v, want missing signing secret error", err)
		}
	})

	t.Run("repository error", func(t *testing.T) {
		t.Setenv(managementTokenEnv, testSecret)
		buildRepositoryHook = func(context.Context) (store.Repository, func(), error) {
			return nil, nil, errors.New("repo failed")
		}
		if err := run(nil); err == nil || err.Error() != "repo failed" {
			t.Fatalf("run() = %v, want repository error", err)
		}
	})

	t.Run("preset bootstrap error", func(t *testing.T) {
		t.Setenv(managementTokenEnv, testSecret)
		buildRepositoryHook = func(context.Context) (store.Repository, func(), error) {
			return store.New(), func() {}, nil
		}
		newSocialServiceHook = func(store.Repository) runtimeSocialService {
			return &socialRuntimeStub{ensureErr: errors.New("ensure failed")}
		}
		if err := run(nil); err == nil || err.Error() != "ensure failed" {
			t.Fatalf("run() = %v, want preset error", err)
		}
	})

	t.Run("environment provider error", func(t *testing.T) {
		t.Setenv(managementTokenEnv, testSecret)
		t.Setenv("AEGION_SOCIAL_GOOGLE_CLIENT_ID", "client")
		t.Setenv("AEGION_SOCIAL_GOOGLE_REDIRECT_URI", "https://app.example.com/api/v1/social/google/callback")
		buildRepositoryHook = func(context.Context) (store.Repository, func(), error) {
			return store.New(), func() {}, nil
		}
		newSocialServiceHook = func(store.Repository) runtimeSocialService {
			return &socialRuntimeStub{upsertErr: errors.New("upsert failed")}
		}
		if err := run(nil); err == nil || err.Error() != "bootstrap google provider: upsert failed" {
			t.Fatalf("run() = %v, want provider bootstrap error", err)
		}
	})

	t.Run("module server error", func(t *testing.T) {
		t.Setenv(managementTokenEnv, testSecret)
		buildRepositoryHook = func(context.Context) (store.Repository, func(), error) {
			return store.New(), func() {}, nil
		}
		newSocialServiceHook = func(store.Repository) runtimeSocialService { return &socialRuntimeStub{} }
		runModuleServer = func(moduleserver.Config) error { return errors.New("server failed") }
		if err := run([]string{"-listen", "127.0.0.1:19006"}); err == nil || err.Error() != "server failed" {
			t.Fatalf("run() = %v, want server error", err)
		}
	})
}

func TestBuildRepositoryRequiresConfiguredDatabase(t *testing.T) {
	t.Setenv(dbURLEnv, "")
	repo, cleanup, err := buildRepository(context.Background())
	if err == nil || !strings.Contains(err.Error(), dbURLEnv) {
		t.Fatalf("buildRepository() = %v, want missing database configuration error", err)
	}
	if repo != nil || cleanup != nil {
		t.Fatalf("buildRepository() returned repo=%v cleanupNil=%t on config failure", repo, cleanup == nil)
	}
}

func TestBuildRepositoryRejectsInvalidDatabaseURL(t *testing.T) {
	t.Setenv(dbURLEnv, "://bad-url")

	repo, cleanup, err := buildRepository(context.Background())
	if err == nil {
		t.Fatal("expected parse error for invalid social database URL")
	}
	if repo != nil || cleanup != nil {
		t.Fatalf("expected nil repo/cleanup on parse error, got repo=%v cleanupNil=%t", repo, cleanup == nil)
	}
}

func TestBuildRepositoryHookedBranches(t *testing.T) {
	origNewPool := newPoolWithConfigHook
	origPing := poolPingHook
	origSchema := schemaCheckHook
	origClose := poolCloseHook
	origDerive := deriveCipherKeyHook
	origNewRepo := newPostgresRepoHook
	t.Cleanup(func() {
		newPoolWithConfigHook = origNewPool
		poolPingHook = origPing
		schemaCheckHook = origSchema
		poolCloseHook = origClose
		deriveCipherKeyHook = origDerive
		newPostgresRepoHook = origNewRepo
	})

	t.Setenv(dbURLEnv, "postgres://user:pass@localhost:5432/aegion?sslmode=disable")
	t.Setenv(cipherSecretEnv, testSecret)

	t.Run("new pool error", func(t *testing.T) {
		newPoolWithConfigHook = func(context.Context, *pgxpool.Config) (*pgxpool.Pool, error) {
			return nil, errors.New("new pool failed")
		}
		_, _, err := buildRepository(context.Background())
		if err == nil || !strings.Contains(err.Error(), "new pool failed") {
			t.Fatalf("buildRepository() = %v, want connection error", err)
		}
	})

	t.Run("ping failure closes pool", func(t *testing.T) {
		closed := false
		newPoolWithConfigHook = func(context.Context, *pgxpool.Config) (*pgxpool.Pool, error) { return nil, nil }
		poolPingHook = func(context.Context, *pgxpool.Pool) error { return errors.New("ping failed") }
		poolCloseHook = func(*pgxpool.Pool) { closed = true }
		_, _, err := buildRepository(context.Background())
		if err == nil || !strings.Contains(err.Error(), "ping failed") || !closed {
			t.Fatalf("buildRepository() = %v, closed=%t; want ping failure and close", err, closed)
		}
	})

	t.Run("schema failure closes pool", func(t *testing.T) {
		closed := false
		newPoolWithConfigHook = func(context.Context, *pgxpool.Config) (*pgxpool.Pool, error) { return nil, nil }
		poolPingHook = func(context.Context, *pgxpool.Pool) error { return nil }
		schemaCheckHook = func(context.Context, *pgxpool.Pool) error { return errors.New("schema missing") }
		poolCloseHook = func(*pgxpool.Pool) { closed = true }
		_, _, err := buildRepository(context.Background())
		if err == nil || err.Error() != "schema missing" || !closed {
			t.Fatalf("buildRepository() = %v, closed=%t; want schema failure and close", err, closed)
		}
	})

	t.Run("cipher failure closes pool", func(t *testing.T) {
		closed := false
		newPoolWithConfigHook = func(context.Context, *pgxpool.Config) (*pgxpool.Pool, error) { return nil, nil }
		poolPingHook = func(context.Context, *pgxpool.Pool) error { return nil }
		schemaCheckHook = func(context.Context, *pgxpool.Pool) error { return nil }
		poolCloseHook = func(*pgxpool.Pool) { closed = true }
		deriveCipherKeyHook = func() ([]byte, error) { return nil, errors.New("derive failed") }
		_, _, err := buildRepository(context.Background())
		if err == nil || err.Error() != "derive failed" || !closed {
			t.Fatalf("buildRepository() = %v, closed=%t; want cipher failure and close", err, closed)
		}
	})

	t.Run("repository failure closes pool", func(t *testing.T) {
		closed := false
		newPoolWithConfigHook = func(context.Context, *pgxpool.Config) (*pgxpool.Pool, error) { return nil, nil }
		poolPingHook = func(context.Context, *pgxpool.Pool) error { return nil }
		schemaCheckHook = func(context.Context, *pgxpool.Pool) error { return nil }
		poolCloseHook = func(*pgxpool.Pool) { closed = true }
		deriveCipherKeyHook = func() ([]byte, error) { return make([]byte, sha256.Size), nil }
		newPostgresRepoHook = func(*pgxpool.Pool, []byte) (store.Repository, error) {
			return nil, errors.New("repository failed")
		}
		_, _, err := buildRepository(context.Background())
		if err == nil || err.Error() != "repository failed" || !closed {
			t.Fatalf("buildRepository() = %v, closed=%t; want repository failure and close", err, closed)
		}
	})

	t.Run("success cleanup", func(t *testing.T) {
		closed := false
		newPoolWithConfigHook = func(context.Context, *pgxpool.Config) (*pgxpool.Pool, error) { return nil, nil }
		poolPingHook = func(context.Context, *pgxpool.Pool) error { return nil }
		schemaCheckHook = func(context.Context, *pgxpool.Pool) error { return nil }
		poolCloseHook = func(*pgxpool.Pool) { closed = true }
		deriveCipherKeyHook = func() ([]byte, error) { return make([]byte, sha256.Size), nil }
		newPostgresRepoHook = func(*pgxpool.Pool, []byte) (store.Repository, error) {
			return store.New(), nil
		}
		repo, cleanup, err := buildRepository(context.Background())
		if err != nil || repo == nil || cleanup == nil {
			t.Fatalf("buildRepository() repo=%v cleanupNil=%t err=%v", repo, cleanup == nil, err)
		}
		cleanup()
		if !closed {
			t.Fatal("expected cleanup to close pool")
		}
	})
}

func TestDeriveCipherKeyRequiresStrongSecret(t *testing.T) {
	t.Setenv(cipherSecretEnv, "")
	if _, err := deriveCipherKey(); err == nil {
		t.Fatal("expected error when cipher secret is empty")
	}

	t.Setenv(cipherSecretEnv, strings.Repeat("x", sha256.Size-1))
	if _, err := deriveCipherKey(); err == nil {
		t.Fatal("expected error for short cipher secret")
	}

	t.Setenv(cipherSecretEnv, testSecret)
	want := sha256.Sum256([]byte(testSecret))
	if got, err := deriveCipherKey(); err != nil || string(got) != string(want[:]) {
		t.Fatalf("deriveCipherKey() err=%v key=%x, want %x", err, got, want)
	}
}

func TestRequiredManagementToken(t *testing.T) {
	t.Setenv(managementTokenEnv, "")
	if _, err := requiredManagementToken(); err == nil {
		t.Fatal("expected missing management token error")
	}

	t.Setenv(managementTokenEnv, strings.Repeat("x", sha256.Size-1))
	if _, err := requiredManagementToken(); err == nil {
		t.Fatal("expected short management token error")
	}

	t.Setenv(managementTokenEnv, testSecret)
	if got, err := requiredManagementToken(); err != nil || got != testSecret {
		t.Fatalf("requiredManagementToken() = %q, %v", got, err)
	}
}

func TestReadIdentitySigningSecretRequiresMountedStrongValue(t *testing.T) {
	t.Setenv(identitySigningSecretFileEnv, "")
	if _, err := readIdentitySigningSecret(); err == nil {
		t.Fatal("expected missing signing secret file error")
	}

	path := t.TempDir() + "/identity-signing-secret"
	t.Setenv(identitySigningSecretFileEnv, path)
	if _, err := readIdentitySigningSecret(); err == nil {
		t.Fatal("expected unreadable signing secret file error")
	}

	if err := os.WriteFile(path, []byte("short"), 0o600); err != nil {
		t.Fatalf("write short signing secret: %v", err)
	}
	if _, err := readIdentitySigningSecret(); err == nil {
		t.Fatal("expected short signing secret error")
	}

	if err := os.WriteFile(path, []byte(testSecret+"\n"), 0o600); err != nil {
		t.Fatalf("write signing secret: %v", err)
	}
	if got, err := readIdentitySigningSecret(); err != nil || string(got) != testSecret {
		t.Fatalf("readIdentitySigningSecret() = %q, %v", got, err)
	}
}

func TestEnvProviderRequestIsCompleteAndSafe(t *testing.T) {
	const prefix = "AEGION_SOCIAL_GOOGLE_"
	t.Setenv(prefix+"CLIENT_ID", "")
	t.Setenv(prefix+"REDIRECT_URI", "")
	if _, configured, err := envProviderRequest("google"); err != nil || configured {
		t.Fatalf("envProviderRequest() configured=%t err=%v, want absent provider", configured, err)
	}

	t.Setenv(prefix+"CLIENT_ID", "google-client")
	if _, _, err := envProviderRequest("google"); err == nil {
		t.Fatal("expected incomplete provider configuration error")
	}

	t.Setenv(prefix+"REDIRECT_URI", "http://attacker.example/callback")
	if _, _, err := envProviderRequest("google"); err == nil {
		t.Fatal("expected unsafe callback URI error")
	}

	t.Setenv(prefix+"REDIRECT_URI", "https://app.example.com/api/v1/social/google/callback")
	t.Setenv(prefix+"TRUST_EMAIL_VERIFIED", "")
	req, configured, err := envProviderRequest("google")
	if err != nil || !configured {
		t.Fatalf("envProviderRequest() configured=%t err=%v", configured, err)
	}
	if req.Slug != "google" || req.Preset != "google" || req.ClientID != "google-client" {
		t.Fatalf("unexpected provider request: %+v", req)
	}
	if req.TrustEmailVerified {
		t.Fatalf("trust_email_verified = true, want explicit opt-in")
	}

	t.Setenv(prefix+"TRUST_EMAIL_VERIFIED", "true")
	req, configured, err = envProviderRequest("google")
	if err != nil || !configured || !req.TrustEmailVerified {
		t.Fatalf("explicit trust setting configured=%t req=%+v err=%v", configured, req, err)
	}

	t.Setenv(prefix+"TRUST_EMAIL_VERIFIED", "not-a-bool")
	if _, _, err := envProviderRequest("google"); err == nil {
		t.Fatal("expected invalid trust setting error")
	}
}

func TestValidateCallbackRedirectURI(t *testing.T) {
	for _, raw := range []string{
		"https://app.example.com/api/v1/social/google/callback",
		"https://app.example.com/self-service/social/google/callback",
		"http://127.0.0.1:9006/api/v1/social/google/callback",
		"http://localhost:9006/self-service/social/google/callback",
	} {
		if err := validateCallbackRedirectURI("google", raw); err != nil {
			t.Fatalf("validateCallbackRedirectURI(%q) = %v, want nil", raw, err)
		}
	}
	for _, raw := range []string{
		"https://app.example.com/api/v1/social/google/callback#fragment",
		"https://user@app.example.com/api/v1/social/google/callback",
		"https://app.example.com/api/v1/social/github/callback",
		"http://app.example.com/api/v1/social/google/callback",
	} {
		if err := validateCallbackRedirectURI("google", raw); err == nil {
			t.Fatalf("validateCallbackRedirectURI(%q) succeeded, want error", raw)
		}
	}
}

func TestSocialReadinessFailsWhenProviderStoreFails(t *testing.T) {
	if err := socialReadiness(nil)(context.Background()); err == nil {
		t.Fatal("expected nil service readiness failure")
	}
	if err := socialReadiness(&socialRuntimeStub{listErr: errors.New("database unavailable")})(context.Background()); err == nil {
		t.Fatal("expected provider store readiness failure")
	}
}

func TestCheckSchemaRejectsNilPool(t *testing.T) {
	if err := checkSchema(context.Background(), nil); err == nil {
		t.Fatal("expected nil pool schema failure")
	}
}
