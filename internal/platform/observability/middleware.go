package observability

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/otel/attribute"
)

// HTTPMiddleware provides observability middleware for HTTP requests
type HTTPMiddleware struct {
	tracer *TracerWrapper
	meter  *MeterWrapper
}

// NewHTTPMiddleware creates a new HTTP middleware for observability
func NewHTTPMiddleware(tracer *TracerWrapper, meter *MeterWrapper) *HTTPMiddleware {
	return &HTTPMiddleware{
		tracer: tracer,
		meter:  meter,
	}
}

// Handler returns an HTTP middleware that adds observability to requests
func (m *HTTPMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Start tracing span for the request
		ctx, span := m.tracer.StartHTTPSpan(r.Context(), r)
		defer span.End()

		// Create a response writer wrapper to capture response data
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		// Extract and set request ID if available
		if requestID := middleware.GetReqID(ctx); requestID != "" {
			m.tracer.SetRequestID(span, requestID)
		}

		// Extract session ID from context if available (from session middleware)
		if sessionID := getSessionIDFromContext(ctx); sessionID != "" {
			m.tracer.SetSessionID(span, sessionID)
		}

		// Extract user ID from context if available (from auth middleware)
		if userID := getUserIDFromContext(ctx); userID != "" {
			m.tracer.SetUserID(span, userID)
		}

		// Store trace and span IDs in context for logger
		ctx = WithTraceInfo(ctx, TraceInfo{
			TraceID: m.tracer.TraceID(ctx),
			SpanID:  m.tracer.SpanID(ctx),
		})

		// Store request ID in context if not already present
		if requestID := middleware.GetReqID(ctx); requestID != "" {
			ctx = WithRequestID(ctx, requestID)
		}

		// Update request context
		r = r.WithContext(ctx)

		// Capture request size
		var requestSize int64
		if r.ContentLength > 0 {
			requestSize = r.ContentLength
		}

		// Add request attributes to span
		span.SetAttributes(
			attribute.String("http.route", getRoutePattern(r)),
			attribute.Int64("http.request.size", requestSize),
		)

		// Process the request
		next.ServeHTTP(ww, r)

		// Calculate duration and get response info
		duration := time.Since(start)
		statusCode := ww.Status()
		responseSize := int64(ww.BytesWritten())

		// Finish the span with response information
		m.tracer.FinishHTTPSpan(span, statusCode, responseSize)

		// Record metrics
		path := getRoutePattern(r)
		if path == "" {
			path = r.URL.Path
		}

		m.meter.RecordHTTPRequest(
			ctx,
			r.Method,
			path,
			statusCode,
			duration,
			requestSize,
			responseSize,
		)

		// Record error metrics if status code indicates error
		if statusCode >= 400 {
			errorType := "client_error"
			if statusCode >= 500 {
				errorType = "server_error"
			}
			m.meter.RecordError(ctx, "http", errorType)
		}
	})
}

// TraceInfo contains trace information for logging
type TraceInfo struct {
	TraceID string
	SpanID  string
}

// Context keys
type contextKey string

const (
	traceInfoKey contextKey = "trace_info"
	requestIDKey contextKey = "request_id"
	sessionIDKey contextKey = "session_id"
	userIDKey    contextKey = "user_id"
)

// WithTraceInfo adds trace information to context
func WithTraceInfo(ctx context.Context, info TraceInfo) context.Context {
	return context.WithValue(ctx, traceInfoKey, info)
}

// GetTraceInfo extracts trace information from context
func GetTraceInfo(ctx context.Context) TraceInfo {
	if info, ok := ctx.Value(traceInfoKey).(TraceInfo); ok {
		return info
	}
	return TraceInfo{}
}

// WithRequestID adds request ID to context
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey, requestID)
}

// GetRequestID extracts request ID from context
func GetRequestID(ctx context.Context) string {
	if requestID, ok := ctx.Value(requestIDKey).(string); ok {
		return requestID
	}
	return ""
}

// WithSessionID adds session ID to context
func WithSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, sessionIDKey, sessionID)
}

// GetSessionID extracts session ID from context
func GetSessionID(ctx context.Context) string {
	if sessionID, ok := ctx.Value(sessionIDKey).(string); ok {
		return sessionID
	}
	return ""
}

// WithUserID adds user ID to context
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
}

// GetUserID extracts user ID from context
func GetUserID(ctx context.Context) string {
	if userID, ok := ctx.Value(userIDKey).(string); ok {
		return userID
	}
	return ""
}

// Helper functions to extract IDs from context (these would be implemented based on your session/auth middleware)

func getSessionIDFromContext(ctx context.Context) string {
	// This should be implemented to extract session ID from your session middleware
	// For example, if you store session info in context:
	// if session, ok := ctx.Value("session").(*Session); ok {
	//     return session.ID
	// }
	return ""
}

func getUserIDFromContext(ctx context.Context) string {
	// This should be implemented to extract user ID from your auth middleware
	// For example, if you store user info in context:
	// if user, ok := ctx.Value("user").(*User); ok {
	//     return user.ID
	// }
	return ""
}

func getRoutePattern(r *http.Request) string {
	// Try to get the route pattern from chi router context
	if routeCtx := chi.RouteContext(r.Context()); routeCtx != nil {
		return routeCtx.RoutePattern()
	}
	return ""
}

// DatabaseMiddleware wraps database operations with observability
type DatabaseMiddleware struct {
	tracer *TracerWrapper
	meter  *MeterWrapper
}

// NewDatabaseMiddleware creates a new database middleware for observability
func NewDatabaseMiddleware(tracer *TracerWrapper, meter *MeterWrapper) *DatabaseMiddleware {
	return &DatabaseMiddleware{
		tracer: tracer,
		meter:  meter,
	}
}

// WrapQuery wraps a database query with observability
func (d *DatabaseMiddleware) WrapQuery(ctx context.Context, operation, table string, fn func(context.Context) error) error {
	start := time.Now()

	// Start database span
	ctx, span := d.tracer.StartDatabaseSpan(ctx, operation, table)
	defer span.End()

	// Execute the query
	err := fn(ctx)
	duration := time.Since(start)

	// Record metrics
	success := err == nil
	d.meter.RecordDatabaseQuery(ctx, operation, table, duration, success)

	// Record error in span if query failed
	if err != nil {
		d.tracer.RecordError(span, err, "database query failed")
	}

	return err
}