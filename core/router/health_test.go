package router

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aegion/aegion/core/registry"
)

func TestHealthEndpoint(t *testing.T) {
	r := New(DefaultConfig(), slog.Default(), nil)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["status"] != "healthy" {
		t.Fatalf("expected healthy status, got %v", body["status"])
	}
}

func TestReadyEndpointWithoutRegistry(t *testing.T) {
	r := New(DefaultConfig(), slog.Default(), nil)
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body ReadinessStatus
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := body.Checks["database"].Status; got != "disabled" {
		t.Fatalf("expected database check disabled, got %s", got)
	}
	if got := body.Checks["cache"].Status; got != "disabled" {
		t.Fatalf("expected cache check disabled, got %s", got)
	}
}

func TestReadyEndpointWithUnhealthyModules(t *testing.T) {
	reg := registry.New(registry.DefaultConfig(), slog.Default())
	_, _ = reg.Register(registry.RegistrationRequest{
		ID:      "mod1",
		Name:    "module",
		Version: "v1",
		Endpoints: []registry.Endpoint{
			{Type: registry.EndpointHTTP, URL: "http://127.0.0.1:8080"},
		},
	})

	r := New(DefaultConfig(), slog.Default(), reg)
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestMetricsEndpoint(t *testing.T) {
	reg := registry.New(registry.DefaultConfig(), slog.Default())
	_, _ = reg.Register(registry.RegistrationRequest{
		ID:      "mod1",
		Name:    "module",
		Version: "v1",
		Endpoints: []registry.Endpoint{
			{Type: registry.EndpointHTTP, URL: "http://127.0.0.1:8080"},
		},
	})
	_ = reg.UpdateStatus("mod1", registry.StatusHealthy)

	r := New(DefaultConfig(), slog.Default(), reg)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if body == "" {
		t.Fatal("expected metrics body")
	}
	if !strings.Contains(body, "aegion_router_uptime_seconds") {
		t.Fatalf("expected uptime metric in body, got %q", body)
	}
	if !strings.Contains(body, "aegion_modules_total 1") {
		t.Fatalf("expected module count metric in body, got %q", body)
	}
	if !strings.Contains(body, "aegion_dependency_status{component=\"database\",status=\"disabled\"} 1") {
		t.Fatalf("expected dependency status metric in body, got %q", body)
	}
}

func TestDatabaseHealthCheckerAndCacheHealthChecker(t *testing.T) {
	dbChecker := NewDatabaseHealthChecker(nil)
	if err := dbChecker.Check(); err == nil {
		t.Fatal("expected error for unconfigured database checker")
	}

	cacheChecker := NewCacheHealthChecker(func() error { return nil })
	if err := cacheChecker.Check(); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestReadyEndpointWithDependencyFailure(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DatabaseChecker = NewDatabaseHealthChecker(func() error { return errors.New("database unavailable") })
	cfg.CacheChecker = NewCacheHealthChecker(func() error { return nil })

	r := New(cfg, slog.Default(), nil)
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}

	var body ReadinessStatus
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := body.Checks["database"].Status; got != "unhealthy" {
		t.Fatalf("expected unhealthy database status, got %s", got)
	}
	if got := body.Checks["cache"].Status; got != "healthy" {
		t.Fatalf("expected healthy cache status, got %s", got)
	}
}

func TestItoa(t *testing.T) {
	if got := itoa(0); got != "0" {
		t.Fatalf("expected 0, got %s", got)
	}
	if got := itoa(123); got != "123" {
		t.Fatalf("expected 123, got %s", got)
	}
	if got := itoa(-7); got != "-7" {
		t.Fatalf("expected -7, got %s", got)
	}
}

func TestMetricsEndpointWithDependencyLatency(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DatabaseChecker = NewDatabaseHealthChecker(func() error {
		time.Sleep(2 * time.Millisecond)
		return nil
	})
	cfg.CacheChecker = NewCacheHealthChecker(func() error { return errors.New("cache down") })

	r := New(cfg, slog.Default(), nil)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "aegion_dependency_latency_milliseconds{component=\"database\"}") {
		t.Fatalf("expected dependency latency metric in body, got %q", body)
	}
	if !strings.Contains(body, "aegion_dependency_status{component=\"cache\",status=\"unhealthy\"} 1") {
		t.Fatalf("expected unhealthy cache metric in body, got %q", body)
	}
}
