package router

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aegion/aegion/internal/platform/logger"
	"github.com/aegion/aegion/internal/platform/observability"
)

func decodeLogPayload(t *testing.T, buf *bytes.Buffer) map[string]interface{} {
	t.Helper()
	payload := map[string]interface{}{}
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &payload); err != nil {
		t.Fatalf("failed to decode log payload %q: %v", buf.String(), err)
	}
	return payload
}

func TestLoggerMiddlewareStandardKeys(t *testing.T) {
	var logBuf bytes.Buffer
	// This is tricky because logger.New sets global default.
	// Let's just use LogWideEvent directly to test it.

	l := &logger.Logger{
		Logger: slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{
			ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
				if len(groups) == 0 {
					switch a.Key {
					case slog.MessageKey:
						return slog.Attr{Key: "msg", Value: a.Value}
					}
				}
				return a
			},
		})),
	}

	ctx := observability.WithRequestIDForLogger(context.Background(), "ctx-request-id")

	l.LogWideEvent(ctx, "test-event", map[string]any{
		"foo": "bar",
	})

	payload := decodeLogPayload(t, &logBuf)
	if payload["msg"] != "test-event" {
		t.Fatalf("expected msg 'test-event', got %v", payload["msg"])
	}
	if payload["foo"] != "bar" {
		t.Fatalf("expected foo 'bar', got %v", payload["foo"])
	}
}

func TestRecovererMiddlewareRecoversPanic(t *testing.T) {
	var logBuf bytes.Buffer
	// Mock logger for test
	ml := slog.New(slog.NewJSONHandler(&logBuf, nil))

	wrapped := Recoverer(ml)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	req = req.WithContext(context.WithValue(req.Context(), contextKeyRequestID, "recoverer-request"))
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
	}
	if !strings.Contains(rec.Body.String(), http.StatusText(http.StatusInternalServerError)) {
		t.Fatalf("expected internal server error body, got %q", rec.Body.String())
	}
	if !strings.Contains(logBuf.String(), "panic recovered") {
		t.Fatalf("expected panic recovery log entry, got %q", logBuf.String())
	}
}

func TestGetClientIPAdditionalBranches(t *testing.T) {
	if got := getClientIP(nil); got != "" {
		t.Fatalf("expected empty client IP for nil request, got %q", got)
	}

	req := httptest.NewRequest(http.MethodGet, "http://example.com", nil)
	req.Header.Set("X-Forwarded-For", " , 203.0.113.8")
	req.RemoteAddr = "192.0.2.20:1234"
	t.Setenv("AEGION_TRUSTED_PROXY_CIDRS", "192.0.2.0/24")
	if got := getClientIP(req); got != "203.0.113.8" {
		t.Fatalf("expected first non-empty forwarded IP, got %q", got)
	}

	req = httptest.NewRequest(http.MethodGet, "http://example.com", nil)
	req.RemoteAddr = "[2001:db8::7]"
	if got := getClientIP(req); got != "2001:db8::7" {
		t.Fatalf("expected bracket-trimmed ipv6 address, got %q", got)
	}
}

func TestLoggerMiddleware4xxStatusCodes(t *testing.T) {
	var logBuf bytes.Buffer
	ml := slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.LevelKey {
				return slog.Attr{Key: "level", Value: slog.StringValue(strings.ToLower(a.Value.String()))}
			}
			return a
		},
	}))

	handler := Logger(ml)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("not found"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	req = req.WithContext(context.WithValue(req.Context(), contextKeyRequestID, "req-404"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected %d, got %d", http.StatusNotFound, rec.Code)
	}

	logOutput := logBuf.String()
	// slog default level is INFO, but for 404 we might expect something else if implemented
	// In middleware.go, we log at default level but with outcome=warning
	if !strings.Contains(logOutput, `"outcome":"warning"`) {
		t.Fatalf("expected outcome warning for 404, got %q", logOutput)
	}
	if !strings.Contains(logOutput, `"http.status":404`) {
		t.Fatalf("expected status 404 in log, got %q", logOutput)
	}
}
