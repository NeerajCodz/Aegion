package main

import (
	"context"
	"crypto/sha256"
	"flag"
	"os"
	"testing"

	"github.com/aegion/aegion/internal/platform/moduleserver"
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
	if len(cfg.GRPCServices) != 1 || cfg.GRPCServices[0] != "social.SocialEngine" {
		t.Fatalf("unexpected grpc services: %#v", cfg.GRPCServices)
	}
	if len(cfg.EventSubscriptions) != 2 || cfg.EventSubscriptions[0] != "identity.created" || cfg.EventSubscriptions[1] != "identity.updated" {
		t.Fatalf("unexpected event subscriptions: %#v", cfg.EventSubscriptions)
	}
}

func TestMainInvokesRunModuleServer(t *testing.T) {
	origRun := runModuleServer
	origArgs := os.Args
	origFlagSet := flag.CommandLine
	t.Cleanup(func() {
		runModuleServer = origRun
		os.Args = origArgs
		flag.CommandLine = origFlagSet
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
