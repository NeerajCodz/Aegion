package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aegion/aegion/core/registry"
	"github.com/aegion/aegion/core/workers"
	"github.com/aegion/aegion/internal/platform/config"
	"github.com/aegion/aegion/internal/platform/database"
)

func newBranchServerConfig() *config.Config {
	return &config.Config{
		Server: config.ServerConfig{
			RequestTimeout: config.Duration(10 * time.Second),
			Registry: config.ServiceRegistryConfig{
				HealthCheckInterval: config.Duration(time.Second),
				HealthCheckTimeout:  config.Duration(time.Second),
			},
			CORS: config.CORSConfig{
				AllowedOrigins:   []string{"https://example.com"},
				AllowedMethods:   []string{"GET", "POST"},
				AllowedHeaders:   []string{"Content-Type", "Authorization"},
				AllowCredentials: true,
			},
		},
		Admin: config.AdminConfig{
			Enabled: false,
			Path:    "/aegion",
		},
	}
}

func TestNewServerAdditionalSecretAndBootstrapBranches(t *testing.T) {
	t.Run("cookie secret fallback and optional auth services", func(t *testing.T) {
		cfg := newBranchServerConfig()
		cfg.Secrets.Cookie = []string{"cookie-secret"}
		cfg.Password.Enabled = true
		cfg.MagicLink.Enabled = true
		cfg.MFA.Enabled = true
		cfg.Passkeys.Enabled = true
		cfg.Passkeys.RPID = "localhost"
		cfg.Passkeys.RPOrigin = "http://localhost"

		s, err := NewServer(context.Background(), &ServerConfig{
			Config:        cfg,
			DB:            &database.DB{Pool: nil},
			Log:           testLogger(),
			WorkerManager: workers.NewManager(workers.ManagerConfig{Log: testLogger()}),
		})
		if err != nil {
			t.Fatalf("NewServer(cookie fallback) error: %v", err)
		}
		if s.passwordAuth == nil || s.magicLinkAuth == nil || s.mfaAuth == nil || s.passkeyAuth == nil {
			t.Fatalf("expected optional auth services to be initialized")
		}
		if err := s.Shutdown(context.Background()); err != nil {
			t.Fatalf("shutdown failed: %v", err)
		}
	})

	t.Run("cipher secret fallback", func(t *testing.T) {
		cfg := newBranchServerConfig()
		cfg.Secrets.Cipher = []string{"cipher-secret"}

		s, err := NewServer(context.Background(), &ServerConfig{
			Config:        cfg,
			DB:            &database.DB{Pool: nil},
			Log:           testLogger(),
			WorkerManager: workers.NewManager(workers.ManagerConfig{Log: testLogger()}),
		})
		if err != nil {
			t.Fatalf("NewServer(cipher fallback) error: %v", err)
		}
		if s.TokenGenerator() == nil {
			t.Fatal("expected token generator to be initialized")
		}
		if err := s.Shutdown(context.Background()); err != nil {
			t.Fatalf("shutdown failed: %v", err)
		}
	})

	t.Run("missing secret fails with explicit error", func(t *testing.T) {
		cfg := newBranchServerConfig()
		_, err := NewServer(context.Background(), &ServerConfig{
			Config: cfg,
			DB:     &database.DB{Pool: nil},
			Log:    testLogger(),
		})
		if err == nil || !strings.Contains(err.Error(), "internal auth secret is not configured") {
			t.Fatalf("expected missing secret error, got %v", err)
		}
	})

	t.Run("empty internal secret fails token generator init", func(t *testing.T) {
		cfg := newBranchServerConfig()
		cfg.Secrets.Internal = []string{""}

		_, err := NewServer(context.Background(), &ServerConfig{
			Config: cfg,
			DB:     &database.DB{Pool: nil},
			Log:    testLogger(),
		})
		if err == nil {
			t.Fatal("expected token generator initialization error")
		}
	})

	t.Run("admin bootstrap warning path does not fail startup", func(t *testing.T) {
		orig := ensureBootstrapAdminOperator
		t.Cleanup(func() {
			ensureBootstrapAdminOperator = orig
		})
		ensureBootstrapAdminOperator = func(ctx context.Context, db *database.DB, email, password string) (bootstrapAdminOutcome, error) {
			return bootstrapAdminOutcome{}, errors.New("forced bootstrap failure")
		}

		cfg := newBranchServerConfig()
		cfg.Secrets.Internal = []string{"dev-internal-secret-change-me-32chars"}
		cfg.Operator.Email = "admin@example.com"
		cfg.Operator.Password = "Password1!"

		s, err := NewServer(context.Background(), &ServerConfig{
			Config:         cfg,
			DB:             &database.DB{Pool: nil},
			Log:            testLogger(),
			AdminBootstrap: true,
			WorkerManager:  workers.NewManager(workers.ManagerConfig{Log: testLogger()}),
		})
		if err != nil {
			t.Fatalf("NewServer(admin bootstrap warning) error: %v", err)
		}
		if err := s.Shutdown(context.Background()); err != nil {
			t.Fatalf("shutdown failed: %v", err)
		}
	})
}

func TestHandleReadyMixedModuleStatusesAndCORSWildcard(t *testing.T) {
	t.Run("ready payload excludes healthy modules from unhealthy list", func(t *testing.T) {
		s := newTestServer(t)
		s.db = &database.DB{Pool: nil}

		origPing := pingDatabase
		pingDatabase = func(ctx context.Context, db *database.DB) error { return nil }
		t.Cleanup(func() {
			pingDatabase = origPing
		})

		registerTestModule(t, s, "healthy-mod", registry.EndpointHTTP, "http://127.0.0.1:19081")
		registerTestModule(t, s, "unhealthy-mod", registry.EndpointHTTP, "http://127.0.0.1:19082")
		if err := s.registry.UpdateStatus("healthy-mod", registry.StatusHealthy); err != nil {
			t.Fatalf("set healthy status: %v", err)
		}
		if err := s.registry.UpdateStatus("unhealthy-mod", registry.StatusUnhealthy); err != nil {
			t.Fatalf("set unhealthy status: %v", err)
		}

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
		s.handleReady(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503, got %d", rec.Code)
		}

		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		unhealthy, ok := body["unhealthy_modules"].([]any)
		if !ok || len(unhealthy) != 1 {
			t.Fatalf("expected one unhealthy module, got %#v", body["unhealthy_modules"])
		}
	})

	t.Run("cors allow-all branch without credentials", func(t *testing.T) {
		s := newTestServer(t)
		s.cfg.Server.CORS.AllowedOrigins = []string{" ", "*"}
		s.cfg.Server.CORS.AllowCredentials = false

		nextCalled := false
		handler := s.corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nextCalled = true
			w.WriteHeader(http.StatusNoContent)
		}))

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/whoami", nil)
		req.Header.Set("Origin", "https://random.example")
		handler.ServeHTTP(rec, req)

		if !nextCalled {
			t.Fatal("expected next handler to be called")
		}
		if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
			t.Fatalf("expected wildcard allow origin, got %q", rec.Header().Get("Access-Control-Allow-Origin"))
		}
		if rec.Header().Get("Access-Control-Allow-Credentials") != "" {
			t.Fatalf("expected no allow-credentials header, got %q", rec.Header().Get("Access-Control-Allow-Credentials"))
		}
	})
}
