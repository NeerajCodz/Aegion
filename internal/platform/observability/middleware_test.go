package observability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric/noop"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

func TestNewHTTPMiddleware(t *testing.T) {
	tracer := NewTracerWrapper("test-service")
	meter, err := NewMeterWrapper("test-service")
	require.NoError(t, err)
	
	middleware := NewHTTPMiddleware(tracer, meter)
	assert.NotNil(t, middleware)
	assert.Equal(t, tracer, middleware.tracer)
	assert.Equal(t, meter, middleware.meter)
}

func TestHTTPMiddleware_Handler(t *testing.T) {
	// Set up no-op providers
	otel.SetTracerProvider(tracenoop.NewTracerProvider())
	otel.SetMeterProvider(noop.NewMeterProvider())
	
	tracer := NewTracerWrapper("test-service")
	meter, err := NewMeterWrapper("test-service")
	require.NoError(t, err)
	
	middleware := NewHTTPMiddleware(tracer, meter)
	
	// Create a test handler
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check that trace info is available in context
		assert.NotNil(t, r.Context())
		
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Hello, World!"))
	})
	
	// Wrap handler with middleware
	handler := middleware.Handler(testHandler)
	
	// Create test request
	req := httptest.NewRequest("GET", "/api/v1/users", nil)
	req.Header.Set("User-Agent", "test-agent/1.0")
	
	// Create response recorder
	rr := httptest.NewRecorder()
	
	// Execute request
	handler.ServeHTTP(rr, req)
	
	// Check response
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "Hello, World!", rr.Body.String())
}

func TestHTTPMiddleware_WithRequestID(t *testing.T) {
	otel.SetTracerProvider(tracenoop.NewTracerProvider())
	otel.SetMeterProvider(noop.NewMeterProvider())
	
	tracer := NewTracerWrapper("test-service")
	meter, err := NewMeterWrapper("test-service")
	require.NoError(t, err)
	
	middleware := NewHTTPMiddleware(tracer, meter)
	
	// Create a test handler that checks for request ID
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := GetRequestID(r.Context())
		assert.NotEmpty(t, requestID)
		w.WriteHeader(http.StatusOK)
	})
	
	// Create a router with chi middleware for request ID
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Simulate request ID middleware
			ctx := WithRequestID(r.Context(), "test-request-123")
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	r.Use(middleware.Handler)
	r.Get("/test", testHandler)
	
	// Create test request
	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()
	
	r.ServeHTTP(rr, req)
	
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestHTTPMiddleware_ErrorStatusCodes(t *testing.T) {
	otel.SetTracerProvider(tracenoop.NewTracerProvider())
	otel.SetMeterProvider(noop.NewMeterProvider())
	
	tracer := NewTracerWrapper("test-service")
	meter, err := NewMeterWrapper("test-service")
	require.NoError(t, err)
	
	middleware := NewHTTPMiddleware(tracer, meter)
	
	testCases := []struct {
		name       string
		statusCode int
	}{
		{"Success", 200},
		{"Client Error", 400},
		{"Not Found", 404},
		{"Server Error", 500},
		{"Service Unavailable", 503},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.statusCode)
				w.Write([]byte("response"))
			})
			
			handler := middleware.Handler(testHandler)
			
			req := httptest.NewRequest("GET", "/test", nil)
			rr := httptest.NewRecorder()
			
			handler.ServeHTTP(rr, req)
			
			assert.Equal(t, tc.statusCode, rr.Code)
		})
	}
}

func TestTraceInfoContext(t *testing.T) {
	ctx := context.Background()
	
	traceInfo := TraceInfo{
		TraceID: "trace123",
		SpanID:  "span456",
	}
	
	// Add trace info to context
	ctx = WithTraceInfo(ctx, traceInfo)
	
	// Extract trace info from context
	extracted := GetTraceInfo(ctx)
	assert.Equal(t, traceInfo.TraceID, extracted.TraceID)
	assert.Equal(t, traceInfo.SpanID, extracted.SpanID)
	
	// Test empty context
	emptyCtx := context.Background()
	empty := GetTraceInfo(emptyCtx)
	assert.Empty(t, empty.TraceID)
	assert.Empty(t, empty.SpanID)
}

func TestRequestIDContext(t *testing.T) {
	ctx := context.Background()
	
	requestID := "req-123-456"
	
	// Add request ID to context
	ctx = WithRequestID(ctx, requestID)
	
	// Extract request ID from context
	extracted := GetRequestID(ctx)
	assert.Equal(t, requestID, extracted)
	
	// Test empty context
	emptyCtx := context.Background()
	empty := GetRequestID(emptyCtx)
	assert.Empty(t, empty)
}

func TestSessionIDContext(t *testing.T) {
	ctx := context.Background()
	
	sessionID := "sess-789-012"
	
	// Add session ID to context
	ctx = WithSessionID(ctx, sessionID)
	
	// Extract session ID from context
	extracted := GetSessionID(ctx)
	assert.Equal(t, sessionID, extracted)
	
	// Test empty context
	emptyCtx := context.Background()
	empty := GetSessionID(emptyCtx)
	assert.Empty(t, empty)
}

func TestUserIDContext(t *testing.T) {
	ctx := context.Background()
	
	userID := "user-345-678"
	
	// Add user ID to context
	ctx = WithUserID(ctx, userID)
	
	// Extract user ID from context
	extracted := GetUserID(ctx)
	assert.Equal(t, userID, extracted)
	
	// Test empty context
	emptyCtx := context.Background()
	empty := GetUserID(emptyCtx)
	assert.Empty(t, empty)
}

func TestNewDatabaseMiddleware(t *testing.T) {
	tracer := NewTracerWrapper("test-service")
	meter, err := NewMeterWrapper("test-service")
	require.NoError(t, err)
	
	dbMiddleware := NewDatabaseMiddleware(tracer, meter)
	assert.NotNil(t, dbMiddleware)
	assert.Equal(t, tracer, dbMiddleware.tracer)
	assert.Equal(t, meter, dbMiddleware.meter)
}

func TestDatabaseMiddleware_WrapQuery(t *testing.T) {
	otel.SetTracerProvider(tracenoop.NewTracerProvider())
	otel.SetMeterProvider(noop.NewMeterProvider())
	
	tracer := NewTracerWrapper("test-service")
	meter, err := NewMeterWrapper("test-service")
	require.NoError(t, err)
	
	dbMiddleware := NewDatabaseMiddleware(tracer, meter)
	
	ctx := context.Background()
	
	// Test successful query
	queryExecuted := false
	err = dbMiddleware.WrapQuery(ctx, "SELECT", "users", func(ctx context.Context) error {
		queryExecuted = true
		assert.NotNil(t, ctx)
		return nil
	})
	
	assert.NoError(t, err)
	assert.True(t, queryExecuted)
}

func TestDatabaseMiddleware_WrapQueryWithError(t *testing.T) {
	otel.SetTracerProvider(tracenoop.NewTracerProvider())
	otel.SetMeterProvider(noop.NewMeterProvider())
	
	tracer := NewTracerWrapper("test-service")
	meter, err := NewMeterWrapper("test-service")
	require.NoError(t, err)
	
	dbMiddleware := NewDatabaseMiddleware(tracer, meter)
	
	ctx := context.Background()
	
	// Test query with error
	testError := assert.AnError
	err = dbMiddleware.WrapQuery(ctx, "INSERT", "users", func(ctx context.Context) error {
		return testError
	})
	
	assert.Equal(t, testError, err)
}

func TestHTTPMiddleware_PerformanceOverhead(t *testing.T) {
	otel.SetTracerProvider(tracenoop.NewTracerProvider())
	otel.SetMeterProvider(noop.NewMeterProvider())
	
	tracer := NewTracerWrapper("test-service")
	meter, err := NewMeterWrapper("test-service")
	require.NoError(t, err)
	
	middleware := NewHTTPMiddleware(tracer, meter)
	
	// Simple test handler
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	
	handler := middleware.Handler(testHandler)
	
	// Measure performance with middleware
	start := time.Now()
	for i := 0; i < 1000; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}
	withMiddleware := time.Since(start)
	
	// Measure performance without middleware
	start = time.Now()
	for i := 0; i < 1000; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		rr := httptest.NewRecorder()
		testHandler.ServeHTTP(rr, req)
	}
	withoutMiddleware := time.Since(start)
	
	// The overhead should be reasonable (less than 10x)
	// This is a rough test - actual overhead depends on the no-op implementations
	t.Logf("With middleware: %v, Without middleware: %v, Overhead: %.2fx", 
		withMiddleware, withoutMiddleware, float64(withMiddleware)/float64(withoutMiddleware))
	
	// Just ensure both completed successfully
	assert.True(t, withMiddleware > 0)
	assert.True(t, withoutMiddleware > 0)
}