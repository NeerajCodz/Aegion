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

func TestCORSMiddlewareWildcardWithCredentialsRequiresExplicitOrigin(t *testing.T) {
	cfg := CORSConfig{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST"},
		AllowedHeaders:   []string{"Content-Type"},
		AllowCredentials: true,
	}

	nextCalled := false
	handler := CORS(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/resource", nil)
	req.Header.Set("Origin", "https://evil.com")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if !nextCalled {
		t.Fatalf("expected next handler to run")
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("expected no CORS origin header when wildcard is used with credentials")
	}
	if rec.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatalf("expected no credentials header when origin is not explicitly allowed")
	}
}

func TestCORSMiddlewareWildcardWithoutCredentialsAllowsAnyOrigin(t *testing.T) {
	cfg := CORSConfig{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST"},
		AllowedHeaders:   []string{"Content-Type"},
		AllowCredentials: false,
	}

	handler := CORS(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/resource", nil)
	req.Header.Set("Origin", "https://anywhere.example")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("expected wildcard allow-origin header, got %q", rec.Header().Get("Access-Control-Allow-Origin"))
	}
	if rec.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatalf("expected credentials header to be omitted for wildcard origin")
	}
}

func TestGetClientIPWithTrustFalseIgnoresForwardedHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://example.com", nil)
	req.RemoteAddr = "192.0.2.9:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.7")
	req.Header.Set("X-Real-IP", "198.51.100.3")

	if got := getClientIPWithTrust(req, false); got != "192.0.2.9" {
		t.Fatalf("expected remote address when proxy headers are not trusted, got %q", got)
	}
}

func TestRateLimitMiddlewareDefaultDoesNotTrustForwardedHeaders(t *testing.T) {
	handler := RateLimit(RateLimitConfig{
		RequestsPerSecond: 1,
		Burst:             1,
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	req1.RemoteAddr = "192.0.2.1:1111"
	req1.Header.Set("X-Forwarded-For", "198.51.100.10")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.RemoteAddr = "192.0.2.1:2222"
	req2.Header.Set("X-Forwarded-For", "198.51.100.11")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec1.Code != http.StatusOK {
		t.Fatalf("expected first request to pass, got %d", rec1.Code)
	}
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second request to be rate limited by remote addr, got %d", rec2.Code)
	}
}

func TestRateLimitMiddlewareTrustProxyUsesForwardedHeaders(t *testing.T) {
	handler := RateLimitWithTrustProxy(RateLimitConfig{
		RequestsPerSecond: 1,
		Burst:             1,
	}, true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	req1.RemoteAddr = "192.0.2.1:1111"
	req1.Header.Set("X-Forwarded-For", "198.51.100.10")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.RemoteAddr = "192.0.2.1:2222"
	req2.Header.Set("X-Forwarded-For", "198.51.100.11")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec1.Code != http.StatusOK {
		t.Fatalf("expected first request to pass, got %d", rec1.Code)
	}
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected second request with distinct forwarded IP to pass, got %d", rec2.Code)
	}
}
