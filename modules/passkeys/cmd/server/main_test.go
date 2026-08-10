package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"
	"github.com/aegion/aegion/internal/platform/moduleserver"
	"github.com/aegion/aegion/modules/passkeys/service"
	"github.com/aegion/aegion/modules/passkeys/store"
)

func TestDefaultListenAddr(t *testing.T) {
	t.Setenv(listenAddrEnv, "")
	if got := defaultListenAddr(); got != defaultListen {
		t.Fatalf("expected default listen addr %q, got %q", defaultListen, got)
	}

	t.Setenv(listenAddrEnv, "127.0.0.1:19004")
	if got := defaultListenAddr(); got != "127.0.0.1:19004" {
		t.Fatalf("expected env listen addr override, got %q", got)
	}
}

func TestPasskeyConfigRequiresFixedHTTPSOrigin(t *testing.T) {
	t.Run("accepts a fixed HTTPS origin for the configured RP ID", func(t *testing.T) {
		t.Setenv(rpIDEnv, "example.test")
		t.Setenv(rpOriginEnv, "https://login.example.test")

		cfg, err := passkeyConfig()
		if err != nil {
			t.Fatalf("passkeyConfig() error = %v", err)
		}
		if cfg.RPID != "example.test" || cfg.RPOrigin != "https://login.example.test" {
			t.Fatalf("unexpected passkey config: %+v", cfg)
		}
	})

	for name, tc := range map[string]struct {
		rpID   string
		origin string
	}{
		"missing RP ID":                {origin: "https://login.example.test"},
		"non HTTPS origin":             {rpID: "example.test", origin: "http://login.example.test"},
		"origin outside RP ID":          {rpID: "example.test", origin: "https://other.test"},
		"origin with a path":            {rpID: "example.test", origin: "https://login.example.test/passkeys"},
		"IP-address RP ID":              {rpID: "127.0.0.1", origin: "https://127.0.0.1"},
		"credential-bearing origin URL": {rpID: "example.test", origin: "https://user:password@login.example.test"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(rpIDEnv, tc.rpID)
			t.Setenv(rpOriginEnv, tc.origin)
			if _, err := passkeyConfig(); err == nil {
				t.Fatal("passkeyConfig() unexpectedly accepted insecure configuration")
			}
		})
	}
}

func TestLoadIdentitySigningSecretRequiresMountedFile(t *testing.T) {
	t.Setenv(identitySigningSecretFileEnv, "")
	if _, err := loadIdentitySigningSecret(); err == nil {
		t.Fatal("loadIdentitySigningSecret() accepted an absent mounted secret")
	}

	secretFile := t.TempDir() + "/identity-signing-secret"
	if err := os.WriteFile(secretFile, []byte("  "+strings.Repeat("s", identitySigningSecretMinBytes)+"\n"), 0o600); err != nil {
		t.Fatalf("write mounted identity secret: %v", err)
	}
	t.Setenv(identitySigningSecretFileEnv, secretFile)
	secret, err := loadIdentitySigningSecret()
	if err != nil {
		t.Fatalf("loadIdentitySigningSecret() error = %v", err)
	}
	if got, want := string(secret), strings.Repeat("s", identitySigningSecretMinBytes); got != want {
		t.Fatalf("loaded secret = %q, want %q", got, want)
	}

	if err := os.WriteFile(secretFile, []byte("short"), 0o600); err != nil {
		t.Fatalf("replace mounted identity secret: %v", err)
	}
	if _, err := loadIdentitySigningSecret(); err == nil {
		t.Fatal("loadIdentitySigningSecret() accepted a weak secret")
	}
}

func TestRequireSignedIdentityBindsIdentityFromVerifiedHeaders(t *testing.T) {
	secret := []byte(strings.Repeat("s", identitySigningSecretMinBytes))
	identityID := uuid.New()
	sessionID := uuid.New()
	req := signedIdentityRequest(t, secret, identityID, sessionID, "{}", "aal1")
	recorder := httptest.NewRecorder()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode trusted request: %v", err)
		}
		if got, want := body["identity_id"], identityID.String(); got != want {
			t.Fatalf("bound identity ID = %q, want %q", got, want)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	requireSignedIdentity(secret, next).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusNoContent, recorder.Body.String())
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func TestRequireSignedIdentityRejectsBodySuppliedIdentity(t *testing.T) {
	secret := []byte(strings.Repeat("s", identitySigningSecretMinBytes))
	req := signedIdentityRequest(t, secret, uuid.New(), uuid.New(), `{"identity_id":"attacker"}`, "aal1")
	recorder := httptest.NewRecorder()
	called := false

	requireSignedIdentity(secret, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})).ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if called {
		t.Fatal("handler received a request carrying a body-supplied identity")
	}
}

func TestRequireSignedIdentityRejectsTamperedContext(t *testing.T) {
	secret := []byte(strings.Repeat("s", identitySigningSecretMinBytes))
	req := signedIdentityRequest(t, secret, uuid.New(), uuid.New(), "{}", "aal1")
	req.Header.Set("X-User-AAL", "aal2")
	recorder := httptest.NewRecorder()

	requireSignedIdentity(secret, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler received a request with a tampered identity context")
	})).ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestModuleConfigDescribesOnlyInstalledProtectedRoutes(t *testing.T) {
	readiness := func(context.Context) error { return nil }
	cfg := moduleConfig("127.0.0.1:9004", nil, readiness)
	if cfg.Module != "passkeys" || cfg.Version != moduleVersion || cfg.ListenAddr != "127.0.0.1:9004" {
		t.Fatalf("unexpected module config header: %+v", cfg)
	}
	if got, want := strings.Join(cfg.Capabilities, ","), "passkey_step_up"; got != want {
		t.Fatalf("capabilities = %q, want %q", got, want)
	}
	if got, want := strings.Join(cfg.Routes, ","), strings.Join(passkeyRoutes, ","); got != want {
		t.Fatalf("routes = %q, want %q", got, want)
	}
	if len(cfg.GRPCServices) != 0 || len(cfg.EventSubscriptions) != 0 {
		t.Fatalf("metadata advertises unwired services or subscriptions: %+v", cfg)
	}
	if cfg.Readiness == nil {
		t.Fatal("module config is missing readiness")
	}
}

func TestBuildRuntimeRejectsAbsentDatabaseBeforeServing(t *testing.T) {
	t.Setenv(rpIDEnv, "example.test")
	t.Setenv(rpOriginEnv, "https://login.example.test")
	t.Setenv(databaseURLEnv, "")
	secretFile := t.TempDir() + "/identity-signing-secret"
	if err := os.WriteFile(secretFile, []byte(strings.Repeat("s", identitySigningSecretMinBytes)), 0o600); err != nil {
		t.Fatalf("write mounted identity secret: %v", err)
	}
	t.Setenv(identitySigningSecretFileEnv, secretFile)

	if _, err := buildRuntime(context.Background()); err == nil {
		t.Fatal("buildRuntime() accepted missing durable database configuration")
	}
}

func TestRunInvokesModuleServerWithProtectedRoutes(t *testing.T) {
	originalRunModuleServer := runModuleServer
	originalBuildRuntime := buildRuntimeHook
	t.Cleanup(func() {
		runModuleServer = originalRunModuleServer
		buildRuntimeHook = originalBuildRuntime
	})

	secret := []byte(strings.Repeat("s", identitySigningSecretMinBytes))
	buildRuntimeHook = func(context.Context) (*runtime, error) {
		return &runtime{
			passkeyService: service.New(store.New(), service.Config{
				RPID:               "example.test",
				RPOrigin:           "https://login.example.test",
				ChallengeTTL:       time.Minute,
				AllowedCredentials: 1,
			}),
			identitySigningSecret: secret,
		}, nil
	}

	var captured moduleserver.Config
	runModuleServer = func(cfg moduleserver.Config) error {
		captured = cfg
		return nil
	}

	if err := run([]string{"-listen", "127.0.0.1:19004"}); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if captured.Module != "passkeys" || captured.ListenAddr != "127.0.0.1:19004" {
		t.Fatalf("run() passed unexpected module config: %+v", captured)
	}
	if captured.RegisterHTTPRoutes == nil || captured.Readiness == nil {
		t.Fatal("run() did not wire protected routes and readiness")
	}
}

func signedIdentityRequest(t *testing.T, secret []byte, identityID, sessionID uuid.UUID, body, aal string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/passkeys/registration/start", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", identityID.String())
	req.Header.Set("X-User-Session-ID", sessionID.String())
	req.Header.Set("X-User-AAL", aal)
	signature, err := platformcrypto.SignIdentityHeaders(secret, req.Header, signedIdentityHeaders, time.Now().UTC())
	if err != nil {
		t.Fatalf("sign identity context: %v", err)
	}
	req.Header.Set("X-Aegion-Signature", signature)
	return req
}
