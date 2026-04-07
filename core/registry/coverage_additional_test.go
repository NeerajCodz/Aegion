package registry

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestDiscoveryAndHealthChecker_AdditionalCoverageBranches(t *testing.T) {
	t.Run("GetHealthyEndpoint returns single endpoint directly", func(t *testing.T) {
		reg := New(DefaultConfig())
		_, err := reg.Register(RegistrationRequest{
			ID:   "single-instance",
			Name: "single-service",
			Endpoints: []Endpoint{
				{Type: EndpointHTTP, URL: "http://localhost:18080"},
			},
		})
		if err != nil {
			t.Fatalf("register failed: %v", err)
		}

		endpoint, err := reg.Discovery().GetHealthyEndpoint("single-service", EndpointHTTP)
		if err != nil {
			t.Fatalf("GetHealthyEndpoint failed: %v", err)
		}
		if endpoint != "http://localhost:18080" {
			t.Fatalf("unexpected endpoint: %q", endpoint)
		}
	})

	t.Run("run loop executes ticker branch", func(t *testing.T) {
		reg := New(DefaultConfig())
		var checks atomic.Int64
		healthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			checks.Add(1)
			w.WriteHeader(http.StatusOK)
		}))
		defer healthServer.Close()

		_, err := reg.Register(RegistrationRequest{
			ID:        "ticker-module",
			Name:      "ticker-module",
			Endpoints: []Endpoint{{Type: EndpointHTTP, URL: "http://localhost:8080"}},
			HealthURL: healthServer.URL,
		})
		if err != nil {
			t.Fatalf("register failed: %v", err)
		}

		hc := NewHealthChecker(reg, 10*time.Millisecond, 100*time.Millisecond)
		hc.initialDelay = 0
		hc.Start()
		time.Sleep(45 * time.Millisecond)
		hc.Stop()

		if checks.Load() < 2 {
			t.Fatalf("expected at least 2 checks (initial + ticker), got %d", checks.Load())
		}
	})

	t.Run("checkAll handles status update errors and unhealthy counts", func(t *testing.T) {
		reg := New(DefaultConfig())

		var moduleID string
		healthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if moduleID != "" {
				_, _ = reg.Deregister(moduleID)
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer healthServer.Close()

		registration, err := reg.Register(RegistrationRequest{
			ID:        "ephemeral-module",
			Name:      "ephemeral-module",
			Endpoints: []Endpoint{{Type: EndpointHTTP, URL: "http://localhost:8080"}},
			HealthURL: healthServer.URL,
		})
		if err != nil {
			t.Fatalf("register failed: %v", err)
		}
		moduleID = registration.ModuleID

		// Register a module without health URL to force a non-healthy result path.
		_, err = reg.Register(RegistrationRequest{
			ID:   "unknown-health",
			Name: "unknown-health",
			Endpoints: []Endpoint{
				{Type: EndpointHTTP, URL: "http://localhost:8081"},
			},
		})
		if err != nil {
			t.Fatalf("register unknown-health failed: %v", err)
		}

		hc := NewHealthChecker(reg, 50*time.Millisecond, 200*time.Millisecond)
		hc.checkAll()

		// Module can be removed by health handler; assert that checkAll completed and still processed remaining module.
		unknown, err := reg.GetModule("unknown-health")
		if err != nil {
			t.Fatalf("expected unknown-health module to remain registered: %v", err)
		}
		if unknown.Status != StatusUnknown {
			t.Fatalf("expected unknown-health module status to be unknown, got %s", unknown.Status)
		}
	})

	t.Run("checkModule handles malformed health URL request creation failure", func(t *testing.T) {
		reg := New(DefaultConfig())
		hc := NewHealthChecker(reg, time.Second, time.Second)

		result := hc.checkModule(&Module{
			ID:        "bad-url-module",
			Name:      "bad-url-module",
			HealthURL: "://invalid-url",
			Status:    StatusHealthy,
		})
		if result.Status != StatusUnhealthy {
			t.Fatalf("expected unhealthy status for malformed URL, got %s", result.Status)
		}
		if result.Error == "" {
			t.Fatalf("expected non-empty error for malformed URL")
		}
	})

	t.Run("checkModule logs recovered path from previously unhealthy status", func(t *testing.T) {
		reg := New(DefaultConfig())
		hc := NewHealthChecker(reg, time.Second, time.Second)

		healthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer healthServer.Close()

		result := hc.checkModule(&Module{
			ID:        "recovering-module",
			Name:      "recovering-module",
			HealthURL: healthServer.URL,
			Status:    StatusUnhealthy,
		})
		if result.Status != StatusHealthy {
			t.Fatalf("expected recovered module to become healthy, got %s", result.Status)
		}
	})

	t.Run("CheckNow returns status update error when module disappears", func(t *testing.T) {
		reg := New(DefaultConfig())

		moduleID := "checknow-disappears"
		healthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = reg.Deregister(moduleID)
			w.WriteHeader(http.StatusOK)
		}))
		defer healthServer.Close()

		_, err := reg.Register(RegistrationRequest{
			ID:        moduleID,
			Name:      moduleID,
			Endpoints: []Endpoint{{Type: EndpointHTTP, URL: "http://localhost:8080"}},
			HealthURL: healthServer.URL,
		})
		if err != nil {
			t.Fatalf("register failed: %v", err)
		}

		result, err := NewHealthChecker(reg, time.Second, time.Second).CheckNow(moduleID)
		if err == nil {
			t.Fatalf("expected status update error when module is removed during CheckNow")
		}
		if result != nil {
			t.Fatalf("expected nil result on status update failure")
		}
		if !errors.Is(err, ErrModuleNotFound) {
			t.Fatalf("expected ErrModuleNotFound, got %v", err)
		}
	})
}
