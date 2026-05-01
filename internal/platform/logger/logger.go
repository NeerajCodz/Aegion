// Package logger provides structured logging for Aegion using slog with wide events pattern.
package logger

import (
	"context"
	"io"
	"log/slog"
	"os"
	"runtime"
	"slices"
	"sync"
	"time"

	"github.com/aegion/aegion/internal/platform/observability"
)

// Config holds logger configuration.
type Config struct {
	Level            string   // debug, info, warn, error
	Format           string   // json, text
	ServiceName      string   // service.name for all logs
	ServiceNamespace string   // service.namespace for all logs
	Environment      string   // deployment.environment
	CloudRegion      string   // cloud.region
	Version          string   // service.version
	CommitHash       string   // git commit hash
	InstanceID       string   // service.instance.id
	RedactFields     []string // fields to redact from logs
}

// serviceInfoHandler wraps slog.Handler and injects service metadata into every log.
type serviceInfoHandler struct {
	handler      slog.Handler
	cfg          Config
	attrs        []slog.Attr
	groups       []string
	mu           sync.RWMutex
	redactMap    map[string]bool
}

func newServiceInfoHandler(w io.Writer, cfg Config) *serviceInfoHandler {
	var handler slog.Handler
	opts := &slog.HandlerOptions{
		Level:     parseLevel(cfg.Level),
		AddSource: true,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Rename "msg" to "message" and "level" to "severity" for consistency
			if len(groups) == 0 {
				switch a.Key {
				case slog.MessageKey:
					return slog.Attr{Key: "msg", Value: a.Value}
				case slog.LevelKey:
					return slog.Attr{Key: "level", Value: slog.StringValue(a.Value.String())}
				case slog.TimeKey:
					return slog.Attr{Key: "time", Value: a.Value}
				}
			}
			return a
		},
	}

	if cfg.Format == "text" {
		handler = slog.NewTextHandler(w, opts)
	} else {
		handler = slog.NewJSONHandler(w, opts)
	}

	// Build base attributes from config
	baseAttrs := []slog.Attr{}
	if cfg.ServiceName != "" {
		baseAttrs = append(baseAttrs, slog.String("service_name", cfg.ServiceName))
	}
	if cfg.ServiceNamespace != "" {
		baseAttrs = append(baseAttrs, slog.String("service_namespace", cfg.ServiceNamespace))
	}
	if cfg.Environment != "" {
		baseAttrs = append(baseAttrs, slog.String("environment", cfg.Environment))
	}
	if cfg.CloudRegion != "" {
		baseAttrs = append(baseAttrs, slog.String("cloud_region", cfg.CloudRegion))
	}
	if cfg.Version != "" {
		baseAttrs = append(baseAttrs, slog.String("service_version", cfg.Version))
	}
	if cfg.CommitHash != "" {
		baseAttrs = append(baseAttrs, slog.String("commit_hash", cfg.CommitHash))
	}
	if cfg.InstanceID != "" {
		baseAttrs = append(baseAttrs, slog.String("instance_id", cfg.InstanceID))
	}

	// Build redact map
	redactMap := make(map[string]bool)
	for _, f := range cfg.RedactFields {
		redactMap[f] = true
	}

	return &serviceInfoHandler{
		handler:   handler,
		cfg:       cfg,
		attrs:     baseAttrs,
		redactMap: redactMap,
	}
}

func (h *serviceInfoHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

func (h *serviceInfoHandler) Handle(ctx context.Context, r slog.Record) error {
	// Extract trace and request context
	traceInfo := observability.GetTraceInfoForLogger(ctx)
	requestID := observability.GetRequestIDForLogger(ctx)

	// Add base service attributes
	for _, attr := range h.attrs {
		r.AddAttrs(attr)
	}

	// Add trace context if available
	if traceInfo.TraceID != "" {
		r.AddAttrs(slog.String("trace_id", traceInfo.TraceID))
	}
	if traceInfo.SpanID != "" {
		r.AddAttrs(slog.String("span_id", traceInfo.SpanID))
	}
	if requestID != "" {
		r.AddAttrs(slog.String("request_id", requestID))
	}

	// Apply redaction
	if len(h.redactMap) > 0 {
		r = h.redactRecord(r)
	}

	return h.handler.Handle(ctx, r)
}

func (h *serviceInfoHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &serviceInfoHandler{
		handler:   h.handler.WithAttrs(attrs),
		cfg:       h.cfg,
		attrs:     append(slices.Clip(h.attrs), attrs...),
		groups:    h.groups,
		redactMap: h.redactMap,
	}
}

func (h *serviceInfoHandler) WithGroup(name string) slog.Handler {
	return &serviceInfoHandler{
		handler:   h.handler.WithGroup(name),
		cfg:       h.cfg,
		attrs:     h.attrs,
		groups:    append(slices.Clip(h.groups), name),
		redactMap: h.redactMap,
	}
}

func (h *serviceInfoHandler) redactRecord(r slog.Record) slog.Record {
	newRecord := slog.Record{
		Time:    r.Time,
		Level:   r.Level,
		Message: r.Message,
		PC:      r.PC,
	}

	r.Attrs(func(a slog.Attr) bool {
		if h.redactMap[a.Key] {
			newRecord.AddAttrs(slog.String(a.Key, "[REDACTED]"))
		} else {
			newRecord.AddAttrs(a)
		}
		return true
	})

	return newRecord
}

// Logger wraps slog for structured logging with wide events support.
type Logger struct {
	sl   *slog.Logger
	cfg  Config
	mu   sync.RWMutex
	ctx  context.Context // base context for trace injection
}

// New creates a new logger with the given configuration.
func New(cfg Config) *Logger {
	handler := newServiceInfoHandler(os.Stdout, cfg)
	sl := slog.New(handler)

	return &Logger{
		sl:  sl,
		cfg: cfg,
		ctx: context.Background(),
	}
}

// parseLevel converts a string level to slog.Level.
func parseLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// DebugContext logs a debug message with context for trace injection.
func (l *Logger) DebugContext(ctx context.Context, msg string, args ...any) {
	l.sl.DebugContext(ctx, msg, args...)
}

// InfoContext logs an info message with context for trace injection.
func (l *Logger) InfoContext(ctx context.Context, msg string, args ...any) {
	l.sl.InfoContext(ctx, msg, args...)
}

// WarnContext logs a warning message with context for trace injection.
func (l *Logger) WarnContext(ctx context.Context, msg string, args ...any) {
	l.sl.WarnContext(ctx, msg, args...)
}

// ErrorContext logs an error message with context for trace injection.
func (l *Logger) ErrorContext(ctx context.Context, msg string, args ...any) {
	l.sl.ErrorContext(ctx, msg, args...)
}

// Debug logs a debug message (without context - use *Context methods instead).
func (l *Logger) Debug(msg string, args ...any) {
	l.sl.Debug(msg, args...)
}

// Info logs an info message (without context - use *Context methods instead).
func (l *Logger) Info(msg string, args ...any) {
	l.sl.Info(msg, args...)
}

// Warn logs a warning message (without context - use *Context methods instead).
func (l *Logger) Warn(msg string, args ...any) {
	l.sl.Warn(msg, args...)
}

// Error logs an error message (without context - use *Context methods instead).
func (l *Logger) Error(msg string, args ...any) {
	l.sl.Error(msg, args...)
}

// WithComponent returns a logger with a component field.
func (l *Logger) WithComponent(component string) *Logger {
	newSL := l.sl.With("component", component)
	return &Logger{
		sl:  newSL,
		cfg: l.cfg,
		ctx: l.ctx,
	}
}

// WithRequestID returns a logger with a request ID field.
func (l *Logger) WithRequestID(requestID string) *Logger {
	newSL := l.sl.With("request_id", requestID)
	return &Logger{
		sl:  newSL,
		cfg: l.cfg,
		ctx: l.ctx,
	}
}

// With returns a logger with additional fields.
func (l *Logger) With(args ...any) *Logger {
	newSL := l.sl.With(args...)
	return &Logger{
		sl:  newSL,
		cfg: l.cfg,
		ctx: l.ctx,
	}
}

// WideEvent creates a wide event builder for emitting a single context-rich log.
// Use this for the wide events pattern - one log per request with all context.
func (l *Logger) WideEvent(ctx context.Context, msg string) *WideEventBuilder {
	return &WideEventBuilder{
		logger: l,
		ctx:    ctx,
		msg:    msg,
		attrs:  make(map[string]any),
		start:  time.Now(),
	}
}

// WideEventBuilder helps build wide events (one context-rich log per request).
type WideEventBuilder struct {
	logger *Logger
	ctx    context.Context
	msg    string
	attrs  map[string]any
	start  time.Time
	err    error
}

// With adds a key-value pair to the wide event.
func (b *WideEventBuilder) With(key string, value any) *WideEventBuilder {
	if b.logger.cfg.RedactFields != nil {
		for _, rf := range b.logger.cfg.RedactFields {
			if rf == key {
				b.attrs[key] = "[REDACTED]"
				return b
			}
		}
	}
	b.attrs[key] = value
	return b
}

// WithError adds an error to the wide event.
func (b *WideEventBuilder) WithError(err error) *WideEventBuilder {
	b.err = err
	if err != nil {
		b.attrs["error"] = err.Error()
		b.attrs["error_type"] = "error"
	}
	return b
}

// WithOutcome sets the outcome of the operation (success, error, etc).
func (b *WideEventBuilder) WithOutcome(outcome string) *WideEventBuilder {
	b.attrs["outcome"] = outcome
	return b
}

// WithStatusCode adds HTTP status code.
func (b *WideEventBuilder) WithStatusCode(code int) *WideEventBuilder {
	b.attrs["http.status"] = code
	return b
}

// Emit sends the wide event log.
func (b *WideEventBuilder) Emit() {
	// Calculate duration
	b.attrs["latency_ms"] = time.Since(b.start).Milliseconds()

	// Convert attrs to slog args
	args := make([]any, 0, len(b.attrs)*2)
	for k, v := range b.attrs {
		args = append(args, k, v)
	}

	if b.err != nil {
		b.logger.ErrorContext(b.ctx, b.msg, args...)
	} else {
		b.logger.InfoContext(b.ctx, b.msg, args...)
	}
}

// LogWideEvent is a convenience function for logging a wide event in one call.
// Example:
//
//	logger.LogWideEvent(ctx, "Request processed", map[string]any{
//	    "http.method": method,
//	    "http.path": path,
//	    "user_id": userID,
//	    "outcome": "success",
//	})
func (l *Logger) LogWideEvent(ctx context.Context, msg string, attrs map[string]any) {
	args := make([]any, 0, len(attrs)*2)
	for k, v := range attrs {
		args = append(args, k, v)
	}
	l.InfoContext(ctx, msg, args...)
}

// ContextKey is the context key for the logger.
type ContextKey struct{}

// FromContext retrieves the logger from context with automatic trace injection.
func FromContext(ctx context.Context) *Logger {
	if l, ok := ctx.Value(ContextKey{}).(*Logger); ok {
		return l
	}
	// Return a default logger
	return New(Config{Level: "info", Format: "json"})
}

// WithContext adds the logger to the context.
func (l *Logger) WithContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, ContextKey{}, l)
}

// Logger returns the underlying slog.Logger for advanced use.
func (l *Logger) Logger() *slog.Logger {
	return l.sl
}

// Fatal logs a fatal message and exits. Use sparingly - prefer returning errors.
func (l *Logger) Fatal(msg string, args ...any) {
	l.Error(msg, args...)
	os.Exit(1)
}

// CapturePanic captures panics and logs them. Use with defer.
func (l *Logger) CapturePanic(ctx context.Context) {
	if r := recover(); r != nil {
		// Get stack trace
		buf := make([]byte, 64<<10)
		n := runtime.Stack(buf, false)
		stack := string(buf[:n])

		l.ErrorContext(ctx, "panic recovered",
			"panic", r,
			"stack_trace", stack,
		)
		panic(r) // Re-panic after logging
	}
}
