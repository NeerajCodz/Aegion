// Package logger provides structured logging for Aegion.
package logger

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
)

// Logger wraps zerolog for structured logging.
type Logger struct {
	zl zerolog.Logger
}

// Config holds logger configuration.
type Config struct {
	Level  string // debug, info, warn, error
	Format string // json, text
}

// New creates a new logger with the given configuration.
func New(cfg Config) *Logger {
	var output io.Writer = os.Stdout

	if cfg.Format == "text" {
		output = zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: time.RFC3339,
		}
	}

	level := parseLevel(cfg.Level)
	zl := zerolog.New(output).
		Level(level).
		With().
		Timestamp().
		Caller().
		Logger()

	return &Logger{zl: zl}
}

// parseLevel converts a string level to zerolog.Level.
func parseLevel(level string) zerolog.Level {
	switch level {
	case "debug":
		return zerolog.DebugLevel
	case "info":
		return zerolog.InfoLevel
	case "warn":
		return zerolog.WarnLevel
	case "error":
		return zerolog.ErrorLevel
	default:
		return zerolog.InfoLevel
	}
}

// With returns a logger with the given fields.
func (l *Logger) With() zerolog.Context {
	return l.zl.With()
}

// Debug logs a debug message.
func (l *Logger) Debug() *zerolog.Event {
	return l.zl.Debug()
}

// Info logs an info message.
func (l *Logger) Info() *zerolog.Event {
	return l.zl.Info()
}

// Warn logs a warning message.
func (l *Logger) Warn() *zerolog.Event {
	return l.zl.Warn()
}

// Error logs an error message.
func (l *Logger) Error() *zerolog.Event {
	return l.zl.Error()
}

// Fatal logs a fatal message and exits.
func (l *Logger) Fatal() *zerolog.Event {
	return l.zl.Fatal()
}

// WithComponent returns a logger with a component field.
func (l *Logger) WithComponent(component string) *Logger {
	return &Logger{
		zl: l.zl.With().Str("component", component).Logger(),
	}
}

// WithRequestID returns a logger with a request ID field.
func (l *Logger) WithRequestID(requestID string) *Logger {
	return &Logger{
		zl: l.zl.With().Str("request_id", requestID).Logger(),
	}
}

// ContextKey is the context key for the logger.
type ContextKey struct{}

// FromContext retrieves the logger from context with automatic trace injection.
func FromContext(ctx context.Context) *Logger {
	var logger *Logger
	if l, ok := ctx.Value(ContextKey{}).(*Logger); ok {
		logger = l
	} else {
		logger = New(Config{Level: "info", Format: "json"})
	}
	
	// Auto-inject trace information if available
	return logger.withTraceContext(ctx)
}

// withTraceContext adds trace information to the logger if available in context
func (l *Logger) withTraceContext(ctx context.Context) *Logger {
	// Try to extract trace info using well-known context keys
	traceInfo := getTraceInfoFromContext(ctx)
	requestID := getRequestIDFromContext(ctx)
	
	newLogger := l
	
	// Add trace ID and span ID if available
	if traceInfo.TraceID != "" {
		newLogger = &Logger{
			zl: newLogger.zl.With().Str("trace_id", traceInfo.TraceID).Logger(),
		}
	}
	
	if traceInfo.SpanID != "" {
		newLogger = &Logger{
			zl: newLogger.zl.With().Str("span_id", traceInfo.SpanID).Logger(),
		}
	}
	
	// Add request ID if available and not already set
	if requestID != "" {
		newLogger = &Logger{
			zl: newLogger.zl.With().Str("request_id", requestID).Logger(),
		}
	}
	
	return newLogger
}

// TraceInfo contains trace information
type TraceInfo struct {
	TraceID string `json:"trace_id"`
	SpanID  string `json:"span_id"`
}

// Helper functions to extract trace information from context
// These use the same context keys as the observability package
func getTraceInfoFromContext(ctx context.Context) TraceInfo {
	info := ctx.Value("trace_info")
	if info != nil {
		// Use reflection to handle different struct types with same field names
		switch v := info.(type) {
		case TraceInfo:
			return v
		default:
			// Try to extract fields dynamically using reflection would be complex
			// For now, return empty - this can be enhanced later
			_ = v
			return TraceInfo{}
		}
	}
	return TraceInfo{}
}

func getRequestIDFromContext(ctx context.Context) string {
	if requestID, ok := ctx.Value("request_id").(string); ok {
		return requestID
	}
	return ""
}

// WithContext adds the logger to the context.
func (l *Logger) WithContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, ContextKey{}, l)
}
