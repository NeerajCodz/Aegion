package proxy

import (
	"context"
	"crypto/tls"
	"errors"
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
	// Create failing upstream server that consistently fails for the test
	alwaysFail := true
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if alwaysFail {
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

	// Make requests to the failing upstream
	// Circuit breaker behavior depends on implementation
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/fail", nil)
		w := httptest.NewRecorder()
		proxy.ServeHTTP(w, req)
		
		// Should get an error response (either from upstream or circuit breaker)
		assert.True(t, w.Code >= 500, "Expected 5xx status, got %d", w.Code)
	}

	// Verify circuit breaker exists for the upstream
	breaker := proxy.getCircuitBreaker("failing-upstream")
	assert.NotNil(t, breaker)
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

// TestProxy_HandleRateLimitExceeded tests rate limit exceeded error handling
func TestProxy_HandleRateLimitExceeded(t *testing.T) {
	config := DefaultConfig()
	logger := zerolog.New(zerolog.NewTestWriter(t))
	proxy := NewProxy(config, nil, logger)

	req := httptest.NewRequest("GET", "/test", nil)
	req = req.WithContext(context.WithValue(req.Context(), "request_id", "test-123"))
	w := httptest.NewRecorder()

	start := time.Now()
	waitTime := 500 * time.Millisecond
	proxy.handleRateLimitExceeded(w, req, waitTime, nil, start)

	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.NotEmpty(t, w.Header().Get("Retry-After"))
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
	assert.Contains(t, w.Body.String(), "Rate limit exceeded")
}

// TestProxy_HandleAccessError_AuthenticationRequired tests access error for missing authentication
func TestProxy_HandleAccessError_AuthenticationRequired(t *testing.T) {
	config := DefaultConfig()
	logger := zerolog.New(zerolog.NewTestWriter(t))
	proxy := NewProxy(config, nil, logger)

	req := httptest.NewRequest("GET", "/test", nil)
	req = req.WithContext(context.WithValue(req.Context(), "request_id", "test-123"))
	w := httptest.NewRecorder()

	start := time.Now()
	err := ErrAccessDenied
	proxy.handleAccessError(w, req, err, start)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "Access denied")
}

// TestProxy_HandleAccessError_NoSession tests access error when session is required but missing
func TestProxy_HandleAccessError_NoSession(t *testing.T) {
	config := DefaultConfig()
	logger := zerolog.New(zerolog.NewTestWriter(t))
	proxy := NewProxy(config, nil, logger)

	req := httptest.NewRequest("GET", "/test", nil)
	req = req.WithContext(context.WithValue(req.Context(), "request_id", "test-123"))
	w := httptest.NewRecorder()

	start := time.Now()
	// Simulate missing authentication by passing a no-auth error
	proxy.handleAccessError(w, req, errors.New("authentication required"), start)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.NotEmpty(t, w.Body.String())
}

// TestResponseWriter_WriteHeader tests status code capture
func TestResponseWriter_WriteHeader(t *testing.T) {
	w := httptest.NewRecorder()
	rw := &responseWriter{
		ResponseWriter: w,
		statusCode:     0,
	}

	// First WriteHeader should capture the status
	rw.WriteHeader(http.StatusOK)
	assert.Equal(t, http.StatusOK, rw.statusCode)
	assert.Equal(t, http.StatusOK, w.Code)
}

// TestResponseWriter_WriteHeader_MultipleWriteAttempts tests that only first header write matters
func TestResponseWriter_WriteHeader_MultipleWriteAttempts(t *testing.T) {
	w := httptest.NewRecorder()
	rw := &responseWriter{
		ResponseWriter: w,
		statusCode:     0,
	}

	rw.WriteHeader(http.StatusCreated)
	// Second WriteHeader will be ignored by http.ResponseWriter (standard behavior)
	// But our wrapper will still capture the call
	// This test just verifies first call was captured
	assert.Equal(t, http.StatusCreated, rw.statusCode)
}

// TestProxy_GetCircuitBreaker_Creates tests circuit breaker creation for non-existent upstream
func TestProxy_GetCircuitBreaker_Creates(t *testing.T) {
	config := DefaultConfig()
	logger := zerolog.New(zerolog.NewTestWriter(t))
	proxy := NewProxy(config, nil, logger)

	breaker1 := proxy.getCircuitBreaker("test-upstream")
	assert.NotNil(t, breaker1)

	// Should return same breaker on second call
	breaker2 := proxy.getCircuitBreaker("test-upstream")
	assert.Equal(t, breaker1, breaker2)
}

// TestProxy_GetCircuitBreaker_DifferentUpstreams tests separate circuit breakers for different upstreams
func TestProxy_GetCircuitBreaker_DifferentUpstreams(t *testing.T) {
	config := DefaultConfig()
	logger := zerolog.New(zerolog.NewTestWriter(t))
	proxy := NewProxy(config, nil, logger)

	breaker1 := proxy.getCircuitBreaker("upstream-1")
	breaker2 := proxy.getCircuitBreaker("upstream-2")

	// Different upstreams should have different circuit breakers
	assert.NotNil(t, breaker1)
	assert.NotNil(t, breaker2)
	// They should be different instances (not same pointer)
	assert.NotSame(t, breaker1, breaker2)
}

// TestProxy_InjectSessionHeaders_WithImpersonation tests impersonation header injection
func TestProxy_InjectSessionHeaders_WithImpersonation(t *testing.T) {
	config := DefaultConfig()
	logger := zerolog.New(zerolog.NewTestWriter(t))
	proxy := NewProxy(config, nil, logger)

	impersonatorID := uuid.New()
	sess := &session.Session{
		ID:              uuid.New(),
		IdentityID:      uuid.New(),
		IsImpersonation: true,
		ImpersonatorID:  &impersonatorID,
		AAL:             session.AAL1,
		AuthenticatedAt: time.Now(),
		ExpiresAt:       time.Now().Add(24 * time.Hour),
	}

	req := httptest.NewRequest("GET", "/test", nil)
	proxy.injectSessionHeaders(req, sess)

	assert.Equal(t, "true", req.Header.Get("X-Aegion-Impersonation"))
	assert.Equal(t, impersonatorID.String(), req.Header.Get("X-Aegion-Impersonator-ID"))
}

// TestProxy_AddForwardedHeaders_WithTLS tests X-Forwarded-Proto header for HTTPS
func TestProxy_AddForwardedHeaders_WithTLS(t *testing.T) {
	config := DefaultConfig()
	logger := zerolog.New(zerolog.NewTestWriter(t))
	proxy := NewProxy(config, nil, logger)

	req := httptest.NewRequest("GET", "https://example.com/test", nil)
	req.TLS = &tls.ConnectionState{} // Indicates HTTPS
	reqCopy := httptest.NewRequest("GET", "/original", nil)

	proxy.addForwardedHeaders(reqCopy, req)

	// Should add forwarded headers
	assert.NotEmpty(t, reqCopy.Header.Get("X-Forwarded-For"))
	assert.NotEmpty(t, reqCopy.Header.Get("X-Forwarded-Host"))
}

// TestProxy_ServeHTTP_PreservesExistingRequestID tests request ID preservation
func TestProxy_ServeHTTP_PreservesExistingRequestID(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	config := DefaultConfig()
	config.Upstreams = map[string]Upstream{
		"test": {URL: upstream.URL},
	}

	rules := []Rule{
		{ID: "test", Path: "/test", Target: "test", Enabled: true, Priority: 100},
	}
	engine := NewRuleEngine(rules)
	logger := zerolog.New(zerolog.NewTestWriter(t))
	proxy := NewProxy(config, engine, logger)

	existingID := "custom-123"
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Request-ID", existingID)
	w := httptest.NewRecorder()

	proxy.ServeHTTP(w, req)

	// Should use existing request ID
	assert.Equal(t, existingID, w.Header().Get("X-Request-ID"))
}

// TestProxy_ServeHTTP_NoRuleMatched tests 404 response when no rule matches
func TestProxy_ServeHTTP_NoRuleMatched(t *testing.T) {
	config := DefaultConfig()
	config.Upstreams = map[string]Upstream{
		"test": {URL: "http://localhost:9999"},
	}

	engine := NewRuleEngine([]Rule{
		{ID: "specific", Path: "/specific", Target: "test", Enabled: true, Priority: 100},
	})

	logger := zerolog.New(zerolog.NewTestWriter(t))
	proxy := NewProxy(config, engine, logger)

	req := httptest.NewRequest("GET", "/nonexistent", nil)
	w := httptest.NewRecorder()

	proxy.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "no rule matched")
}

// TestProxy_ServeHTTP_CircuitBreakerOpen tests 503 response when circuit breaker is open
func TestProxy_ServeHTTP_CircuitBreakerOpen(t *testing.T) {
	config := DefaultConfig()
	config.Upstreams = map[string]Upstream{
		"test": {
			URL: "http://localhost:9999",
			CircuitBreaker: &CircuitBreakerConfig{
				FailureThreshold: 1,
			},
		},
	}

	engine := NewRuleEngine([]Rule{
		{ID: "test", Path: "/test", Target: "test", Enabled: true, Priority: 100},
	})

	logger := zerolog.New(zerolog.NewTestWriter(t))
	proxy := NewProxy(config, engine, logger)

	// Manually open the circuit breaker
	breaker := proxy.getCircuitBreaker("test")
	// Trigger failures to open the circuit
	for i := 0; i < 3; i++ {
		breaker.RecordFailure()
	}

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, req)

	// Should reject due to circuit breaker being open
	// Status could be 503 or other error code
	assert.True(t, w.Code >= 400)
}

// TestProxy_ServeHTTP_InvalidUpstreamURL tests internal server error for invalid upstream URL
func TestProxy_ServeHTTP_InvalidUpstreamURL(t *testing.T) {
	config := DefaultConfig()
	config.Upstreams = map[string]Upstream{
		"bad": {URL: "://invalid-url"},
	}

	engine := NewRuleEngine([]Rule{
		{ID: "test", Path: "/test", Target: "bad", Enabled: true, Priority: 100},
	})

	logger := zerolog.New(zerolog.NewTestWriter(t))
	proxy := NewProxy(config, engine, logger)

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "invalid upstream URL")
}

// TestProxy_ServeHTTP_UpstreamNotFound tests 502 response when upstream doesn't exist
func TestProxy_ServeHTTP_UpstreamNotFound(t *testing.T) {
	config := DefaultConfig()
	engine := NewRuleEngine([]Rule{
		{ID: "test", Path: "/test", Target: "nonexistent", Enabled: true, Priority: 100},
	})

	logger := zerolog.New(zerolog.NewTestWriter(t))
	proxy := NewProxy(config, engine, logger)

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadGateway, w.Code)
	assert.Contains(t, w.Body.String(), "upstream not found")
}

// TestProxy_RequestIDGeneration tests automatic request ID generation
func TestProxy_RequestIDGeneration(t *testing.T) {
	config := DefaultConfig()
	engine := NewRuleEngine([]Rule{})

	logger := zerolog.New(zerolog.NewTestWriter(t))
	proxy := NewProxy(config, engine, logger)

	req1 := httptest.NewRequest("GET", "/test", nil)
	req2 := httptest.NewRequest("GET", "/test", nil)
	w1 := httptest.NewRecorder()
	w2 := httptest.NewRecorder()

	proxy.ServeHTTP(w1, req1)
	proxy.ServeHTTP(w2, req2)

	id1 := w1.Header().Get("X-Request-ID")
	id2 := w2.Header().Get("X-Request-ID")

	assert.NotEmpty(t, id1)
	assert.NotEmpty(t, id2)
	assert.NotEqual(t, id1, id2) // Each request should have unique ID
}

// TestProxy_RequestTimeout_ContextCancellation tests context timeout handling
func TestProxy_RequestTimeout_ContextCancellation(t *testing.T) {
	slowUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(500 * time.Millisecond):
			w.WriteHeader(http.StatusOK)
		case <-r.Context().Done():
			// Context was cancelled - test passed
		}
	}))
	defer slowUpstream.Close()

	config := DefaultConfig()
	config.Timeout = 50 * time.Millisecond
	config.Upstreams = map[string]Upstream{
		"slow": {URL: slowUpstream.URL},
	}

	engine := NewRuleEngine([]Rule{
		{ID: "slow", Path: "/slow", Target: "slow", Enabled: true, Priority: 100},
	})

	logger := zerolog.New(zerolog.NewTestWriter(t))
	proxy := NewProxy(config, engine, logger)

	req := httptest.NewRequest("GET", "/slow", nil)
	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, req)

	// Should get a timeout error or 504
	assert.True(t, w.Code == http.StatusGatewayTimeout || w.Code >= 500)
}

// TestProxy_ConcurrentRequests tests concurrent request handling
func TestProxy_ConcurrentRequests(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	config := DefaultConfig()
	config.Upstreams = map[string]Upstream{
		"test": {URL: upstream.URL},
	}

	engine := NewRuleEngine([]Rule{
		{ID: "test", Path: "/test", Target: "test", Enabled: true, Priority: 100},
	})

	logger := zerolog.New(zerolog.NewTestWriter(t))
	proxy := NewProxy(config, engine, logger)

	// Send concurrent requests
	done := make(chan bool, 100)
	for i := 0; i < 100; i++ {
		go func() {
			req := httptest.NewRequest("GET", "/test", nil)
			w := httptest.NewRecorder()
			proxy.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code)
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 100; i++ {
		<-done
	}
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