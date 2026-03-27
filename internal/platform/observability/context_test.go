package observability

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

func TestTraceInfoForLogger(t *testing.T) {
	traceInfo := TraceInfoForLogger{
		TraceID: "trace123",
		SpanID:  "span456",
	}
	
	assert.Equal(t, "trace123", traceInfo.TraceID)
	assert.Equal(t, "span456", traceInfo.SpanID)
}

func TestAddTraceToContext(t *testing.T) {
	otel.SetTracerProvider(tracenoop.NewTracerProvider())
	
	ctx := context.Background()
	
	// With no-op tracer, span context won't be valid
	// so AddTraceToContext should return the original context
	newCtx := AddTraceToContext(ctx)
	assert.NotNil(t, newCtx)
	
	// Extract trace info - should be empty with no-op tracer
	traceInfo := GetTraceInfoForLogger(newCtx)
	assert.Empty(t, traceInfo.TraceID)
	assert.Empty(t, traceInfo.SpanID)
}

func TestGetTraceInfoForLogger(t *testing.T) {
	ctx := context.Background()
	
	// Test with explicit trace info in context
	traceInfo := TraceInfoForLogger{
		TraceID: "explicit-trace-123",
		SpanID:  "explicit-span-456",
	}
	
	ctx = context.WithValue(ctx, TraceInfoContextKey, traceInfo)
	
	extracted := GetTraceInfoForLogger(ctx)
	assert.Equal(t, traceInfo.TraceID, extracted.TraceID)
	assert.Equal(t, traceInfo.SpanID, extracted.SpanID)
}

func TestGetTraceInfoForLogger_Empty(t *testing.T) {
	ctx := context.Background()
	
	// Test with empty context
	traceInfo := GetTraceInfoForLogger(ctx)
	assert.Empty(t, traceInfo.TraceID)
	assert.Empty(t, traceInfo.SpanID)
}

func TestWithRequestIDForLogger(t *testing.T) {
	ctx := context.Background()
	requestID := "test-request-123"
	
	newCtx := WithRequestIDForLogger(ctx, requestID)
	assert.NotNil(t, newCtx)
	
	extracted := GetRequestIDForLogger(newCtx)
	assert.Equal(t, requestID, extracted)
}

func TestGetRequestIDForLogger_Empty(t *testing.T) {
	ctx := context.Background()
	
	requestID := GetRequestIDForLogger(ctx)
	assert.Empty(t, requestID)
}

func TestContextKeys(t *testing.T) {
	// Ensure context keys are string constants
	assert.Equal(t, "trace_info", TraceInfoContextKey)
	assert.Equal(t, "request_id", RequestIDContextKey)
}

func TestContextKeysAreConsistent(t *testing.T) {
	ctx := context.Background()
	
	// Test that the context keys work consistently
	traceInfo := TraceInfoForLogger{
		TraceID: "consistency-trace",
		SpanID:  "consistency-span",
	}
	requestID := "consistency-request"
	
	// Add values using the utility functions
	ctx = context.WithValue(ctx, TraceInfoContextKey, traceInfo)
	ctx = WithRequestIDForLogger(ctx, requestID)
	
	// Extract values using utility functions
	extractedTrace := GetTraceInfoForLogger(ctx)
	extractedRequest := GetRequestIDForLogger(ctx)
	
	assert.Equal(t, traceInfo, extractedTrace)
	assert.Equal(t, requestID, extractedRequest)
	
	// Verify the raw context values are accessible with the constants
	rawTrace := ctx.Value(TraceInfoContextKey)
	rawRequest := ctx.Value(RequestIDContextKey)
	
	assert.Equal(t, traceInfo, rawTrace)
	assert.Equal(t, requestID, rawRequest)
}