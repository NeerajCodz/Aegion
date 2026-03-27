package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthChecker_SuccessfulCheck(t *testing.T) {
	// Create test server that returns healthy response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Aegion-Proxy-HealthChecker/1.0", r.Header.Get("User-Agent"))
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))
	defer server.Close()

	config := HealthCheckerConfig{
		URL:            server.URL + "/health",
		Interval:       100 * time.Millisecond,
		Timeout:        time.Second,
		ExpectedStatus: http.StatusOK,
		Logger:         zerolog.New(zerolog.NewTestWriter(t)),
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
		w.Write([]byte("Internal Server Error"))
	}))
	defer server.Close()

	config := HealthCheckerConfig{
		URL:            server.URL + "/health",
		Interval:       100 * time.Millisecond,
		Timeout:        time.Second,
		ExpectedStatus: http.StatusOK,
		Logger:         zerolog.New(zerolog.NewTestWriter(t)),
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
	assert.Nil(t, metrics.LastError) // No network error, just wrong status code
	assert.False(t, metrics.IsHealthy())
}

func TestHealthChecker_NetworkError(t *testing.T) {
	// Use a URL that will cause connection refused
	config := HealthCheckerConfig{
		URL:            "http://localhost:0/health", // Port 0 should be refused
		Interval:       100 * time.Millisecond,
		Timeout:        100 * time.Millisecond,
		ExpectedStatus: http.StatusOK,
		Logger:         zerolog.New(zerolog.NewTestWriter(t)),
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
		Logger:         zerolog.New(zerolog.NewTestWriter(t)),
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
		Logger:         zerolog.New(zerolog.NewTestWriter(t)),
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
		Logger:         zerolog.New(zerolog.NewTestWriter(t)),
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
		Logger:         zerolog.New(zerolog.NewTestWriter(t)),
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
		Logger:         zerolog.New(zerolog.NewTestWriter(t)),
	}

	hc := NewHealthChecker(config)

	// Perform check - should timeout
	start := time.Now()
	hc.performCheck()
	duration := time.Since(start)

	// Should fail due to timeout
	assert.Equal(t, HealthStatusUnhealthy, hc.GetStatus())
	
	// Should have timed out quickly
	assert.Less(t, duration, 100*time.Millisecond)

	metrics := hc.GetMetrics()
	assert.NotNil(t, metrics.LastError)
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

	logger := zerolog.New(zerolog.NewTestWriter(t))
	proxy := NewProxy(config, nil, logger)

	// Wait for initial health checks
	time.Sleep(150 * time.Millisecond)

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

	logger := zerolog.New(zerolog.NewTestWriter(t))
	proxy := NewProxy(config, nil, logger)

	// Test with health checking enabled
	time.Sleep(150 * time.Millisecond) // Wait for health check
	assert.True(t, proxy.IsUpstreamHealthy("test-service"))

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
		URL: "http://example.com/health",
		Logger: zerolog.New(zerolog.NewTestWriter(t)),
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
		Logger:         zerolog.New(zerolog.NewTestWriter(&testing.T{})),
	}

	hc := NewHealthChecker(config)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hc.performCheck()
	}
}