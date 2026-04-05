package router

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/aegion/aegion/core/registry"
)

func TestHealthEndpoint(t *testing.T) {
	r := New(DefaultConfig(), zerolog.Nop(), nil)
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
	r := New(DefaultConfig(), zerolog.Nop(), nil)
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestReadyEndpointWithUnhealthyModules(t *testing.T) {
	reg := registry.New(registry.DefaultConfig())
	_, _ = reg.Register(registry.RegistrationRequest{
		ID:      "mod1",
		Name:    "module",
		Version: "v1",
		Endpoints: []registry.Endpoint{
			{Type: registry.EndpointHTTP, URL: "http://127.0.0.1:8080"},
		},
	})

	r := New(DefaultConfig(), zerolog.Nop(), reg)
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestMetricsEndpoint(t *testing.T) {
	reg := registry.New(registry.DefaultConfig())
	_, _ = reg.Register(registry.RegistrationRequest{
		ID:      "mod1",
		Name:    "module",
		Version: "v1",
		Endpoints: []registry.Endpoint{
			{Type: registry.EndpointHTTP, URL: "http://127.0.0.1:8080"},
		},
	})
	_ = reg.UpdateStatus("mod1", registry.StatusHealthy)

	r := New(DefaultConfig(), zerolog.Nop(), reg)
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
}

func TestDatabaseHealthCheckerAndCacheHealthChecker(t *testing.T) {
	dbChecker := NewDatabaseHealthChecker(nil)
	if err := dbChecker.Check(); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	cacheChecker := NewCacheHealthChecker(func() error { return nil })
	if err := cacheChecker.Check(); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	_ = time.Now()
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
