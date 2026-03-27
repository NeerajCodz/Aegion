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