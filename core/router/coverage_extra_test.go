package router

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aegion/aegion/core/registry"
	"github.com/aegion/aegion/internal/xlog"
)

func TestReadyEndpoint_DegradedAndCancelledContext(t *testing.T) {
	reg := registry.New(registry.DefaultConfig(), xlog.New(xlog.Config{}))
	defer reg.Stop()

	_, _ = reg.Register(registry.RegistrationRequest{
		ID:      "mod-healthy",
		Name:    "healthy",
		Version: "v1",
		Endpoints: []registry.Endpoint{
			{Type: registry.EndpointHTTP, URL: "http://127.0.0.1:18081"},
		},
	})
	_, _ = reg.Register(registry.RegistrationRequest{
		ID:      "mod-starting",
		Name:    "starting",
		Version: "v1",
		Endpoints: []registry.Endpoint{
			{Type: registry.EndpointHTTP, URL: "http://127.0.0.1:18082"},
		},
	})
	_ = reg.UpdateStatus("mod-healthy", registry.StatusHealthy)

	nopLog := xlog.New(xlog.Config{})
	r := New(DefaultConfig(), nopLog, reg)

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected degraded readiness to return 200, got %d", rec.Code)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode readiness response: %v", err)
	}
	checks, _ := body["checks"].(map[string]interface{})
	modules, _ := checks["modules"].(map[string]interface{})
	if modules["status"] != "degraded" {
		t.Fatalf("expected module status degraded, got %v", modules["status"])
	}

	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	cancelledReq := httptest.NewRequest(http.MethodGet, "/ready", nil).WithContext(cancelledCtx)
	cancelledRec := httptest.NewRecorder()
	r.handleReady(cancelledRec, cancelledReq)
	if cancelledRec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected cancelled context to return 503, got %d", cancelledRec.Code)
	}
}

func TestHealthChecker_AdditionalBranches(t *testing.T) {
	dbChecker := NewDatabaseHealthChecker(func() error { return errors.New("db down") })
	if err := dbChecker.Check(); err == nil || err.Error() != "db down" {
		t.Fatalf("expected db checker error, got %v", err)
	}

	cacheChecker := NewCacheHealthChecker(nil)
	if err := cacheChecker.Check(); err == nil {
		t.Fatal("expected error for nil cache check function")
	}
}

func TestModuleProxy_TimeoutAndForwardedProtoBranches(t *testing.T) {
	nopLog := xlog.New(xlog.Config{})
	proxy := NewModuleProxy(ModuleProxyConfig{
		ModuleID: "password",
		Logger:   nopLog,
	})

	original := httptest.NewRequest(http.MethodGet, "https://gateway.local/module", nil)
	original.Host = "gateway.local"
	original.RemoteAddr = "203.0.113.10:1234"
	original.TLS = &tls.ConnectionState{}
	req := httptest.NewRequest(http.MethodGet, "http://placeholder/module", nil)
	proxy.addForwardedHeaders(req, original)
	if got := req.Header.Get("X-Forwarded-Proto"); got != "https" {
		t.Fatalf("expected forwarded proto https, got %q", got)
	}

	expiredCtx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	timeoutReq := httptest.NewRequest(http.MethodGet, "/module", nil).WithContext(expiredCtx)
	timeoutRec := httptest.NewRecorder()
	proxy.handleProxyError(timeoutRec, timeoutReq, errors.New("upstream timeout"), "req-timeout")
	if timeoutRec.Code != http.StatusGatewayTimeout {
		t.Fatalf("expected gateway timeout on deadline exceeded, got %d", timeoutRec.Code)
	}

	proxy.logResponse(&http.Response{StatusCode: http.StatusNotFound}, "req-warn", time.Now().Add(-time.Millisecond))
	proxy.logResponse(&http.Response{StatusCode: http.StatusInternalServerError}, "req-error", time.Now().Add(-time.Millisecond))
}

func TestModuleProxyServeHTTP_TransportErrorPath(t *testing.T) {
	nopLog := xlog.New(xlog.Config{})
	reg := registry.New(registry.DefaultConfig(), nopLog)
	defer reg.Stop()

	_, _ = reg.Register(registry.RegistrationRequest{
		ID:      "password",
		Name:    "password",
		Version: "v1",
		Endpoints: []registry.Endpoint{
			{Type: registry.EndpointHTTP, URL: "http://127.0.0.1:1"},
		},
	})
	_ = reg.UpdateStatus("password", registry.StatusHealthy)

	proxy := NewModuleProxy(ModuleProxyConfig{
		Registry: reg,
		ModuleID: "password",
		Timeout:  200 * time.Millisecond,
		Logger:   nopLog,
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/module/test", nil)
	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable && rec.Code != http.StatusBadGateway {
		t.Fatalf("expected proxy transport error status, got %d", rec.Code)
	}
}
