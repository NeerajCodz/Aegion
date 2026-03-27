package registry

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthCheckerNew(t *testing.T) {
	registry := New(DefaultConfig())
	interval := 10 * time.Second
	timeout := 5 * time.Second

	hc := NewHealthChecker(registry, interval, timeout)

	assert.NotNil(t, hc)
	assert.Equal(t, interval, hc.interval)
	assert.Equal(t, timeout, hc.timeout)
	assert.Equal(t, registry, hc.registry)
	assert.NotNil(t, hc.client)
	assert.Equal(t, timeout, hc.client.Timeout)
}

func TestHealthCheckerStart(t *testing.T) {
	registry := New(DefaultConfig())
	hc := NewHealthChecker(registry, 1*time.Second, 1*time.Second)

	hc.Start()
	defer hc.Stop()

	assert.True(t, hc.running)

	// Starting again should be idempotent
	hc.Start()
	assert.True(t, hc.running)
}

func TestHealthCheckerStop(t *testing.T) {
	registry := New(DefaultConfig())
	hc := NewHealthChecker(registry, 1*time.Second, 1*time.Second)

	hc.Start()
	time.Sleep(100 * time.Millisecond)
	assert.True(t, hc.running)

	hc.Stop()
	assert.False(t, hc.running)

	// Stopping again should be idempotent
	hc.Stop()
	assert.False(t, hc.running)
}

func TestHealthCheckerStopWithoutStart(t *testing.T) {
	registry := New(DefaultConfig())
	hc := NewHealthChecker(registry, 1*time.Second, 1*time.Second)

	// Should not panic
	hc.Stop()
	assert.False(t, hc.running)
}

func TestHealthCheckerCheckModuleHealthy(t *testing.T) {
	registry := New(DefaultConfig())
	hc := NewHealthChecker(registry, 1*time.Second, 1*time.Second)

	// Mock health server
	healthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer healthServer.Close()

	module := &Module{
		ID:        "test-module",
		Name:      "Test Module",
		HealthURL: healthServer.URL,
		Status:    StatusStarting,
	}

	result := hc.checkModule(module)

	assert.Equal(t, "test-module", result.ModuleID)
	assert.Equal(t, StatusHealthy, result.Status)
	assert.Empty(t, result.Error)
	assert.Greater(t, result.Latency, time.Duration(0))
}

func TestHealthCheckerCheckModuleUnhealthy(t *testing.T) {
	registry := New(DefaultConfig())
	hc := NewHealthChecker(registry, 1*time.Second, 1*time.Second)

	// Mock health server returning 500
	healthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer healthServer.Close()

	module := &Module{
		ID:        "test-module",
		Name:      "Test Module",
		HealthURL: healthServer.URL,
		Status:    StatusHealthy,
	}

	result := hc.checkModule(module)

	assert.Equal(t, "test-module", result.ModuleID)
	assert.Equal(t, StatusUnhealthy, result.Status)
	assert.NotEmpty(t, result.Error)
	assert.Contains(t, result.Error, "non-2xx status")
}

func TestHealthCheckerCheckModuleTimeout(t *testing.T) {
	registry := New(DefaultConfig())
	hc := NewHealthChecker(registry, 1*time.Second, 100*time.Millisecond)

	// Mock health server that delays
	healthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer healthServer.Close()

	module := &Module{
		ID:        "test-module",
		Name:      "Test Module",
		HealthURL: healthServer.URL,
		Status:    StatusHealthy,
	}

	result := hc.checkModule(module)

	assert.Equal(t, "test-module", result.ModuleID)
	assert.Equal(t, StatusUnhealthy, result.Status)
	assert.NotEmpty(t, result.Error)
	assert.Contains(t, result.Error, "context deadline exceeded")
}

func TestHealthCheckerCheckModuleNoHealthURL(t *testing.T) {
	registry := New(DefaultConfig())
	hc := NewHealthChecker(registry, 1*time.Second, 1*time.Second)

	module := &Module{
		ID:     "test-module",
		Name:   "Test Module",
		Status: StatusStarting,
	}

	result := hc.checkModule(module)

	assert.Equal(t, "test-module", result.ModuleID)
	assert.Equal(t, StatusUnknown, result.Status)
	assert.NotEmpty(t, result.Error)
	assert.Contains(t, result.Error, "no health URL configured")
}

func TestHealthCheckerCheckModuleInvalidURL(t *testing.T) {
	registry := New(DefaultConfig())
	hc := NewHealthChecker(registry, 1*time.Second, 1*time.Second)

	module := &Module{
		ID:        "test-module",
		Name:      "Test Module",
		HealthURL: "http://invalid.local/health",
		Status:    StatusHealthy,
	}

	result := hc.checkModule(module)

	assert.Equal(t, "test-module", result.ModuleID)
	assert.Equal(t, StatusUnhealthy, result.Status)
	assert.NotEmpty(t, result.Error)
}

func TestHealthCheckerCheckNow(t *testing.T) {
	registry := New(DefaultConfig())
	hc := NewHealthChecker(registry, 1*time.Second, 1*time.Second)

	healthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer healthServer.Close()

	// Register module
	req := RegistrationRequest{
		ID:        "test-module",
		Name:      "Test Module",
		Endpoints: []Endpoint{{Type: EndpointHTTP, URL: "http://localhost:8080"}},
		HealthURL: healthServer.URL,
	}
	_, err := registry.Register(req)
	require.NoError(t, err)

	result, err := hc.CheckNow("test-module")

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "test-module", result.ModuleID)
	assert.Equal(t, StatusHealthy, result.Status)

	// Verify status was updated
	module, _ := registry.GetModule("test-module")
	assert.Equal(t, StatusHealthy, module.Status)
}

func TestHealthCheckerCheckNowNotFound(t *testing.T) {
	registry := New(DefaultConfig())
	hc := NewHealthChecker(registry, 1*time.Second, 1*time.Second)

	result, err := hc.CheckNow("nonexistent")

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, ErrModuleNotFound, err)
}

func TestHealthCheckerSetInterval(t *testing.T) {
	registry := New(DefaultConfig())
	hc := NewHealthChecker(registry, 1*time.Second, 1*time.Second)

	newInterval := 30 * time.Second
	hc.SetInterval(newInterval)

	assert.Equal(t, newInterval, hc.interval)
}

func TestHealthCheckerSetTimeout(t *testing.T) {
	registry := New(DefaultConfig())
	hc := NewHealthChecker(registry, 1*time.Second, 1*time.Second)

	newTimeout := 10 * time.Second
	hc.SetTimeout(newTimeout)

	assert.Equal(t, newTimeout, hc.timeout)
	assert.Equal(t, newTimeout, hc.client.Timeout)
}

func TestHealthCheckerGetInterval(t *testing.T) {
	interval := 15 * time.Second
	registry := New(DefaultConfig())
	hc := NewHealthChecker(registry, interval, 1*time.Second)

	retrieved := hc.GetInterval()

	assert.Equal(t, interval, retrieved)
}

func TestHealthCheckerGetTimeout(t *testing.T) {
	timeout := 7 * time.Second
	registry := New(DefaultConfig())
	hc := NewHealthChecker(registry, 1*time.Second, timeout)

	retrieved := hc.GetTimeout()

	assert.Equal(t, timeout, retrieved)
}

func TestHealthCheckerCheckAll(t *testing.T) {
	registry := New(DefaultConfig())
	hc := NewHealthChecker(registry, 1*time.Second, 1*time.Second)

	healthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer healthServer.Close()

	// Register multiple modules
	for i := 0; i < 3; i++ {
		req := RegistrationRequest{
			ID:        "module-" + string(rune(i)),
			Name:      "Module " + string(rune(i)),
			Endpoints: []Endpoint{{Type: EndpointHTTP, URL: "http://localhost:8080"}},
			HealthURL: healthServer.URL,
		}
		_, err := registry.Register(req)
		require.NoError(t, err)
	}

	hc.checkAll()

	// Verify all modules are healthy
	for i := 0; i < 3; i++ {
		module, _ := registry.GetModule("module-" + string(rune(i)))
		assert.Equal(t, StatusHealthy, module.Status)
	}
}

func TestHealthCheckerCheckAllNoModules(t *testing.T) {
	registry := New(DefaultConfig())
	hc := NewHealthChecker(registry, 1*time.Second, 1*time.Second)

	// Should not panic with no modules
	hc.checkAll()
	assert.Equal(t, 0, registry.ModuleCount())
}

func TestHealthCheckerRecovery(t *testing.T) {
	registry := New(DefaultConfig())
	hc := NewHealthChecker(registry, 1*time.Second, 1*time.Second)

	counter := 0
	healthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		counter++
		if counter <= 1 {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer healthServer.Close()

	module := &Module{
		ID:        "test-module",
		Name:      "Test Module",
		HealthURL: healthServer.URL,
		Status:    StatusHealthy,
	}

	// First check: unhealthy
	result1 := hc.checkModule(module)
	assert.Equal(t, StatusUnhealthy, result1.Status)

	// Second check: healthy (recovery)
	result2 := hc.checkModule(module)
	assert.Equal(t, StatusHealthy, result2.Status)
}

func TestHealthCheckerConcurrentChecks(t *testing.T) {
	registry := New(DefaultConfig())
	hc := NewHealthChecker(registry, 1*time.Second, 1*time.Second)

	healthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer healthServer.Close()

	// Register 5 modules
	for i := 0; i < 5; i++ {
		req := RegistrationRequest{
			ID:        "module-" + string(rune('0'+i)),
			Name:      "Module " + string(rune('0'+i)),
			Endpoints: []Endpoint{{Type: EndpointHTTP, URL: "http://localhost:8080"}},
			HealthURL: healthServer.URL,
		}
		_, err := registry.Register(req)
		require.NoError(t, err)
	}

	start := time.Now()
	hc.checkAll()
	duration := time.Since(start)

	// All checks should complete in parallel (around 50ms), not sequentially (250ms)
	assert.Less(t, duration, 200*time.Millisecond)
}

func TestHealthCheckerContextTimeout(t *testing.T) {
	registry := New(DefaultConfig())
	hc := NewHealthChecker(registry, 1*time.Second, 100*time.Millisecond)

	// Create a server that never responds
	healthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(10 * time.Second):
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer healthServer.Close()

	module := &Module{
		ID:        "test-module",
		Name:      "Test Module",
		HealthURL: healthServer.URL,
		Status:    StatusHealthy,
	}

	result := hc.checkModule(module)

	assert.Equal(t, StatusUnhealthy, result.Status)
	assert.NotEmpty(t, result.Error)
}

func TestHealthCheckerModuleStatusCodeBoundaries(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		isHealthy  bool
	}{
		{"200 OK", http.StatusOK, true},
		{"201 Created", http.StatusCreated, true},
		{"204 No Content", http.StatusNoContent, true},
		{"299 Custom 2xx", 299, true},
		{"300 Multiple Choices", http.StatusMultipleChoices, false},
		{"400 Bad Request", http.StatusBadRequest, false},
		{"500 Internal Error", http.StatusInternalServerError, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := New(DefaultConfig())
			hc := NewHealthChecker(registry, 1*time.Second, 1*time.Second)

			healthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))
			defer healthServer.Close()

			module := &Module{
				ID:        "test-module",
				Name:      "Test Module",
				HealthURL: healthServer.URL,
				Status:    StatusHealthy,
			}

			result := hc.checkModule(module)

			if tt.isHealthy {
				assert.Equal(t, StatusHealthy, result.Status, tt.name)
				assert.Empty(t, result.Error, tt.name)
			} else {
				assert.Equal(t, StatusUnhealthy, result.Status, tt.name)
			}
		})
	}
}

func TestHealthCheckerWithContext(t *testing.T) {
	registry := New(DefaultConfig())
	hc := NewHealthChecker(registry, 1*time.Second, 1*time.Second)

	healthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify context is passed
		select {
		case <-r.Context().Done():
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer healthServer.Close()

	module := &Module{
		ID:        "test-module",
		Name:      "Test Module",
		HealthURL: healthServer.URL,
		Status:    StatusStarting,
	}

	result := hc.checkModule(module)

	assert.Equal(t, StatusHealthy, result.Status)
	assert.Empty(t, result.Error)
}

func TestHealthCheckerLatencyMeasurement(t *testing.T) {
	registry := New(DefaultConfig())
	hc := NewHealthChecker(registry, 1*time.Second, 5*time.Second)

	delay := 100 * time.Millisecond
	healthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(delay)
		w.WriteHeader(http.StatusOK)
	}))
	defer healthServer.Close()

	module := &Module{
		ID:        "test-module",
		Name:      "Test Module",
		HealthURL: healthServer.URL,
		Status:    StatusStarting,
	}

	result := hc.checkModule(module)

	assert.Greater(t, result.Latency, delay)
	assert.Less(t, result.Latency, delay+50*time.Millisecond)
}

func TestHealthCheckerStatusUpdate(t *testing.T) {
	registry := New(DefaultConfig())
	hc := NewHealthChecker(registry, 1*time.Second, 1*time.Second)

	healthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer healthServer.Close()

	req := RegistrationRequest{
		ID:        "test-module",
		Name:      "Test Module",
		Endpoints: []Endpoint{{Type: EndpointHTTP, URL: "http://localhost:8080"}},
		HealthURL: healthServer.URL,
	}
	_, err := registry.Register(req)
	require.NoError(t, err)

	module, _ := registry.GetModule("test-module")
	assert.Equal(t, StatusStarting, module.Status)

	_, err = hc.CheckNow("test-module")
	require.NoError(t, err)

	module, _ = registry.GetModule("test-module")
	assert.Equal(t, StatusHealthy, module.Status)
}
