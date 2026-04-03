package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aegion/aegion/core/registry"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aegion/aegion/core/flows"
	"github.com/aegion/aegion/core/workers"
	"github.com/aegion/aegion/internal/platform/config"
	"github.com/aegion/aegion/internal/platform/database"
)

func TestServerHealthAndLivenessHandlers(t *testing.T) {
	s := newTestServer(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	s.handleHealth(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Fatalf("expected health body to include status ok, got %q", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/health/live", nil)
	s.handleLive(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"status":"alive"`) {
		t.Fatalf("expected live body to include status alive, got %q", rec.Body.String())
	}
}

func TestHandleReadyDatabaseUnavailable(t *testing.T) {
	pool, err := pgxpool.New(
		context.Background(),
		"postgres://aegion:aegion@127.0.0.1:1/aegion?sslmode=disable&connect_timeout=1",
	)
	if err != nil {
		t.Fatalf("failed to create test pool: %v", err)
	}
	defer pool.Close()

	s := newTestServer(t)
	s.db = &database.DB{Pool: pool}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	s.handleReady(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "database unavailable") {
		t.Fatalf("expected readiness failure body, got %q", rec.Body.String())
	}
}

func TestServerBootstrapGettersAndShutdown(t *testing.T) {
	s := newTestServer(t)
	s.flowService = flows.NewService(newRouteFlowStore(), flows.DefaultConfig())
	s.router = SetupRoutes(s)

	if err := s.bootstrapAdmin(context.Background()); err != nil {
		t.Fatalf("bootstrapAdmin without credentials should not fail: %v", err)
	}

	s.cfg.Operator.Email = "admin@example.com"
	s.cfg.Operator.Password = "test-password"
	if err := s.bootstrapAdmin(context.Background()); err != nil {
		t.Fatalf("bootstrapAdmin with credentials should not fail: %v", err)
	}

	if s.Handler() == nil {
		t.Fatalf("expected non-nil handler")
	}
	if s.Registry() == nil {
		t.Fatalf("expected non-nil registry")
	}
	if s.FlowService() == nil {
		t.Fatalf("expected non-nil flow service")
	}
	if s.TokenGenerator() == nil {
		t.Fatalf("expected non-nil token generator")
	}

	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown should not fail: %v", err)
	}
}

func TestNewServerInitializesCoreComponents(t *testing.T) {
	log := testLogger()

	cfg := &config.Config{
		Server: config.ServerConfig{
			RequestTimeout: config.Duration(10 * time.Second),
			InternalNet: config.InternalNetConfig{
				HealthCheckInt:     config.Duration(time.Second),
				HealthCheckTimeout: config.Duration(time.Second),
			},
		},
		Admin: config.AdminConfig{
			Enabled: false,
			Path:    "/aegion",
		},
	}

	server, err := NewServer(context.Background(), &ServerConfig{
		Config:         cfg,
		DB:             &database.DB{Pool: nil},
		Log:            log,
		WorkerManager:  workers.NewManager(workers.ManagerConfig{Log: log}),
		AdminBootstrap: true,
	})
	if err != nil {
		t.Fatalf("NewServer returned error: %v", err)
	}

	if server.Handler() == nil {
		t.Fatalf("expected handler to be initialized")
	}
	if server.Registry() == nil {
		t.Fatalf("expected registry to be initialized")
	}
	if server.FlowService() == nil {
		t.Fatalf("expected flow service to be initialized")
	}
	if server.TokenGenerator() == nil {
		t.Fatalf("expected token generator to be initialized")
	}

	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown should not fail: %v", err)
	}
}

func TestLifecycleDrainHTTP_UnstartedServer(t *testing.T) {
	lc := NewLifecycle(&LifecycleConfig{
		Log:        testLogger(),
		Server:     newTestServer(t),
		HTTPServer: &http.Server{},
	})

	err := lc.drainHTTP(context.Background())
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("expected nil or ErrServerClosed, got %v", err)
	}
}

func TestLifecycleShutdown_CleansRegistryAndIsIdempotent(t *testing.T) {
	s := newTestServer(t)
	registerTestModule(t, s, "lifecycle-module", registry.EndpointHTTP, "http://localhost:9999")

	httpSrv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer ln.Close()

	serveDone := make(chan struct{})
	go func() {
		_ = httpSrv.Serve(ln)
		close(serveDone)
	}()

	lc := NewLifecycle(&LifecycleConfig{
		Log:        testLogger(),
		Server:     s,
		HTTPServer: httpSrv,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := lc.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown should succeed, got %v", err)
	}
	if !lc.IsDraining() {
		t.Fatalf("expected lifecycle to be in draining state after shutdown")
	}
	if s.registry.ModuleCount() != 0 {
		t.Fatalf("expected registry to be cleaned up")
	}

	if err := lc.Shutdown(ctx); err != nil {
		t.Fatalf("second shutdown should be idempotent, got %v", err)
	}

	select {
	case <-serveDone:
	case <-time.After(2 * time.Second):
		t.Fatalf("expected HTTP server to stop")
	}
}

func TestLifecycleCleanupRegistry_NoRegistry(t *testing.T) {
	lc := NewLifecycle(&LifecycleConfig{
		Log:        testLogger(),
		Server:     &Server{},
		HTTPServer: &http.Server{},
	})

	if err := lc.cleanupRegistry(context.Background()); err != nil {
		t.Fatalf("cleanupRegistry should not fail when registry is nil: %v", err)
	}
}
