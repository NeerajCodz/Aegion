package observability

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

func TestNewTracerWrapper(t *testing.T) {
	tracer := NewTracerWrapper("test-service")
	assert.NotNil(t, tracer)
	assert.Equal(t, "test-service", tracer.serviceName)
	assert.NotNil(t, tracer.tracer)
}

func TestTracerWrapper_StartSpan(t *testing.T) {
	// Set up a no-op tracer to avoid actual telemetry
	otel.SetTracerProvider(tracenoop.NewTracerProvider())
	
	tracer := NewTracerWrapper("test-service")
	ctx := context.Background()
	
	ctx, span := tracer.StartSpan(ctx, "test-operation")
	assert.NotNil(t, ctx)
	assert.NotNil(t, span)
	
	span.End()
}

func TestTracerWrapper_StartHTTPSpan(t *testing.T) {
	otel.SetTracerProvider(tracenoop.NewTracerProvider())
	
	tracer := NewTracerWrapper("test-service")
	req := httptest.NewRequest("GET", "/api/v1/users?limit=10", nil)
	req.Header.Set("User-Agent", "test-agent/1.0")
	req.Header.Set("X-Forwarded-For", "192.168.1.100")
	
	ctx, span := tracer.StartHTTPSpan(context.Background(), req)
	assert.NotNil(t, ctx)
	assert.NotNil(t, span)
	
	span.End()
}

func TestTracerWrapper_StartDatabaseSpan(t *testing.T) {
	otel.SetTracerProvider(tracenoop.NewTracerProvider())
	
	tracer := NewTracerWrapper("test-service")
	ctx := context.Background()
	
	ctx, span := tracer.StartDatabaseSpan(ctx, "SELECT", "users")
	assert.NotNil(t, ctx)
	assert.NotNil(t, span)
	
	span.End()
}

func TestTracerWrapper_StartServiceSpan(t *testing.T) {
	otel.SetTracerProvider(tracenoop.NewTracerProvider())
	
	tracer := NewTracerWrapper("test-service")
	ctx := context.Background()
	
	ctx, span := tracer.StartServiceSpan(ctx, "auth", "validate_token")
	assert.NotNil(t, ctx)
	assert.NotNil(t, span)
	
	span.End()
}

func TestTracerWrapper_StartModuleSpan(t *testing.T) {
	otel.SetTracerProvider(tracenoop.NewTracerProvider())
	
	tracer := NewTracerWrapper("test-service")
	ctx := context.Background()
	
	ctx, span := tracer.StartModuleSpan(ctx, "user-management", "create_user")
	assert.NotNil(t, ctx)
	assert.NotNil(t, span)
	
	span.End()
}

func TestTracerWrapper_FinishHTTPSpan(t *testing.T) {
	otel.SetTracerProvider(tracenoop.NewTracerProvider())
	
	tracer := NewTracerWrapper("test-service")
	_, span := tracer.StartSpan(context.Background(), "test")
	
	// Test various status codes
	testCases := []struct {
		statusCode   int
		responseSize int64
	}{
		{200, 1024},
		{400, 512},
		{500, 256},
	}
	
	for _, tc := range testCases {
		tracer.FinishHTTPSpan(span, tc.statusCode, tc.responseSize)
		// No assertions here since we're using no-op tracer
		// In real implementation, we'd check span attributes
	}
}

func TestTracerWrapper_TraceIDAndSpanID(t *testing.T) {
	otel.SetTracerProvider(tracenoop.NewTracerProvider())
	
	tracer := NewTracerWrapper("test-service")
	ctx := context.Background()
	
	// With no-op tracer, IDs should be empty
	traceID := tracer.TraceID(ctx)
	spanID := tracer.SpanID(ctx)
	
	assert.Empty(t, traceID)
	assert.Empty(t, spanID)
}

func TestTracerWrapper_TraceHeader(t *testing.T) {
	otel.SetTracerProvider(tracenoop.NewTracerProvider())
	
	tracer := NewTracerWrapper("test-service")
	ctx := context.Background()
	
	// With no-op tracer, header should be empty
	header := tracer.TraceHeader(ctx)
	assert.Empty(t, header)
}

func TestTracerWrapper_WithSpan(t *testing.T) {
	otel.SetTracerProvider(tracenoop.NewTracerProvider())
	
	tracer := NewTracerWrapper("test-service")
	ctx := context.Background()
	
	called := false
	err := tracer.WithSpan(ctx, "test-operation", func(ctx context.Context, span trace.Span) error {
		called = true
		assert.NotNil(t, ctx)
		assert.NotNil(t, span)
		return nil
	})
	
	assert.NoError(t, err)
	assert.True(t, called)
}

func TestTracerWrapper_WithSpanError(t *testing.T) {
	otel.SetTracerProvider(tracenoop.NewTracerProvider())
	
	tracer := NewTracerWrapper("test-service")
	
	testErr := assert.AnError
	err := tracer.WithSpan(context.Background(), "test-operation", func(ctx context.Context, span trace.Span) error {
		return testErr
	})
	
	assert.Equal(t, testErr, err)
}

func TestGetClientIP(t *testing.T) {
	tests := []struct {
		name           string
		headers        map[string]string
		remoteAddr     string
		expectedIP     string
	}{
		{
			name:       "X-Forwarded-For header",
			headers:    map[string]string{"X-Forwarded-For": "192.168.1.100"},
			remoteAddr: "10.0.0.1:12345",
			expectedIP: "192.168.1.100",
		},
		{
			name:       "X-Real-IP header",
			headers:    map[string]string{"X-Real-IP": "172.16.0.50"},
			remoteAddr: "10.0.0.1:12345",
			expectedIP: "172.16.0.50",
		},
		{
			name:       "X-Client-IP header",
			headers:    map[string]string{"X-Client-IP": "203.0.113.1"},
			remoteAddr: "10.0.0.1:12345",
			expectedIP: "203.0.113.1",
		},
		{
			name:       "RemoteAddr fallback",
			headers:    map[string]string{},
			remoteAddr: "10.0.0.1:12345",
			expectedIP: "10.0.0.1:12345",
		},
		{
			name: "X-Forwarded-For precedence",
			headers: map[string]string{
				"X-Forwarded-For": "192.168.1.100",
				"X-Real-IP":       "172.16.0.50",
				"X-Client-IP":     "203.0.113.1",
			},
			remoteAddr: "10.0.0.1:12345",
			expectedIP: "192.168.1.100",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tt.remoteAddr
			
			for key, value := range tt.headers {
				req.Header.Set(key, value)
			}
			
			ip := getClientIP(req)
			assert.Equal(t, tt.expectedIP, ip)
		})
	}
}