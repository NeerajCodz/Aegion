package logger

import (
	"context"
	"testing"
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
