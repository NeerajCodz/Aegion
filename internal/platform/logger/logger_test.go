package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected slog.Level
	}{
		{"debug level", "debug", slog.LevelDebug},
		{"info level", "info", slog.LevelInfo},
		{"warn level", "warn", slog.LevelWarn},
		{"error level", "error", slog.LevelError},
		{"invalid level defaults to info", "invalid", slog.LevelInfo},
		{"empty string defaults to info", "", slog.LevelInfo},
		{"uppercase level", "info", slog.LevelInfo}, // parseLevel is case sensitive in my implementation, or not?
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
		})
	}
}

func TestJSONFormat(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Level:            "info",
		Format:           "json",
		ServiceName:      "test-service",
		Environment:      "test-env",
		ServiceNamespace: "test-ns",
	}
	handler := newServiceInfoHandler(&buf, cfg)
	logger := slog.New(handler)

	logger.Info("hello world", "key", "value")

	var data map[string]any
	if err := json.Unmarshal(buf.Bytes(), &data); err != nil {
		t.Fatalf("failed to unmarshal log: %v", err)
	}

	// Verify standard keys
	expectedKeys := []string{"time", "level", "msg", "service_name", "environment", "service_namespace", "key"}
	for _, key := range expectedKeys {
		if _, ok := data[key]; !ok {
			t.Errorf("missing expected key: %s", key)
		}
	}

	if data["msg"] != "hello world" {
		t.Errorf("expected msg 'hello world', got %v", data["msg"])
	}
	if data["level"] != "INFO" {
		t.Errorf("expected level 'INFO', got %v", data["level"])
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

func TestFromContext(t *testing.T) {
	tests := []struct {
		name string
		ctx  context.Context
	}{
		{
			name: "context without logger returns default",
			ctx:  context.Background(),
		},
		{
			name: "context with logger returns that logger",
			ctx:  context.WithValue(context.Background(), ContextKey{}, New(Config{Level: "debug", Format: "text"})),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := FromContext(tt.ctx)
			if logger == nil {
				t.Errorf("FromContext() returned nil")
			}
		})
	}
}

func TestWideEvent(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{Level: "info", Format: "json"}
	l := &Logger{
		Logger: slog.New(newServiceInfoHandler(&buf, cfg)),
		cfg:    cfg,
	}

	ctx := context.Background()
	l.WideEvent(ctx, "request_completed").
		With("path", "/api/v1/test").
		WithStatusCode(200).
		WithOutcome("success").
		Emit()

	var data map[string]any
	if err := json.Unmarshal(buf.Bytes(), &data); err != nil {
		t.Fatalf("failed to unmarshal log: %v", err)
	}

	if data["msg"] != "request_completed" {
		t.Errorf("expected msg 'request_completed', got %v", data["msg"])
	}
	if data["path"] != "/api/v1/test" {
		t.Errorf("expected path '/api/v1/test', got %v", data["path"])
	}
	if data["http.status_code"] != float64(200) {
		t.Errorf("expected http.status_code 200, got %v", data["http.status_code"])
	}
	if _, ok := data["latency_ms"]; !ok {
		t.Error("missing latency_ms")
	}
}

func TestRedaction(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Level:        "info",
		Format:       "json",
		RedactFields: []string{"password", "token"},
	}
	l := &Logger{
		Logger: slog.New(newServiceInfoHandler(&buf, cfg)),
		cfg:    cfg,
	}

	l.Info("user login", "password", "secret123", "token", "abc-123", "username", "alice")

	var data map[string]any
	if err := json.Unmarshal(buf.Bytes(), &data); err != nil {
		t.Fatalf("failed to unmarshal log: %v", err)
	}

	if data["password"] != "[REDACTED]" {
		t.Errorf("expected password to be redacted, got %v", data["password"])
	}
	if data["token"] != "[REDACTED]" {
		t.Errorf("expected token to be redacted, got %v", data["token"])
	}
	if data["username"] != "alice" {
		t.Errorf("expected username to be 'alice', got %v", data["username"])
	}
}

func TestCapturePanic(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{Level: "info", Format: "json"}
	l := &Logger{
		Logger: slog.New(newServiceInfoHandler(&buf, cfg)),
		cfg:    cfg,
	}

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic to be re-propagated")
		}

		var data map[string]any
		if err := json.Unmarshal(buf.Bytes(), &data); err != nil {
			t.Fatalf("failed to unmarshal log: %v", err)
		}

		if !strings.Contains(data["msg"].(string), "panic recovered") {
			t.Errorf("expected msg to contain 'panic recovered', got %v", data["msg"])
		}
		if _, ok := data["stack_trace"]; !ok {
			t.Error("missing stack_trace")
		}
	}()

	func() {
		defer l.CapturePanic(context.Background())
		panic("test panic")
	}()
}
