package logger

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected zerolog.Level
	}{
		{"debug level", "debug", zerolog.DebugLevel},
		{"info level", "info", zerolog.InfoLevel},
		{"warn level", "warn", zerolog.WarnLevel},
		{"error level", "error", zerolog.ErrorLevel},
		{"invalid level defaults to info", "invalid", zerolog.InfoLevel},
		{"empty string defaults to info", "", zerolog.InfoLevel},
		{"uppercase level (case sensitive)", "INFO", zerolog.InfoLevel},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseLevel(tt.input)
			if result != tt.expected {
				t.Errorf("parseLevel(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestNew(t *testing.T) {
	tests := []struct {
		name   string
		config Config
	}{
		{"json format logger", Config{Level: "info", Format: "json"}},
		{"text format logger", Config{Level: "debug", Format: "text"}},
		{"default format logger", Config{Level: "error", Format: ""}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := New(tt.config)
			if logger == nil {
				t.Error("New() returned nil logger")
			}
			// Note: Cannot directly compare zerolog.Logger due to unexported fields
		})
	}
}

func TestWithComponent(t *testing.T) {
	logger := New(Config{Level: "info", Format: "json"})
	componentLogger := logger.WithComponent("test-component")

	if componentLogger == nil {
		t.Error("WithComponent() returned nil logger")
	}
	
	if componentLogger == logger {
		t.Error("WithComponent() returned same logger instance instead of new one")
	}
}

func TestWithRequestID(t *testing.T) {
	logger := New(Config{Level: "info", Format: "json"})
	requestLogger := logger.WithRequestID("test-request-123")

	if requestLogger == nil {
		t.Error("WithRequestID() returned nil logger")
	}
	
	if requestLogger == logger {
		t.Error("WithRequestID() returned same logger instance instead of new one")
	}
}

func TestFromContext(t *testing.T) {
	tests := []struct {
		name    string
		ctx     context.Context
		wantNil bool
	}{
		{
			name:    "context without logger returns default",
			ctx:     context.Background(),
			wantNil: false, // Should return default logger, not nil
		},
		{
			name:    "context with logger returns that logger",
			ctx:     context.WithValue(context.Background(), ContextKey{}, New(Config{Level: "debug", Format: "text"})),
			wantNil: false,
		},
		{
			name:    "context with non-logger value returns default",
			ctx:     context.WithValue(context.Background(), ContextKey{}, "not a logger"),
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := FromContext(tt.ctx)
			if (logger == nil) != tt.wantNil {
				t.Errorf("FromContext() returned nil=%v, want nil=%v", logger == nil, tt.wantNil)
			}
		})
	}
}

func TestWithContext(t *testing.T) {
	logger := New(Config{Level: "info", Format: "json"})
	ctx := context.Background()
	
	newCtx := logger.WithContext(ctx)
	
	if newCtx == nil {
		t.Error("WithContext() returned nil context")
	}
	
	// Verify the logger can be retrieved from the context
	retrievedLogger := FromContext(newCtx)
	if retrievedLogger != logger {
		t.Error("Logger retrieved from context is not the same as the one added")
	}
}

func TestLoggerMethods(t *testing.T) {
	logger := New(Config{Level: "debug", Format: "json"})

	// Test that all log level methods return non-nil events
	if event := logger.Debug(); event == nil {
		t.Error("Debug() returned nil event")
	}
	
	if event := logger.Info(); event == nil {
		t.Error("Info() returned nil event")
	}
	
	if event := logger.Warn(); event == nil {
		t.Error("Warn() returned nil event")
	}
	
	if event := logger.Error(); event == nil {
		t.Error("Error() returned nil event")
	}
	
	// Test With() method - just test that it doesn't panic
	ctx := logger.With()
	_ = ctx // Use the context to avoid unused variable error
}

func TestContextKey(t *testing.T) {
	// Test that ContextKey is a distinct type
	key1 := ContextKey{}
	key2 := ContextKey{}
	
	// Two instances of ContextKey should be equal (empty structs)
	ctx := context.WithValue(context.Background(), key1, "value1")
	if value := ctx.Value(key2); value != "value1" {
		t.Error("ContextKey instances should be equal")
	}
}

func TestLoggerFatal(t *testing.T) {
	// Fatal() returns an event but actually calling .Msg() would exit
	// So we just test that Fatal() returns a non-nil event
	logger := New(Config{Level: "debug", Format: "json"})
	if event := logger.Fatal(); event == nil {
		t.Error("Fatal() returned nil event")
	}
}

func TestFromContextWithLogger(t *testing.T) {
	logger := New(Config{Level: "debug", Format: "json"})
	ctx := context.WithValue(context.Background(), ContextKey{}, logger)
	
	retrieved := FromContext(ctx)
	if retrieved == nil {
		t.Error("FromContext() returned nil when logger was in context")
	}
}

func TestFromContextWithoutLogger(t *testing.T) {
	ctx := context.Background()
	
	retrieved := FromContext(ctx)
	if retrieved == nil {
		t.Error("FromContext() returned nil when no logger in context")
	}
}

func TestWithContextExtended(t *testing.T) {
	logger := New(Config{Level: "info", Format: "json"})
	ctx := context.Background()
	
	newCtx := logger.WithContext(ctx)
	if newCtx == ctx {
		t.Error("WithContext() should return new context")
	}
	
	// Verify the logger is stored in context
	retrieved := FromContext(newCtx)
	if retrieved == nil {
		t.Error("Logger should be retrievable from context after WithContext()")
	}
}

func TestWithTraceContext(t *testing.T) {
	logger := New(Config{Level: "info", Format: "json"})
	
	// Test with empty context (no trace info)
	ctx := context.Background()
	newLogger := logger.withTraceContext(ctx)
	if newLogger == nil {
		t.Error("withTraceContext() returned nil")
	}
	
	// Test with request ID in context via router middleware key
	type requestIDKey struct{}
	ctxWithRequestID := context.WithValue(ctx, requestIDKey{}, "test-request-123")
	newLogger = logger.withTraceContext(ctxWithRequestID)
	if newLogger == nil {
		t.Error("withTraceContext() with request ID returned nil")
	}
}

func TestGetTraceInfoFromContext(t *testing.T) {
	// Test with empty context
	ctx := context.Background()
	traceInfo := getTraceInfoFromContext(ctx)
	
	// Should return zero values for empty context
	if traceInfo.TraceID != "" || traceInfo.SpanID != "" {
		t.Error("expected empty trace info for empty context")
	}

	// Test with TraceInfo type in context
	ctxWithTraceInfo := context.WithValue(ctx, "trace_info", TraceInfo{
		TraceID: "test-trace-id",
		SpanID:  "test-span-id",
	})
	traceInfo = getTraceInfoFromContext(ctxWithTraceInfo)
	if traceInfo.TraceID != "test-trace-id" {
		t.Errorf("expected TraceID 'test-trace-id', got %q", traceInfo.TraceID)
	}
	if traceInfo.SpanID != "test-span-id" {
		t.Errorf("expected SpanID 'test-span-id', got %q", traceInfo.SpanID)
	}

	// Test with different type that has same fields (fallback case)
	type otherTraceInfo struct {
		TraceID string
		SpanID  string
	}
	ctxWithOtherType := context.WithValue(ctx, "trace_info", otherTraceInfo{
		TraceID: "other-trace-id",
		SpanID:  "other-span-id",
	})
	// This should return empty TraceInfo since it's a different type
	traceInfo = getTraceInfoFromContext(ctxWithOtherType)
	if traceInfo.TraceID != "" {
		t.Errorf("expected empty TraceID for different type, got %q", traceInfo.TraceID)
	}
}

func TestGetRequestIDFromContext(t *testing.T) {
	// Test with empty context
	ctx := context.Background()
	requestID := getRequestIDFromContext(ctx)
	
	if requestID != "" {
		t.Errorf("expected empty request ID for empty context, got %q", requestID)
	}
	
	// Test with request_id key and string value
	ctxWithRequestID := context.WithValue(ctx, "request_id", "my-request-123")
	requestID = getRequestIDFromContext(ctxWithRequestID)
	if requestID != "my-request-123" {
		t.Errorf("expected request ID 'my-request-123', got %q", requestID)
	}

	// Test with wrong type (should return empty)
	ctxWithWrongType := context.WithValue(ctx, "request_id", 12345)
	requestID = getRequestIDFromContext(ctxWithWrongType)
	if requestID != "" {
		t.Errorf("expected empty request ID for wrong type, got %q", requestID)
	}
}