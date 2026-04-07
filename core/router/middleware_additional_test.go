package router

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"

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

func TestWithCorrelationFieldsIncludesRequestAndTraceIDs(t *testing.T) {
	var logBuf bytes.Buffer
	log := zerolog.New(&logBuf)

	ctx := observability.WithRequestIDForLogger(context.Background(), "ctx-request-id")
	ctx = context.WithValue(ctx, observability.TraceInfoContextKey, observability.TraceInfoForLogger{
		TraceID: "trace-id-123",
		SpanID:  "span-id-456",
	})

	withCorrelationFields(log.Info(), ctx, "").Msg("from-context")
	payload := decodeLogPayload(t, &logBuf)
	if payload["request_id"] != "ctx-request-id" {
		t.Fatalf("expected context request_id, got %v", payload["request_id"])
	}
	if payload["trace_id"] != "trace-id-123" {
		t.Fatalf("expected trace_id from context, got %v", payload["trace_id"])
	}
	if payload["span_id"] != "span-id-456" {
		t.Fatalf("expected span_id from context, got %v", payload["span_id"])
	}

	logBuf.Reset()
	withCorrelationFields(log.Info(), ctx, "explicit-request-id").Msg("explicit")
	payload = decodeLogPayload(t, &logBuf)
	if payload["request_id"] != "explicit-request-id" {
		t.Fatalf("expected explicit request_id to win, got %v", payload["request_id"])
	}
}

func TestRecovererMiddlewareRecoversPanic(t *testing.T) {
	var logBuf bytes.Buffer
	wrapped := Recoverer(zerolog.New(&logBuf))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
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
	if !strings.Contains(logBuf.String(), "boom") {
		t.Fatalf("expected panic value in log output, got %q", logBuf.String())
	}
}

func TestGetClientIPAdditionalBranches(t *testing.T) {
	if got := getClientIP(nil); got != "" {
		t.Fatalf("expected empty client IP for nil request, got %q", got)
	}

	req := httptest.NewRequest(http.MethodGet, "http://example.com", nil)
	req.Header.Set("X-Forwarded-For", " , 203.0.113.8")
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
	logger := zerolog.New(&logBuf)

	handler := Logger(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	if !strings.Contains(logOutput, `"level":"warn"`) {
		t.Fatalf("expected warn level for 404, got %q", logOutput)
	}
	if !strings.Contains(logOutput, `"status":404`) {
		t.Fatalf("expected status 404 in log, got %q", logOutput)
	}
}

func TestCORSMiddlewareRejectsDisallowedOrigin(t *testing.T) {
	cfg := CORSConfig{
		AllowedOrigins: []string{"https://example.com"},
		AllowedMethods: []string{"GET", "POST"},
		AllowedHeaders: []string{"Content-Type"},
	}

	handler := CORS(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/resource", nil)
	req.Header.Set("Origin", "https://evil.com")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("expected disallowed origin to have no CORS headers, got %q", rec.Header().Get("Access-Control-Allow-Origin"))
	}
}
