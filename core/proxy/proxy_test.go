package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aegion/aegion/core/session"
)

func TestProxy_ServeHTTP(t *testing.T) {
	// Create test upstream server
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"message": "upstream response"}`))
	}))
	defer upstream.Close()

	// Configure proxy
	config := DefaultConfig()
	config.Upstreams = map[string]Upstream{
		"test-upstream": {
			URL:         upstream.URL,
			HealthCheck: "/health",
			Weight:      100,
		},
	}
	config.DefaultTarget = "test-upstream"

	// Create rules
	rules := []Rule{
		{
			ID:      "api-rule",
			Path:    "/api/*",
			Methods: []string{"GET", "POST"},
			Target:  "test-upstream",
			Enabled: true,
			Priority: 100,
		},
	}
	engine := NewRuleEngine(rules)

	// Create proxy
	logger := zerolog.New(zerolog.NewTestWriter(t))
	proxy := NewProxy(config, engine, logger)

	tests := []struct {
		name           string
		method         string
		path           string
		expectedStatus int
		expectedHeader string
	}{
		{
			name:           "successful proxy to upstream",
			method:         "GET",
			path:           "/api/users",
			expectedStatus: http.StatusOK,
			expectedHeader: "application/json",
		},
		{
			name:           "POST request to API",
			method:         "POST",
			path:           "/api/users",
			expectedStatus: http.StatusOK,
			expectedHeader: "application/json",
		},
		{
			name:           "no rule matched",
			method:         "GET",
			path:           "/invalid",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "method not allowed",
			method:         "DELETE",
			path:           "/api/users",
			expectedStatus: http.StatusNotFound, // No rule matches DELETE
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			w := httptest.NewRecorder()

			proxy.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			
			if tt.expectedHeader != "" {
				assert.Equal(t, tt.expectedHeader, w.Header().Get("Content-Type"))
			}

			// Check that request ID header is set
			assert.NotEmpty(t, w.Header().Get("X-Request-ID"))
		})
	}
}

func TestProxy_Forward(t *testing.T) {
	// Create test upstream server
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check forwarded headers
		assert.NotEmpty(t, r.Header.Get("X-Request-ID"))
		assert.NotEmpty(t, r.Header.Get("X-Forwarded-For"))
		assert.NotEmpty(t, r.Header.Get("X-Forwarded-Proto"))
		
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("upstream response"))
	}))
	defer upstream.Close()

	// Parse upstream URL
	targetURL, err := url.Parse(upstream.URL)
	require.NoError(t, err)

	// Create proxy
	config := DefaultConfig()
	proxy := NewProxy(config, nil, zerolog.New(zerolog.NewTestWriter(t)))

	// Create test request
	req := httptest.NewRequest("GET", "/test", nil)
	req = req.WithContext(context.WithValue(req.Context(), "request_id", "test-123"))
	w := httptest.NewRecorder()

	// Create test rule and upstream
	rule := &Rule{
		ID:      "test",
		Headers: map[string]string{"X-Custom": "value"},
	}
	upstream_config := Upstream{
		Headers: map[string]string{"X-Upstream": "header"},
	}

	// Forward request
	err = proxy.Forward(targetURL, w, req, rule, upstream_config)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "upstream response", w.Body.String())
}

func TestProxy_ForwardWithSessionHeaders(t *testing.T) {
	// Create session IDs
	sessionID := uuid.New()
	identityID := uuid.New()

	// Create test upstream server that checks session headers
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, sessionID.String(), r.Header.Get("X-Aegion-Session-ID"))
		assert.Equal(t, identityID.String(), r.Header.Get("X-Aegion-Identity-ID"))
		assert.Equal(t, string(session.AAL2), r.Header.Get("X-Aegion-AAL"))
		
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("authenticated response"))
	}))
	defer upstream.Close()

	// Parse upstream URL
	targetURL, err := url.Parse(upstream.URL)
	require.NoError(t, err)

	// Create session
	sess := &session.Session{
		ID:         sessionID,
		IdentityID: identityID,
		AAL:        session.AAL2,
		AuthenticatedAt: time.Now(),
	}

	// Create proxy
	config := DefaultConfig()
	proxy := NewProxy(config, nil, zerolog.New(zerolog.NewTestWriter(t)))

	// Create test request with session
	req := httptest.NewRequest("GET", "/test", nil)
	req = req.WithContext(session.WithSession(req.Context(), sess))
	req = req.WithContext(context.WithValue(req.Context(), "request_id", "test-123"))
	w := httptest.NewRecorder()

	// Forward request
	rule := &Rule{ID: "test"}
	upstream_config := Upstream{}
	err = proxy.Forward(targetURL, w, req, rule, upstream_config)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestProxy_ForwardWithPathRewrite(t *testing.T) {
	// Create test upstream server
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check that path was rewritten
		assert.Equal(t, "/v2/users", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	// Parse upstream URL
	targetURL, err := url.Parse(upstream.URL)
	require.NoError(t, err)

	// Create proxy
	config := DefaultConfig()
	proxy := NewProxy(config, nil, zerolog.New(zerolog.NewTestWriter(t)))

	// Create test request
	req := httptest.NewRequest("GET", "/api/v1/users", nil)
	req = req.WithContext(context.WithValue(req.Context(), "request_id", "test-123"))
	w := httptest.NewRecorder()

	// Create rule with rewrite
	rule := &Rule{
		ID: "rewrite-test",
		Rewrite: &RewriteConfig{
			StripPrefix: "/api/v1",
			AddPrefix:   "/v2",
		},
	}
	upstream_config := Upstream{}

	// Forward request
	err = proxy.Forward(targetURL, w, req, rule, upstream_config)
	require.NoError(t, err)
}

func TestProxy_CircuitBreakerIntegration(t *testing.T) {
	// Create failing upstream server
	var requestCount int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount <= 3 { // Fail first 3 actual requests that reach the server
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	// Configure proxy with circuit breaker
	config := DefaultConfig()
	config.Upstreams = map[string]Upstream{
		"failing-upstream": {
			URL: upstream.URL,
			CircuitBreaker: &CircuitBreakerConfig{
				FailureThreshold: 3,
				Timeout:          100 * time.Millisecond,
				SuccessThreshold: 2,
			},
		},
	}

	// Create rules
	rules := []Rule{
		{
			ID:      "fail-rule",
			Path:    "/fail",
			Target:  "failing-upstream",
			Enabled: true,
			Priority: 100,
		},
	}
	engine := NewRuleEngine(rules)

	// Create proxy
	logger := zerolog.New(zerolog.NewTestWriter(t))
	proxy := NewProxy(config, engine, logger)

	// Make requests to trigger circuit breaker
	for i := 0; i < 6; i++ {
		req := httptest.NewRequest("GET", "/fail", nil)
		w := httptest.NewRecorder()
		proxy.ServeHTTP(w, req)
		
		if i < 3 {
			// First 3 requests should get through but get upstream error
			assert.Equal(t, http.StatusInternalServerError, w.Code) // Upstream returns 500
		} else {
			// After 3 failures, circuit breaker should open
			assert.Equal(t, http.StatusServiceUnavailable, w.Code)
		}
	}

	// Wait for circuit breaker timeout
	time.Sleep(150 * time.Millisecond)

	// Next request should go through (half-open state)
	req := httptest.NewRequest("GET", "/fail", nil)
	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, req)
	
	// This should succeed now as the upstream is "fixed" (4th actual request to server)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestProxy_RequestTimeout(t *testing.T) {
	// Create slow upstream server
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond) // Slower than timeout
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	// Configure proxy with short timeout
	config := DefaultConfig()
	config.Timeout = 50 * time.Millisecond
	config.Upstreams = map[string]Upstream{
		"slow-upstream": {
			URL: upstream.URL,
		},
	}

	// Create rules
	rules := []Rule{
		{
			ID:      "timeout-rule",
			Path:    "/slow",
			Target:  "slow-upstream",
			Enabled: true,
			Priority: 100,
		},
	}
	engine := NewRuleEngine(rules)

	// Create proxy
	logger := zerolog.New(zerolog.NewTestWriter(t))
	proxy := NewProxy(config, engine, logger)

	// Make request that should timeout
	req := httptest.NewRequest("GET", "/slow", nil)
	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, req)

	assert.Equal(t, http.StatusGatewayTimeout, w.Code)
	assert.Contains(t, w.Body.String(), "Request timeout")
}

func BenchmarkProxy_ServeHTTP(b *testing.B) {
	// Create test upstream server
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))
	defer upstream.Close()

	// Configure proxy
	config := DefaultConfig()
	config.Upstreams = map[string]Upstream{
		"bench-upstream": {
			URL: upstream.URL,
		},
	}

	rules := []Rule{
		{
			ID:      "bench-rule",
			Path:    "/api/*",
			Target:  "bench-upstream",
			Enabled: true,
			Priority: 100,
		},
	}
	engine := NewRuleEngine(rules)

	logger := zerolog.New(zerolog.NewTestWriter(&testing.T{}))
	proxy := NewProxy(config, engine, logger)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := httptest.NewRequest("GET", "/api/test", nil)
			w := httptest.NewRecorder()
			proxy.ServeHTTP(w, req)
		}
	})
}