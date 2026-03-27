package observability

import (
	"context"
	"fmt"
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// TracerWrapper wraps OpenTelemetry tracer with helper methods
type TracerWrapper struct {
	tracer      trace.Tracer
	serviceName string
}

// NewTracerWrapper creates a new tracer wrapper
func NewTracerWrapper(serviceName string) *TracerWrapper {
	return &TracerWrapper{
		tracer:      otel.Tracer(serviceName),
		serviceName: serviceName,
	}
}

// StartSpan starts a new span with the given name and options
func (t *TracerWrapper) StartSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return t.tracer.Start(ctx, name, opts...)
}

// StartHTTPSpan starts a span for an HTTP request
func (t *TracerWrapper) StartHTTPSpan(ctx context.Context, r *http.Request) (context.Context, trace.Span) {
	spanName := fmt.Sprintf("%s %s", r.Method, r.URL.Path)
	
	opts := []trace.SpanStartOption{
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(
			attribute.String("http.method", r.Method),
			attribute.String("http.url", r.URL.String()),
			attribute.String("http.scheme", r.URL.Scheme),
			attribute.String("http.target", r.URL.Path),
			attribute.String("http.user_agent", r.UserAgent()),
			attribute.String("http.client_ip", getClientIP(r)),
		),
	}

	// Add query parameters as attributes (be careful with sensitive data)
	if r.URL.RawQuery != "" {
		opts = append(opts, trace.WithAttributes(
			attribute.String("http.url", r.URL.RequestURI()),
		))
	}

	// Add host information
	if r.Host != "" {
		opts = append(opts, trace.WithAttributes(
			attribute.String("http.host", r.Host),
		))
	}

	return t.tracer.Start(ctx, spanName, opts...)
}

// StartDatabaseSpan starts a span for a database operation
func (t *TracerWrapper) StartDatabaseSpan(ctx context.Context, operation, table string) (context.Context, trace.Span) {
	spanName := fmt.Sprintf("db.%s %s", operation, table)
	
	opts := []trace.SpanStartOption{
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.operation", operation),
			attribute.String("db.collection.name", table),
			attribute.String("db.system", "postgresql"),
		),
	}

	return t.tracer.Start(ctx, spanName, opts...)
}

// StartServiceSpan starts a span for a service operation
func (t *TracerWrapper) StartServiceSpan(ctx context.Context, service, operation string) (context.Context, trace.Span) {
	spanName := fmt.Sprintf("%s.%s", service, operation)
	
	opts := []trace.SpanStartOption{
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("service.name", service),
			attribute.String("service.operation", operation),
		),
	}

	return t.tracer.Start(ctx, spanName, opts...)
}

// StartModuleSpan starts a span for a module operation
func (t *TracerWrapper) StartModuleSpan(ctx context.Context, moduleID, operation string) (context.Context, trace.Span) {
	spanName := fmt.Sprintf("module.%s.%s", moduleID, operation)
	
	opts := []trace.SpanStartOption{
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("aegion.module.id", moduleID),
			attribute.String("aegion.module.operation", operation),
		),
	}

	return t.tracer.Start(ctx, spanName, opts...)
}

// FinishHTTPSpan finishes an HTTP span with response information
func (t *TracerWrapper) FinishHTTPSpan(span trace.Span, statusCode int, responseSize int64) {
	span.SetAttributes(
		attribute.Int("http.status_code", statusCode),
		attribute.Int64("http.response.body.size", responseSize),
	)

	// Set span status based on HTTP status code
	switch {
	case statusCode >= 400 && statusCode < 500:
		span.SetStatus(codes.Error, http.StatusText(statusCode))
	case statusCode >= 500:
		span.SetStatus(codes.Error, http.StatusText(statusCode))
	default:
		span.SetStatus(codes.Ok, "")
	}

	span.End()
}

// RecordError records an error in the span
func (t *TracerWrapper) RecordError(span trace.Span, err error, description string) {
	if err != nil {
		span.RecordError(err, trace.WithAttributes(
			attribute.String("error.description", description),
		))
		span.SetStatus(codes.Error, description)
	}
}

// AddEvent adds an event to the span
func (t *TracerWrapper) AddEvent(span trace.Span, name string, attrs ...attribute.KeyValue) {
	span.AddEvent(name, trace.WithAttributes(attrs...))
}

// SetUserID sets user ID attribute on the span
func (t *TracerWrapper) SetUserID(span trace.Span, userID string) {
	span.SetAttributes(attribute.String("user.id", userID))
}

// SetSessionID sets session ID attribute on the span
func (t *TracerWrapper) SetSessionID(span trace.Span, sessionID string) {
	span.SetAttributes(attribute.String("aegion.session.id", sessionID))
}

// SetRequestID sets request ID attribute on the span
func (t *TracerWrapper) SetRequestID(span trace.Span, requestID string) {
	span.SetAttributes(attribute.String("aegion.request.id", requestID))
}

// TraceID extracts the trace ID from the context
func (t *TracerWrapper) TraceID(ctx context.Context) string {
	span := trace.SpanFromContext(ctx)
	if span != nil && span.SpanContext().IsValid() {
		return span.SpanContext().TraceID().String()
	}
	return ""
}

// SpanID extracts the span ID from the context
func (t *TracerWrapper) SpanID(ctx context.Context) string {
	span := trace.SpanFromContext(ctx)
	if span != nil && span.SpanContext().IsValid() {
		return span.SpanContext().SpanID().String()
	}
	return ""
}

// TraceHeader returns the trace header for propagation
func (t *TracerWrapper) TraceHeader(ctx context.Context) string {
	span := trace.SpanFromContext(ctx)
	if span != nil && span.SpanContext().IsValid() {
		sc := span.SpanContext()
		return fmt.Sprintf("00-%s-%s-%02x",
			sc.TraceID().String(),
			sc.SpanID().String(),
			sc.TraceFlags(),
		)
	}
	return ""
}

// WithSpan executes a function within a span
func (t *TracerWrapper) WithSpan(ctx context.Context, name string, fn func(context.Context, trace.Span) error, opts ...trace.SpanStartOption) error {
	ctx, span := t.tracer.Start(ctx, name, opts...)
	defer span.End()

	if err := fn(ctx, span); err != nil {
		t.RecordError(span, err, "operation failed")
		return err
	}

	return nil
}

// getClientIP extracts the client IP address from the request
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first (most common proxy header)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}

	// Check X-Real-IP header (nginx)
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Check X-Client-IP header
	if xci := r.Header.Get("X-Client-IP"); xci != "" {
		return xci
	}

	// Fall back to RemoteAddr
	return r.RemoteAddr
}