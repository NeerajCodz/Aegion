package logger

import (
	"context"
	"testing"

	"github.com/aegion/aegion/internal/platform/observability"
)

func TestWithTraceContext_AddsTraceSpanAndRequestID(t *testing.T) {
	l := New(Config{Level: "info", Format: "json"})

	ctx := context.Background()
	ctx = context.WithValue(ctx, traceInfoContextKey, TraceInfo{
		TraceID: "trace-123",
		SpanID:  "span-456",
	})
	ctx = context.WithValue(ctx, requestIDContextKey, "req-789")

	got := l.withTraceContext(ctx)
	if got == nil {
		t.Fatalf("expected non-nil logger with trace context")
	}
}

func TestGetRequestIDFromContext_ObservabilityContext(t *testing.T) {
	ctx := observability.WithRequestIDForLogger(context.Background(), "obs-req-123")
	got := getRequestIDFromContext(ctx)
	if got != "obs-req-123" {
		t.Fatalf("expected observability request ID, got %q", got)
	}
}

func TestGetTraceInfoFromContext_ObservabilityContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), observability.TraceInfoContextKey, observability.TraceInfoForLogger{
		TraceID: "obs-trace-123",
		SpanID:  "obs-span-456",
	})

	info := getTraceInfoFromContext(ctx)
	if info.TraceID != "obs-trace-123" || info.SpanID != "obs-span-456" {
		t.Fatalf("unexpected trace info: %+v", info)
	}
}
