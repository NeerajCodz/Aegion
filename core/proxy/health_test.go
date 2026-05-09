package proxy

import (
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func stopProxyHealthCheckers(p *Proxy) {
	p.healthMux.RLock()
	checkers := make([]*HealthChecker, 0, len(p.healthCheckers))
	for _, checker := range p.healthCheckers {
		checkers = append(checkers, checker)
	}
	p.healthMux.RUnlock()

	for _, checker := range checkers {
		checker.Stop()
	}
}

func TestHealthChecker_SuccessfulCheck(t *testing.T) {
	// Create test server that returns healthy response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Aegion-Proxy-HealthChecker/1.0", r.Header.Get("User-Agent"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))
	defer server.Close()

	config := HealthCheckerConfig{
		URL:            server.URL + "/health",
		Interval:       100 * time.Millisecond,
		Timeout:        time.Second,
		ExpectedStatus: http.StatusOK,
		Logger:         slog.Default(),
	}

	hc := NewHealthChecker(config)

	// Perform initial check
	hc.performCheck()

	// Check status
	assert.Equal(t, HealthStatusHealthy, hc.GetStatus())

	metrics := hc.GetMetrics()
	assert.Equal(t, HealthStatusHealthy, metrics.Status)
	assert.Equal(t, int64(1), metrics.CheckCount)
	assert.Equal(t, int64(0), metrics.FailureCount)
	assert.Equal(t, float64(1.0), metrics.SuccessRate)
	assert.Nil(t, metrics.LastError)
	assert.True(t, metrics.IsHealthy())
}

func TestHealthChecker_FailedCheck(t *testing.T) {
	// Create test server that returns error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("Internal Server Error"))
	}))
	defer server.Close()

	config := HealthCheckerConfig{
		URL:            server.URL + "/health",
		Interval:       100 * time.Millisecond,
		Timeout:        time.Second,
		ExpectedStatus: http.StatusOK,
		Logger:         slog.Default(),
	}

	hc := NewHealthChecker(config)

	// Perform initial check
	hc.performCheck()

	// Check status
	assert.Equal(t, HealthStatusUnhealthy, hc.GetStatus())

	metrics := hc.GetMetrics()
	assert.Equal(t, HealthStatusUnhealthy, metrics.Status)
	assert.Equal(t, int64(1), metrics.CheckCount)
	assert.Equal(t, int64(1), metrics.FailureCount)
	assert.Equal(t, float64(0.0), metrics.SuccessRate)
	assert.EqualError(t, metrics.LastError, "unexpected health status 500")
	assert.False(t, metrics.IsHealthy())
}

func TestHealthChecker_ExpectedBodyMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not-ok"))
	}))
	defer server.Close()

	hc := NewHealthChecker(HealthCheckerConfig{
		URL:            server.URL + "/health",
		Timeout:        time.Second,
		ExpectedStatus: http.StatusOK,
		ExpectedBody:   "OK",
		Logger:         slog.Default(),
	})

	hc.performCheck()

	metrics := hc.GetMetrics()
	assert.Equal(t, HealthStatusUnhealthy, metrics.Status)
	assert.EqualError(t, metrics.LastError, "unexpected health response body")
}

func TestHealthChecker_ExpectedBodyMatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("READY"))
	}))
	defer server.Close()

	hc := NewHealthChecker(HealthCheckerConfig{
		URL:            server.URL + "/health",
		Timeout:        time.Second,
		ExpectedStatus: http.StatusOK,
		ExpectedBody:   "READY",
		Logger:         slog.Default(),
	})

	hc.performCheck()

	metrics := hc.GetMetrics()
	assert.Equal(t, HealthStatusHealthy, metrics.Status)
	assert.Nil(t, metrics.LastError)
}


func TestHealthChecker_ExpectedBodyTooLargeByContentLength(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "10")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))
	defer server.Close()

	hc := NewHealthChecker(HealthCheckerConfig{
		URL:            server.URL + "/health",
		Timeout:        time.Second,
		ExpectedStatus: http.StatusOK,
		ExpectedBody:   "OK",
		Logger:         slog.Default(),
	})

	hc.performCheck()

	metrics := hc.GetMetrics()
	assert.Equal(t, HealthStatusUnhealthy, metrics.Status)
	assert.EqualError(t, metrics.LastError, "unexpected health response body")
}

func TestHealthChecker_ExpectedBodyTooLargeByReadLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK!"))
	}))
	defer server.Close()

	hc := NewHealthChecker(HealthCheckerConfig{
		URL:            server.URL + "/health",
		Timeout:        time.Second,
		ExpectedStatus: http.StatusOK,
		ExpectedBody:   "OK",
		Logger:         slog.Default(),
	})

	hc.performCheck()

	metrics := hc.GetMetrics()
	assert.Equal(t, HealthStatusUnhealthy, metrics.Status)
	assert.EqualError(t, metrics.LastError, "unexpected health response body")
}

func TestHealthChecker_NetworkError(t *testing.T) {
	// Use a URL that will cause connection refused
	config := HealthCheckerConfig{
		URL:            "http://localhost:0/health", // Port 0 should be refused
		Interval:       100 * time.Millisecond,
		Timeout:        100 * time.Millisecond,
		ExpectedStatus: http.StatusOK,
		Logger:         slog.Default(),
	}

	hc := NewHealthChecker(config)

	// Perform initial check
	hc.performCheck()

	// Check status
	assert.Equal(t, HealthStatusUnhealthy, hc.GetStatus())

	metrics := hc.GetMetrics()
	assert.Equal(t, HealthStatusUnhealthy, metrics.Status)
	assert.Equal(t, int64(1), metrics.CheckCount)
	assert.Equal(t, int64(1), metrics.FailureCount)
	assert.NotNil(t, metrics.LastError)
}

func TestHealthChecker_CustomHeaders(t *testing.T) {
	// Create test server that checks for custom headers
	expectedHeaders := map[string]string{
		"X-Auth-Token": "secret123",
		"X-Client-ID":  "health-checker",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for key, expectedValue := range expectedHeaders {
			actualValue := r.Header.Get(key)
			assert.Equal(t, expectedValue, actualValue, "Header %s mismatch", key)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := HealthCheckerConfig{
		URL:            server.URL + "/health",
		Interval:       100 * time.Millisecond,
		Timeout:        time.Second,
		ExpectedStatus: http.StatusOK,
		Headers:        expectedHeaders,
		Logger:         slog.Default(),
	}

	hc := NewHealthChecker(config)

	// Perform check
	hc.performCheck()

	// Should be healthy if headers were correct
	assert.Equal(t, HealthStatusHealthy, hc.GetStatus())
}

func TestHealthChecker_CustomMethod(t *testing.T) {
	// Create test server that expects POST
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := HealthCheckerConfig{
		URL:            server.URL + "/health",
		Interval:       100 * time.Millisecond,
		Timeout:        time.Second,
		ExpectedStatus: http.StatusOK,
		Method:         "POST",
		Logger:         slog.Default(),
	}

	hc := NewHealthChecker(config)

	// Perform check
	hc.performCheck()

	// Should be healthy
	assert.Equal(t, HealthStatusHealthy, hc.GetStatus())
}

func TestHealthChecker_Start_Stop(t *testing.T) {
	// Create test server
	checkCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		checkCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := HealthCheckerConfig{
		URL:            server.URL + "/health",
		Interval:       50 * time.Millisecond,
		Timeout:        time.Second,
		ExpectedStatus: http.StatusOK,
		Logger:         slog.Default(),
	}

	hc := NewHealthChecker(config)

	// Start health checker in goroutine
	go hc.Start()

	// Wait for a few checks
	time.Sleep(200 * time.Millisecond)

	// Stop health checker
	hc.Stop()

	// Should have performed multiple checks
	assert.Greater(t, checkCount, 2, "should have performed multiple health checks")

	// Wait a bit more to ensure it stopped
	currentCount := checkCount
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, currentCount, checkCount, "health checks should have stopped")
}

func TestHealthChecker_StatusTransitions(t *testing.T) {
	// Create test server that can toggle health status
	isHealthy := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isHealthy {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()

	config := HealthCheckerConfig{
		URL:            server.URL + "/health",
		Interval:       100 * time.Millisecond,
		Timeout:        time.Second,
		ExpectedStatus: http.StatusOK,
		Logger:         slog.Default(),
	}

	hc := NewHealthChecker(config)

	// Initial state should be unknown
	assert.Equal(t, HealthStatusUnknown, hc.GetStatus())

	// First check - unhealthy
	hc.performCheck()
	assert.Equal(t, HealthStatusUnhealthy, hc.GetStatus())

	// Make server healthy and check again
	isHealthy = true
	hc.performCheck()
	assert.Equal(t, HealthStatusHealthy, hc.GetStatus())

	// Make server unhealthy again
	isHealthy = false
	hc.performCheck()
	assert.Equal(t, HealthStatusUnhealthy, hc.GetStatus())
}

func TestHealthChecker_Timeout(t *testing.T) {
	// Create test server that responds slowly
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond) // Slower than timeout
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := HealthCheckerConfig{
		URL:            server.URL + "/health",
		Interval:       100 * time.Millisecond,
		Timeout:        50 * time.Millisecond, // Short timeout
		ExpectedStatus: http.StatusOK,
		Logger:         slog.Default(),
	}

	hc := NewHealthChecker(config)

	// Perform check - should timeout
	start := time.Now()
	hc.performCheck()
	duration := time.Since(start)

	// Should fail due to timeout
	assert.Equal(t, HealthStatusUnhealthy, hc.GetStatus())

	// Should have timed out well before the upstream's full response latency.
	assert.Less(t, duration, 300*time.Millisecond)

	metrics := hc.GetMetrics()
	assert.NotNil(t, metrics.LastError)
}

func TestNewHealthChecker_DisablesRedirectFollowing(t *testing.T) {
	redirectFollowed := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/final", http.StatusFound)
			return
		}
		if r.URL.Path == "/final" {
			redirectFollowed = true
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	hc := NewHealthChecker(HealthCheckerConfig{
		URL:     server.URL + "/redirect",
		Timeout: time.Second,
		Logger:  slog.Default(),
	})

	resp, err := hc.client.Get(server.URL + "/redirect")
	require.NoError(t, err)
	defer func() {
		_ = resp.Body.Close()
	}()

	assert.Equal(t, http.StatusFound, resp.StatusCode)
	assert.False(t, redirectFollowed)
}

func TestHealthChecker_RecordFailure_AlreadyUnhealthy(t *testing.T) {
	hc := NewHealthChecker(HealthCheckerConfig{
		URL:    "http://example.com/health",
		Logger: slog.Default(),
	})

	firstErr := errors.New("first failure")
	secondErr := errors.New("second failure")

	hc.recordFailure(firstErr)
	hc.recordFailure(secondErr)

	metrics := hc.GetMetrics()
	assert.Equal(t, HealthStatusUnhealthy, metrics.Status)
	assert.Equal(t, int64(2), metrics.FailureCount)
	assert.Equal(t, secondErr, metrics.LastError)
}

func TestHealthStatus_String(t *testing.T) {
	tests := []struct {
		status   HealthStatus
		expected string
	}{
		{HealthStatusUnknown, "unknown"},
		{HealthStatusHealthy, "healthy"},
		{HealthStatusUnhealthy, "unhealthy"},
		{HealthStatus(999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.status.String())
		})
	}
}

func TestHealthMetrics_IsHealthy(t *testing.T) {
	tests := []struct {
		name     string
		status   HealthStatus
		expected bool
	}{
		{"healthy status", HealthStatusHealthy, true},
		{"unhealthy status", HealthStatusUnhealthy, false},
		{"unknown status", HealthStatusUnknown, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metrics := HealthMetrics{Status: tt.status}
			assert.Equal(t, tt.expected, metrics.IsHealthy())
		})
	}
}

func TestProxy_GetUpstreamHealth(t *testing.T) {
	// Create test upstream servers
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server1.Close()

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server2.Close()

	// Configure proxy
	config := DefaultConfig()
	config.EnableHealthChecks = true
	config.HealthCheckInterval = 100 * time.Millisecond
	config.Upstreams = map[string]Upstream{
		"healthy-service": {
			URL:         server1.URL,
			HealthCheck: "/health",
		},
		"unhealthy-service": {
			URL:         server2.URL,
			HealthCheck: "/health",
		},
	}

	logger := slog.Default()
	proxy := NewProxy(config, nil, logger)
	defer stopProxyHealthCheckers(proxy)

	require.Eventually(t, func() bool {
		health := proxy.GetUpstreamHealth()
		if len(health) != 2 {
			return false
		}
		statuses := map[string]HealthStatus{}
		for _, h := range health {
			statuses[h.Name] = h.Health.Status
		}
		return statuses["healthy-service"] == HealthStatusHealthy &&
			statuses["unhealthy-service"] == HealthStatusUnhealthy
	}, time.Second, 25*time.Millisecond)

	// Get upstream health
	health := proxy.GetUpstreamHealth()

	require.Len(t, health, 2)

	// Sort by name for consistent testing
	if health[0].Name > health[1].Name {
		health[0], health[1] = health[1], health[0]
	}

	// Check healthy service
	assert.Equal(t, "healthy-service", health[0].Name)
	assert.Equal(t, server1.URL, health[0].URL)
	assert.Equal(t, HealthStatusHealthy, health[0].Health.Status)

	// Check unhealthy service
	assert.Equal(t, "unhealthy-service", health[1].Name)
	assert.Equal(t, server2.URL, health[1].URL)
	assert.Equal(t, HealthStatusUnhealthy, health[1].Health.Status)
}

func TestProxy_IsUpstreamHealthy(t *testing.T) {
	// Create test upstream server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Configure proxy
	config := DefaultConfig()
	config.EnableHealthChecks = true
	config.HealthCheckInterval = 100 * time.Millisecond
	config.Upstreams = map[string]Upstream{
		"test-service": {
			URL:         server.URL,
			HealthCheck: "/health",
		},
	}

	logger := slog.Default()
	proxy := NewProxy(config, nil, logger)
	defer stopProxyHealthCheckers(proxy)

	// Test with health checking enabled (eventually healthy after async checker starts)
	require.Eventually(t, func() bool {
		return proxy.IsUpstreamHealthy("test-service")
	}, time.Second, 25*time.Millisecond)

	// Test with non-existent upstream
	assert.True(t, proxy.IsUpstreamHealthy("non-existent")) // Should assume healthy

	// Test with health checking disabled
	configNoHealth := DefaultConfig()
	configNoHealth.EnableHealthChecks = false
	proxyNoHealth := NewProxy(configNoHealth, nil, logger)
	assert.True(t, proxyNoHealth.IsUpstreamHealthy("any-service")) // Should assume healthy
}

func TestHealthCheckerConfig_Defaults(t *testing.T) {
	config := HealthCheckerConfig{
		URL:    "http://example.com/health",
		Logger: slog.Default(),
	}

	hc := NewHealthChecker(config)

	// Check that defaults were applied
	assert.Equal(t, 30*time.Second, hc.config.Interval)
	assert.Equal(t, 5*time.Second, hc.config.Timeout)
	assert.Equal(t, http.StatusOK, hc.config.ExpectedStatus)
	assert.Equal(t, "GET", hc.config.Method)
}

func BenchmarkHealthChecker_PerformCheck(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := HealthCheckerConfig{
		URL:            server.URL + "/health",
		Interval:       time.Minute,
		Timeout:        time.Second,
		ExpectedStatus: http.StatusOK,
		Logger:         slog.Default(),
	}

	hc := NewHealthChecker(config)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hc.performCheck()
	}
}
