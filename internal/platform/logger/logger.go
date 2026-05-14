// Package logger provides structured logging for Aegion using slog with wide events pattern.
package logger

import (
	"context"
	"io"
	"log/slog"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/aegion/aegion/internal/platform/observability"
	"github.com/aegion/aegion/internal/xlog"
)

// Config holds logger configuration.
type Config struct {
	Level            string   // debug, info, warn, error
	Format           string   // json, text
	ServiceName      string   // service_name for all logs
	ServiceNamespace string   // service_namespace for all logs
	Environment      string   // environment
	CloudRegion      string   // cloud_region
	Version          string   // service_version
	CommitHash       string   // commit_hash
	InstanceID       string   // instance_id
	Developer        string   // developer (local only)
	RedactFields     []string // fields to redact from logs
}

// serviceInfoHandler wraps slog.Handler and injects service metadata into every log.
type serviceInfoHandler struct {
	handler   slog.Handler
	cfg       Config
	attrs     []slog.Attr
	redactMap map[string]bool
}

func newServiceInfoHandler(w io.Writer, cfg Config) *serviceInfoHandler {
	var handler slog.Handler
	opts := &slog.HandlerOptions{
		Level:     parseLevel(cfg.Level),
		AddSource: true,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Ensure keys match requirements: time, level, msg
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

	// Build base attributes from config using standardized keys
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
	if cfg.Developer != "" {
		baseAttrs = append(baseAttrs, slog.String("developer", cfg.Developer))
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
	r.AddAttrs(h.attrs...)

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
		attrs:     h.attrs,
		redactMap: h.redactMap,
	}
}

func (h *serviceInfoHandler) WithGroup(name string) slog.Handler {
	return &serviceInfoHandler{
		handler:   h.handler.WithGroup(name),
		cfg:       h.cfg,
		attrs:     h.attrs,
		redactMap: h.redactMap,
	}
}

func (h *serviceInfoHandler) redactRecord(r slog.Record) slog.Record {
	newRecord := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)

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
	*slog.Logger
	cfg  Config
	xlog *xlog.Logger
}

// New creates a new logger with the given configuration and sets it as the default slog logger.
func New(cfg Config) *Logger {
	xl := xlog.New(xlog.Config{
		ServiceName:    cfg.ServiceName,
		ServiceVersion: cfg.Version,
		Environment:    cfg.Environment,
		Region:         cfg.CloudRegion,
		DeploymentID:   cfg.CommitHash,
		InstanceID:     cfg.InstanceID,
		Level:          cfg.Level,
		Format:         cfg.Format,
		RedactFields:   cfg.RedactFields,
	})
	handler := xlog.NewSlogHandler(xl)
	sl := slog.New(handler)

	// Set as global default so slog.InfoContext works
	slog.SetDefault(sl)

	return &Logger{
		Logger: sl,
		cfg:    cfg,
		xlog:   xl,
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

// WithComponent returns a logger with a component field.
func (l *Logger) WithComponent(component string) *Logger {
	return &Logger{
		Logger: l.Logger.With("component", component),
		cfg:    l.cfg,
		xlog:   l.xlog,
	}
}

// WithRequestID returns a logger with a request ID field.
func (l *Logger) WithRequestID(requestID string) *Logger {
	return &Logger{
		Logger: l.Logger.With("request_id", requestID),
		cfg:    l.cfg,
		xlog:   l.xlog,
	}
}

// With returns a logger with additional fields.
func (l *Logger) With(args ...any) *Logger {
	return &Logger{
		Logger: l.Logger.With(args...),
		cfg:    l.cfg,
		xlog:   l.xlog,
	}
}

// XLogger exposes the native xlog logger for migration callers and tests.
func (l *Logger) XLogger() *xlog.Logger {
	if l == nil || l.xlog == nil {
		return xlog.Default()
	}
	return l.xlog
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
	mu     sync.Mutex
}

// With adds a key-value pair to the wide event.
func (b *WideEventBuilder) With(key string, value any) *WideEventBuilder {
	b.mu.Lock()
	defer b.mu.Unlock()
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
	b.mu.Lock()
	defer b.mu.Unlock()
	b.err = err
	if err != nil {
		b.attrs["error"] = err.Error()
	}
	return b
}

// WithOutcome sets the outcome of the operation (success, error, etc).
func (b *WideEventBuilder) WithOutcome(outcome string) *WideEventBuilder {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.attrs["outcome"] = outcome
	return b
}

// WithStatusCode adds HTTP status code.
func (b *WideEventBuilder) WithStatusCode(code int) *WideEventBuilder {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.attrs["http.status_code"] = code
	return b
}

// Emit sends the wide event log.
func (b *WideEventBuilder) Emit() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.logger.xlog != nil {
		event := b.logger.xlog.Start(b.ctx, b.msg, xlog.WithKind(xlog.KindRequest))
		for k, v := range b.attrs {
			event.Set(k, v)
		}
		latencyMs := time.Since(b.start).Milliseconds()
		event.Set("duration_ms", latencyMs)
		event.Set("latency_ms", latencyMs)
		if b.err != nil {
			event.Error(b.err)
		} else if outcome, ok := b.attrs["outcome"].(string); ok && outcome != "success" {
			event.Rejected(nil)
		} else {
			event.Success()
		}
		_ = event.Emit()
		return
	}

	b.attrs["latency_ms"] = time.Since(b.start).Milliseconds()
	args := make([]any, 0, len(b.attrs)*2)
	for k, v := range b.attrs {
		args = append(args, k, v)
	}
	b.logger.InfoContext(b.ctx, b.msg, args...)
}

// ContextKey is the context key for the logger.
type ContextKey struct{}

// FromContext retrieves the logger from context.
func FromContext(ctx context.Context) *Logger {
	if l, ok := ctx.Value(ContextKey{}).(*Logger); ok {
		return l
	}
	// Return a default logger wrapper around the global slog
	return &Logger{Logger: slog.Default(), cfg: Config{Level: "info", Format: "json"}, xlog: xlog.Default()}
}

// WithContext adds the logger to the context.
func (l *Logger) WithContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, ContextKey{}, l)
}

// LogWideEvent is a convenience method to log a wide event directly from a map.
func (l *Logger) LogWideEvent(ctx context.Context, msg string, attrs map[string]any) {
	builder := l.WideEvent(ctx, msg)
	for k, v := range attrs {
		if k == "error" {
			if err, ok := v.(error); ok {
				builder.WithError(err)
				continue
			}
		}
		if k == "outcome" {
			if outcome, ok := v.(string); ok {
				builder.WithOutcome(outcome)
				continue
			}
		}
		if k == "http.status_code" || k == "http.status" {
			if code, ok := v.(int); ok {
				builder.WithStatusCode(code)
				continue
			}
		}
		builder.With(k, v)
	}
	builder.Emit()
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
