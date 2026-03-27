package observability

import (
	"context"

	"go.opentelemetry.io/otel/trace"
)

// ContextKeysForLogger provides context keys that the logger package can use
// to extract trace information. This avoids circular imports.

// TraceInfoForLogger represents trace information for the logger
type TraceInfoForLogger struct {
	TraceID string `json:"trace_id"`
	SpanID  string `json:"span_id"`
}

// Context key constants that the logger package can reference
const (
	TraceInfoContextKey = "trace_info"
	RequestIDContextKey = "request_id"
)

// AddTraceToContext adds trace information to context for logger consumption
func AddTraceToContext(ctx context.Context) context.Context {
	span := trace.SpanFromContext(ctx)
	if span == nil || !span.SpanContext().IsValid() {
		return ctx
	}

	traceInfo := TraceInfoForLogger{
		TraceID: span.SpanContext().TraceID().String(),
		SpanID:  span.SpanContext().SpanID().String(),
	}

	return context.WithValue(ctx, TraceInfoContextKey, traceInfo)
}

// GetTraceInfoForLogger extracts trace information in a format the logger expects
func GetTraceInfoForLogger(ctx context.Context) TraceInfoForLogger {
	if info, ok := ctx.Value(TraceInfoContextKey).(TraceInfoForLogger); ok {
		return info
	}
	
	// Fallback: extract from OpenTelemetry span directly
	span := trace.SpanFromContext(ctx)
	if span != nil && span.SpanContext().IsValid() {
		return TraceInfoForLogger{
			TraceID: span.SpanContext().TraceID().String(),
			SpanID:  span.SpanContext().SpanID().String(),
		}
	}
	
	return TraceInfoForLogger{}
}

// WithRequestIDForLogger adds request ID to context for logger consumption
func WithRequestIDForLogger(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, RequestIDContextKey, requestID)
}

// GetRequestIDForLogger extracts request ID from context
func GetRequestIDForLogger(ctx context.Context) string {
	if requestID, ok := ctx.Value(RequestIDContextKey).(string); ok {
		return requestID
	}
	return ""
}