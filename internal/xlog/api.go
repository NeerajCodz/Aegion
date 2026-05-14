package xlog

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// With returns a logger with additional default fields.
func (l *Logger) With(args ...any) *Logger {
	if l == nil {
		l = Default()
	}
	next := *l
	next.fields = make(map[string]any, len(l.fields)+(len(args)/2))
	for key, value := range l.fields {
		next.fields[key] = value
	}
	applyArgs(next.fields, l.redactor, args...)
	return &next
}

// WithComponent adds a component field.
func (l *Logger) WithComponent(component string) *Logger {
	return l.With("component", component)
}

// WithRequestID adds a request id field.
func (l *Logger) WithRequestID(requestID string) *Logger {
	return l.With("request.id", requestID)
}

// Debug logs a system event.
func (l *Logger) Debug(msg string, args ...any) {
	l.log(context.Background(), normalizeEventName(msg), KindSystem, OutcomeSuccess, nil, args...)
}

// Info logs a system event.
func (l *Logger) Info(msg string, args ...any) {
	l.log(context.Background(), normalizeEventName(msg), KindSystem, OutcomeSuccess, nil, args...)
}

// Warn logs a rejected system event.
func (l *Logger) Warn(msg string, args ...any) {
	l.log(context.Background(), normalizeEventName(msg), KindSystem, OutcomeRejected, nil, args...)
}

// Error logs a failed system event.
func (l *Logger) Error(msg string, args ...any) {
	l.log(context.Background(), normalizeEventName(msg), KindSystem, OutcomeError, nil, args...)
}

// DebugContext logs a debug context event.
func (l *Logger) DebugContext(ctx context.Context, msg string, args ...any) {
	l.log(ctx, normalizeEventName(msg), KindSystem, OutcomeSuccess, nil, args...)
}

// InfoContext logs a context event.
func (l *Logger) InfoContext(ctx context.Context, msg string, args ...any) {
	l.log(ctx, normalizeEventName(msg), KindSystem, OutcomeSuccess, nil, args...)
}

// WarnContext logs a context warning event.
func (l *Logger) WarnContext(ctx context.Context, msg string, args ...any) {
	l.log(ctx, normalizeEventName(msg), KindSystem, OutcomeRejected, nil, args...)
}

// ErrorContext logs a context error event.
func (l *Logger) ErrorContext(ctx context.Context, msg string, args ...any) {
	l.log(ctx, normalizeEventName(msg), KindSystem, OutcomeError, nil, args...)
}

// Fatal logs an error and exits.
func (l *Logger) Fatal(msg string, args ...any) {
	l.log(context.Background(), normalizeEventName(msg), KindSystem, OutcomeError, nil, args...)
	os.Exit(1)
}

// WideEvent starts a wide request event for compatibility migrations.
func (l *Logger) WideEvent(ctx context.Context, msg string) *Event {
	return l.Start(ctx, normalizeEventName(msg), WithKind(KindRequest))
}

// LogWideEvent emits a compatibility wide event from a field map.
func (l *Logger) LogWideEvent(ctx context.Context, msg string, attrs map[string]any) {
	event := l.WideEvent(ctx, msg)
	for key, value := range attrs {
		if key == "error" {
			if err, ok := value.(error); ok {
				event.Error(err)
				continue
			}
		}
		event.Set(key, value)
	}
	if outcome, ok := attrs["outcome"].(string); ok {
		switch strings.ToLower(strings.TrimSpace(outcome)) {
		case string(OutcomeError), "failed":
			event.Error(nil)
		case string(OutcomeRejected), "warning":
			event.Rejected(nil)
		case string(OutcomeTimeout):
			event.Timeout(nil)
		case string(OutcomeCancelled):
			event.Cancelled(nil)
		default:
			event.Success()
		}
	} else {
		event.Success()
	}
	_ = event.Emit()
}

// WithContext returns a context carrying this logger.
func (l *Logger) WithContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, loggerContextKey{}, l)
}

// FromContext returns the logger from context or the default logger.
func FromContext(ctx context.Context) *Logger {
	if ctx != nil {
		if l, ok := ctx.Value(loggerContextKey{}).(*Logger); ok && l != nil {
			return l
		}
	}
	return Default()
}

func (l *Logger) log(ctx context.Context, name string, kind Kind, outcome Outcome, err error, args ...any) {
	if l == nil {
		l = Default()
	}
	event := l.Start(ctx, name, WithKind(kind))
	applyArgsToEvent(event, args...)
	switch outcome {
	case OutcomeError:
		event.Error(err)
	case OutcomeRejected:
		event.Rejected(err)
	case OutcomeTimeout:
		event.Timeout(err)
	case OutcomeCancelled:
		event.Cancelled(err)
	default:
		event.Success()
	}
	_ = event.Emit()
}

func applyArgsToEvent(event *Event, args ...any) {
	for i := 0; i < len(args); i += 2 {
		key := fmt.Sprint(args[i])
		var value any
		if i+1 < len(args) {
			value = args[i+1]
		}
		if key == "error" {
			if err, ok := value.(error); ok {
				event.Error(err)
				continue
			}
		}
		event.Set(key, value)
	}
}

func applyArgs(fields map[string]any, redactor *Redactor, args ...any) {
	for i := 0; i < len(args); i += 2 {
		key := strings.TrimSpace(fmt.Sprint(args[i]))
		if key == "" {
			continue
		}
		var value any
		if i+1 < len(args) {
			value = args[i+1]
		}
		fields[key] = redactor.Apply(key, value)
	}
}

type loggerContextKey struct{}

type xloggerProvider interface {
	XLogger() *Logger
}

// Adapt normalizes supported logger types to an xlog logger.
func Adapt(v any) *Logger {
	switch l := v.(type) {
	case nil:
		return Default()
	case *Logger:
		if l == nil {
			return Default()
		}
		return l
	case xloggerProvider:
		if adapted := l.XLogger(); adapted != nil {
			return adapted
		}
	case *slog.Logger:
		if l != nil {
			return Default()
		}
	}
	return Default()
}
