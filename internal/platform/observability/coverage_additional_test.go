package observability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric/noop"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestObservabilityContextAndTracer_AdditionalCoverageBranches(t *testing.T) {
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})
	otel.SetTracerProvider(tp)

	ctx, span := otel.Tracer("obs-test").Start(context.Background(), "with-valid-span")
	t.Cleanup(func() {
		span.End()
	})

	ctxWithTrace := AddTraceToContext(ctx)
	traceInfo := GetTraceInfoForLogger(ctxWithTrace)
	if traceInfo.TraceID == "" || traceInfo.SpanID == "" {
		t.Fatalf("expected trace info from valid span, got %+v", traceInfo)
	}

	// Cover fallback extraction path when explicit TraceInfoContextKey is absent.
	fallback := GetTraceInfoForLogger(ctx)
	if fallback.TraceID == "" || fallback.SpanID == "" {
		t.Fatalf("expected fallback trace extraction from span context, got %+v", fallback)
	}

	wrapper := NewTracerWrapper("obs-test")
	traceID := wrapper.TraceID(ctx)
	spanID := wrapper.SpanID(ctx)
	header := wrapper.TraceHeader(ctx)
	if traceID == "" || spanID == "" || header == "" {
		t.Fatalf("expected non-empty trace identifiers and header")
	}
	if !strings.Contains(header, traceID) || !strings.Contains(header, spanID) {
		t.Fatalf("expected trace header to include trace and span IDs, got %q", header)
	}
}

func TestObservabilityStandards_AdditionalCoverageBranches(t *testing.T) {
	if got := HTTPRouteLabel(" ?query=1", ""); got != "/" {
		t.Fatalf("expected root when route candidate becomes empty after query trim, got %q", got)
	}
	if got := HTTPRouteLabel("api/v1/status", ""); got != "/api/v1/status" {
		t.Fatalf("expected missing leading slash to be normalized, got %q", got)
	}
	if got := HTTPRouteLabel("", "/api/v1/"); got != "/api/v1" {
		t.Fatalf("expected trailing slash to be removed, got %q", got)
	}

	longName := strings.Repeat("x", 70)
	if got := NormalizeDBResource(longName); len(got) != 64 {
		t.Fatalf("expected normalized DB resource length 64, got %d", len(got))
	}

	if got := sanitizePathSegment(":identity"); got != ":identity" {
		t.Fatalf("expected colon-prefixed segment to be preserved, got %q", got)
	}
	if got := sanitizePathSegment(strings.Repeat("a", 65)); got != "{id}" {
		t.Fatalf("expected >64 path segment to normalize as {id}, got %q", got)
	}
	if got := sanitizePathSegment("0123456789abcdef"); got != "{id}" {
		t.Fatalf("expected hex segment to normalize as {id}, got %q", got)
	}
}

func TestHTTPMiddleware_AdditionalCoverageBranches(t *testing.T) {
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample())))
	otel.SetMeterProvider(noop.NewMeterProvider())

	tracer := NewTracerWrapper("middleware-extra")
	meter, err := NewMeterWrapper("middleware-extra")
	if err != nil {
		t.Fatalf("failed to create meter wrapper: %v", err)
	}
	obsMiddleware := NewHTTPMiddleware(tracer, meter)

	var requestIDFromContext string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestIDFromContext = GetRequestID(r.Context())
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("created"))
	})

	router := chi.NewRouter()
	router.Use(chimiddleware.RequestID)
	router.Use(obsMiddleware.Handler)
	router.Post("/upload", handler)

	body := strings.NewReader("payload-body")
	req := httptest.NewRequest(http.MethodPost, "/upload", body)
	req.ContentLength = int64(len("payload-body"))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, rec.Code)
	}
	if requestIDFromContext == "" {
		t.Fatalf("expected request ID to be propagated through observability middleware")
	}
}
