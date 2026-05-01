package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aegion/aegion/core/orchestrator"
	"github.com/aegion/aegion/core/registry"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aegion/aegion/core/flows"
	"github.com/aegion/aegion/core/workers"
	"github.com/aegion/aegion/internal/platform/config"
	"github.com/aegion/aegion/internal/platform/database"
)

type blockingWorker struct {
	stopDelay time.Duration
}

func (w *blockingWorker) Name() string { return "blocking-worker" }
func (w *blockingWorker) Start(ctx context.Context) error {
	<-ctx.Done()
	return nil
}
func (w *blockingWorker) Stop() { time.Sleep(w.stopDelay) }

type stubModuleOrchestrator struct {
	startErr error
	stopErr  error

	startCalls int
	stopCalls  int
}

func (s *stubModuleOrchestrator) Start(ctx context.Context) error {
	s.startCalls++
	return s.startErr
}

func (s *stubModuleOrchestrator) Stop(ctx context.Context) error {
	s.stopCalls++
	return s.stopErr
}

func (s *stubModuleOrchestrator) RestartModule(ctx context.Context, moduleID string) error {
	return nil
}

type stubTelemetryProvider struct {
	shutdownCalls int
	shutdownErr   error
}

func (s *stubTelemetryProvider) Shutdown(ctx context.Context) error {
	s.shutdownCalls++
	return s.shutdownErr
}

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

func TestHandleReadyDatabaseHealthy(t *testing.T) {
	s := newTestServer(t)
	s.db = &database.DB{Pool: nil}

	originalPing := pingDatabase
	pingDatabase = func(ctx context.Context, db *database.DB) error { return nil }
	defer func() { pingDatabase = originalPing }()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	s.handleReady(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"status":"ready"`) {
		t.Fatalf("expected readiness success body, got %q", rec.Body.String())
	}
}

func TestHandleReadyRegistryUnavailable(t *testing.T) {
	s := newTestServer(t)
	s.db = &database.DB{Pool: nil}
	s.registry = nil

	originalPing := pingDatabase
	pingDatabase = func(ctx context.Context, db *database.DB) error { return nil }
	defer func() { pingDatabase = originalPing }()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	s.handleReady(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"reason":"registry unavailable"`) {
		t.Fatalf("expected registry unavailable body, got %q", rec.Body.String())
	}
}

func TestHandleReadyModulesUnhealthy(t *testing.T) {
	s := newTestServer(t)
	s.db = &database.DB{Pool: nil}

	originalPing := pingDatabase
	pingDatabase = func(ctx context.Context, db *database.DB) error { return nil }
	defer func() { pingDatabase = originalPing }()

	registerTestModule(t, s, "proxy", registry.EndpointHTTP, "http://127.0.0.1:9999")
	if err := s.registry.UpdateStatus("proxy", registry.StatusUnhealthy); err != nil {
		t.Fatalf("failed to update module status: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	s.handleReady(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode readiness response: %v", err)
	}
	if body["reason"] != "modules unhealthy" {
		t.Fatalf("unexpected reason: %v", body["reason"])
	}
	unhealthy, ok := body["unhealthy_modules"].([]interface{})
	if !ok || len(unhealthy) != 1 {
		t.Fatalf("expected one unhealthy module, got %#v", body["unhealthy_modules"])
	}
}

func TestServerBootstrapGettersAndShutdown(t *testing.T) {
	s := newTestServer(t)
	s.flowService = flows.NewService(newRouteFlowStore(), flows.DefaultConfig())
	s.router = SetupRoutes(s)

	origBootstrap := ensureBootstrapAdminOperator
	t.Cleanup(func() {
		ensureBootstrapAdminOperator = origBootstrap
	})

	var (
		bootstrapCalls   int
		capturedEmail    string
		capturedPassword string
	)
	ensureBootstrapAdminOperator = func(ctx context.Context, db *database.DB, email, password string) (bootstrapAdminOutcome, error) {
		bootstrapCalls++
		capturedEmail = email
		capturedPassword = password
		return bootstrapAdminOutcome{
			IdentityID:      uuid.New(),
			OperatorID:      uuid.New(),
			CreatedIdentity: true,
			CreatedOperator: true,
		}, nil
	}

	if err := s.bootstrapAdmin(context.Background()); err != nil {
		t.Fatalf("bootstrapAdmin without credentials should not fail: %v", err)
	}
	if bootstrapCalls != 0 {
		t.Fatalf("bootstrap should not run without configured credentials")
	}

	s.cfg.Operator.Email = "Admin@Example.com "
	s.cfg.Operator.Password = " test-password "
	if err := s.bootstrapAdmin(context.Background()); err != nil {
		t.Fatalf("bootstrapAdmin with credentials should not fail: %v", err)
	}
	if bootstrapCalls != 1 {
		t.Fatalf("expected bootstrap handler call, got %d", bootstrapCalls)
	}
	if capturedEmail != "admin@example.com" {
		t.Fatalf("expected normalized email admin@example.com, got %q", capturedEmail)
	}
	if capturedPassword != "test-password" {
		t.Fatalf("expected trimmed password, got %q", capturedPassword)
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

func TestBootstrapAdmin_PropagatesProvisioningError(t *testing.T) {
	s := newTestServer(t)
	s.cfg.Operator.Email = "admin@example.com"
	s.cfg.Operator.Password = "password"

	origBootstrap := ensureBootstrapAdminOperator
	t.Cleanup(func() {
		ensureBootstrapAdminOperator = origBootstrap
	})

	ensureBootstrapAdminOperator = func(ctx context.Context, db *database.DB, email, password string) (bootstrapAdminOutcome, error) {
		return bootstrapAdminOutcome{}, errors.New("bootstrap failed")
	}

	err := s.bootstrapAdmin(context.Background())
	if err == nil || !strings.Contains(err.Error(), "bootstrap failed") {
		t.Fatalf("expected bootstrap error to be returned, got %v", err)
	}
}

func TestBootstrapAdmin_RejectsPlaceholderPassword(t *testing.T) {
	s := newTestServer(t)
	s.cfg.Operator.Email = "admin@example.com"
	s.cfg.Operator.Password = "admin123!"

	origBootstrap := ensureBootstrapAdminOperator
	t.Cleanup(func() {
		ensureBootstrapAdminOperator = origBootstrap
	})

	bootstrapCalls := 0
	ensureBootstrapAdminOperator = func(ctx context.Context, db *database.DB, email, password string) (bootstrapAdminOutcome, error) {
		bootstrapCalls++
		return bootstrapAdminOutcome{}, nil
	}

	err := s.bootstrapAdmin(context.Background())
	if err == nil || !strings.Contains(err.Error(), "placeholder value") {
		t.Fatalf("expected placeholder credential error, got %v", err)
	}
	if bootstrapCalls != 0 {
		t.Fatalf("expected bootstrap to be blocked for placeholder password")
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
		Secrets: config.SecretsConfig{
			Internal: []string{"dev-internal-secret-change-me-32chars"},
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
	if server.policyChecker != nil {
		t.Fatalf("expected policy checker to be nil when policy is disabled")
	}

	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown should not fail: %v", err)
	}
}

func TestNewServerInitializesPolicyCheckerWhenEnabled(t *testing.T) {
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
		Policy: config.PolicyConfig{
			Enabled:      true,
			DefaultModel: "rbac",
			RBAC:         config.PolicyRBACConfig{Enabled: true},
		},
		Secrets: config.SecretsConfig{
			Internal: []string{"dev-internal-secret-change-me-32chars"},
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

	if server.policyChecker == nil {
		t.Fatalf("expected policy checker to be initialized when policy is enabled")
	}

	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown should not fail: %v", err)
	}
}

func TestNewServerStartsModuleOrchestratorWhenConfigPathProvided(t *testing.T) {
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
		Secrets: config.SecretsConfig{
			Internal: []string{"dev-internal-secret-change-me-32chars"},
		},
	}

	stub := &stubModuleOrchestrator{}
	var capturedCfg orchestrator.Config
	orig := newModuleOrchestrator
	t.Cleanup(func() {
		newModuleOrchestrator = orig
	})
	newModuleOrchestrator = func(cfg orchestrator.Config) (moduleOrchestrator, error) {
		capturedCfg = cfg
		return stub, nil
	}

	server, err := NewServer(context.Background(), &ServerConfig{
		Config:         cfg,
		ConfigPath:     "configs/aegion.yaml",
		DB:             &database.DB{Pool: nil},
		Log:            log,
		WorkerManager:  workers.NewManager(workers.ManagerConfig{Log: log}),
		AdminBootstrap: false,
	})
	if err != nil {
		t.Fatalf("NewServer returned error: %v", err)
	}
	if server.Orchestrator() == nil {
		t.Fatalf("expected orchestrator to be initialized")
	}
	if stub.startCalls != 1 {
		t.Fatalf("expected orchestrator start to be called once, got %d", stub.startCalls)
	}
	if capturedCfg.ConfigPath != "configs/aegion.yaml" {
		t.Fatalf("expected config path to be forwarded")
	}
	if capturedCfg.Registry == nil {
		t.Fatalf("expected registry to be passed to orchestrator config")
	}
	if len(capturedCfg.TokenSecret) == 0 {
		t.Fatalf("expected token secret in orchestrator config")
	}
}

func TestNewServerReturnsErrorWhenOrchestratorStartFails(t *testing.T) {
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
		Secrets: config.SecretsConfig{
			Internal: []string{"dev-internal-secret-change-me-32chars"},
		},
	}

	orig := newModuleOrchestrator
	t.Cleanup(func() {
		newModuleOrchestrator = orig
	})
	newModuleOrchestrator = func(cfg orchestrator.Config) (moduleOrchestrator, error) {
		return &stubModuleOrchestrator{startErr: errors.New("orchestrator start failed")}, nil
	}

	_, err := NewServer(context.Background(), &ServerConfig{
		Config:         cfg,
		ConfigPath:     "configs/aegion.yaml",
		DB:             &database.DB{Pool: nil},
		Log:            log,
		WorkerManager:  workers.NewManager(workers.ManagerConfig{Log: log}),
		AdminBootstrap: false,
	})
	if err == nil {
		t.Fatalf("expected NewServer to fail when orchestrator start fails")
	}
	if !strings.Contains(err.Error(), "orchestrator start failed") {
		t.Fatalf("expected orchestrator start error, got %v", err)
	}
}

func TestServerShutdownStopsModuleOrchestrator(t *testing.T) {
	s := newTestServer(t)
	stub := &stubModuleOrchestrator{}
	s.orchestrator = stub

	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown should not fail: %v", err)
	}
	if stub.stopCalls != 1 {
		t.Fatalf("expected orchestrator stop to be called once, got %d", stub.stopCalls)
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

func TestLifecycleShutdown_ShutsDownObservabilityProvider(t *testing.T) {
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer httpSrv.Close()

	provider := &stubTelemetryProvider{}
	lc := NewLifecycle(&LifecycleConfig{
		Log:           testLogger(),
		Server:        newTestServer(t),
		HTTPServer:    httpSrv.Config,
		Observability: provider,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := lc.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("shutdown should succeed with observability provider, got %v", err)
	}
	if provider.shutdownCalls != 1 {
		t.Fatalf("expected observability provider shutdown once, got %d", provider.shutdownCalls)
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
	defer func() {
		_ = ln.Close()
	}()

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

func TestLifecycleShutdown_CanceledContextAndWorkerBranch(t *testing.T) {
	s := newTestServer(t)
	wm := workers.NewManager(workers.ManagerConfig{Log: testLogger()})
	wm.Register(&blockingWorker{stopDelay: 50 * time.Millisecond})

	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer httpSrv.Close()

	lc := NewLifecycle(&LifecycleConfig{
		Log:           testLogger(),
		Server:        s,
		HTTPServer:    httpSrv.Config,
		WorkerManager: wm,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := lc.Shutdown(ctx)
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("unexpected shutdown error for canceled context: %v", err)
	}
	if !lc.IsDraining() {
		t.Fatalf("expected lifecycle to be marked as draining")
	}
}

func TestLifecycleDrainHTTP_DeadlineExceededForcesClose(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	serveDone := make(chan struct{})

	httpSrv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case started <- struct{}{}:
			default:
			}
			<-release
			w.WriteHeader(http.StatusNoContent)
		}),
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer func() {
		_ = ln.Close()
	}()

	go func() {
		_ = httpSrv.Serve(ln)
		close(serveDone)
	}()

	go func() {
		_, _ = http.Get("http://" + ln.Addr().String())
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for inflight request")
	}

	lc := NewLifecycle(&LifecycleConfig{
		Log:        testLogger(),
		Server:     newTestServer(t),
		HTTPServer: httpSrv,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	if err := lc.drainHTTP(ctx); err != nil {
		t.Fatalf("expected drainHTTP to force close on deadline exceeded, got %v", err)
	}

	close(release)
	select {
	case <-serveDone:
	case <-time.After(2 * time.Second):
		t.Fatalf("expected server to stop after force close")
	}
}

func TestLifecycleDrainHTTP_ReturnsContextErrorWhenCanceled(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	serveDone := make(chan struct{})

	httpSrv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case started <- struct{}{}:
			default:
			}
			<-release
			w.WriteHeader(http.StatusNoContent)
		}),
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer func() {
		_ = ln.Close()
	}()

	go func() {
		_ = httpSrv.Serve(ln)
		close(serveDone)
	}()

	go func() {
		_, _ = http.Get("http://" + ln.Addr().String())
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for inflight request")
	}

	lc := NewLifecycle(&LifecycleConfig{
		Log:        testLogger(),
		Server:     newTestServer(t),
		HTTPServer: httpSrv,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = lc.drainHTTP(ctx)
	if !errors.Is(err, context.Canceled) && !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("expected context cancellation style error, got %v", err)
	}

	close(release)
	select {
	case <-serveDone:
	case <-time.After(2 * time.Second):
		t.Fatalf("expected server to stop after cancellation")
	}
}

func TestLifecycleCleanupRegistry_ClosedRegistryWarnPath(t *testing.T) {
	s := newTestServer(t)
	registerTestModule(t, s, "closed-registry-module", registry.EndpointHTTP, "http://localhost:9099")

	// Force Deregister to return ErrRegistryClosed inside cleanup loop.
	s.registry.Stop()

	lc := NewLifecycle(&LifecycleConfig{
		Log:        testLogger(),
		Server:     s,
		HTTPServer: &http.Server{},
	})

	if err := lc.cleanupRegistry(context.Background()); err != nil {
		t.Fatalf("cleanupRegistry should tolerate closed registry warnings: %v", err)
	}
}
