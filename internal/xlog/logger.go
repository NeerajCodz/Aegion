package xlog

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/aegion/aegion/internal/platform/observability"
)

var (
	defaultMu     sync.RWMutex
	defaultLogger *Logger
)

// New creates a logger. Events are delivered only to explicitly configured sinks.
func New(cfg Config) *Logger {
	if cfg.ServiceName == "" {
		cfg.ServiceName = "aegion"
	}
	if cfg.ServiceVersion == "" {
		cfg.ServiceVersion = "0.0.0"
	}
	if cfg.Region == "" {
		cfg.Region = cfg.CloudRegion
	}
	if cfg.DeploymentID == "" {
		cfg.DeploymentID = cfg.CommitHash
	}
	if cfg.Environment == "" {
		cfg.Environment = firstNonEmpty(os.Getenv("AEGION_ENVIRONMENT"), os.Getenv("AEGION_ENV"), "development")
	}
	if cfg.Clock == nil {
		cfg.Clock = func() time.Time { return time.Now().UTC() }
	}

	sinks := cfg.Sinks
	if len(sinks) == 0 {
		sinks = []Sink{lozaSink{}}
	}
	l := &Logger{
		cfg:      cfg,
		sink:     NewMultiSink(sinks...),
		redactor: NewRedactor(cfg.RedactFields),
		schema:   NewSchema(cfg.SchemaMode),
		now:      cfg.Clock,
		fields:   map[string]any{},
	}
	defaultMu.Lock()
	defaultLogger = l
	defaultMu.Unlock()
	return l
}

// Default returns the process-wide xlog logger.
func Default() *Logger {
	defaultMu.RLock()
	l := defaultLogger
	defaultMu.RUnlock()
	if l != nil {
		return l
	}
	return New(Config{})
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" {
			return v
		}
	}
	return ""
}

// Start begins a wide event.
func (l *Logger) Start(ctx context.Context, name string, opts ...Option) *Event {
	if ctx == nil {
		ctx = context.Background()
	}
	e := &Event{
		logger:  l,
		ctx:     ctx,
		name:    strings.TrimSpace(name),
		kind:    KindSystem,
		outcome: OutcomeUnknown,
		fields:  make(map[string]any),
		start:   l.now(),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(e)
		}
	}
	for key, value := range l.fields {
		e.Set(key, value)
	}
	return e
}

// Event is a mutable wide event emitted once.
type Event struct {
	logger  *Logger
	ctx     context.Context
	name    string
	kind    Kind
	outcome Outcome
	fields  map[string]any
	start   time.Time
	err     error
	emitted bool
	mu      sync.Mutex
}

// Set attaches context to the event.
func (e *Event) Set(key string, value any) *Event {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.emitted {
		return e
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return e
	}
	e.fields[key] = e.logger.redactor.Apply(key, value)
	return e
}

// Success marks the event successful.
func (e *Event) Success() *Event {
	return e.setOutcome(OutcomeSuccess)
}

// Error marks the event failed and adds structured error fields.
func (e *Event) Error(err error) *Event {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.outcome = OutcomeError
	e.err = err
	if err != nil {
		e.fields["error.type"] = fmt.Sprintf("%T", err)
		e.fields["error.message"] = err.Error()
		var coded interface{ Code() string }
		if errors.As(err, &coded) {
			e.fields["error.code"] = coded.Code()
		}
	}
	return e
}

// Timeout marks the event timed out.
func (e *Event) Timeout(err error) *Event {
	e.Error(err)
	e.outcome = OutcomeTimeout
	return e
}

// Cancelled marks the event cancelled.
func (e *Event) Cancelled(err error) *Event {
	e.Error(err)
	e.outcome = OutcomeCancelled
	return e
}

// Rejected marks the event rejected.
func (e *Event) Rejected(err error) *Event {
	e.Error(err)
	e.outcome = OutcomeRejected
	return e
}

func (e *Event) setOutcome(outcome Outcome) *Event {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.outcome = outcome
	return e
}

// Emit finalizes and sends the event once.
func (e *Event) Emit() error {
	e.mu.Lock()
	if e.emitted {
		e.mu.Unlock()
		return nil
	}
	e.emitted = true
	record := e.recordLocked()
	e.mu.Unlock()

	if err := e.logger.schema.Validate(record.Fields); err != nil && e.logger.schema.mode == "strict" {
		return err
	}
	return e.logger.sink.Emit(e.ctx, record)
}

func (e *Event) recordLocked() Record {
	now := e.logger.now()
	fields := make(map[string]any, len(e.fields)+16)
	for k, v := range e.fields {
		fields[k] = v
	}
	fields["event.name"] = e.name
	fields["event.kind"] = string(e.kind)
	fields["event.outcome"] = string(e.outcome)
	fields["timestamp"] = now.Format(time.RFC3339Nano)
	fields["duration_ms"] = now.Sub(e.start).Milliseconds()
	fields["service.name"] = e.logger.cfg.ServiceName
	fields["service.version"] = e.logger.cfg.ServiceVersion
	fields["environment"] = e.logger.cfg.Environment
	if e.logger.cfg.Region != "" {
		fields["region"] = e.logger.cfg.Region
	}
	if e.logger.cfg.ServiceNamespace != "" {
		fields["service.namespace"] = e.logger.cfg.ServiceNamespace
	}
	if e.logger.cfg.DeploymentID != "" {
		fields["deployment.id"] = e.logger.cfg.DeploymentID
	}
	if e.logger.cfg.InstanceID != "" {
		fields["host.name"] = e.logger.cfg.InstanceID
	}
	if e.logger.cfg.Developer != "" {
		fields["developer"] = e.logger.cfg.Developer
	}
	traceInfo := observability.GetTraceInfoForLogger(e.ctx)
	if traceInfo.TraceID != "" {
		fields["trace.id"] = traceInfo.TraceID
	}
	if traceInfo.SpanID != "" {
		fields["span.id"] = traceInfo.SpanID
	}
	if requestID := observability.GetRequestIDForLogger(e.ctx); requestID != "" {
		fields["request.id"] = requestID
	}
	return Record{Fields: fields}
}

// Run executes fn inside the event lifecycle.
func (e *Event) Run(fn func(context.Context, *Event) error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("panic: %v", recovered)
			e.Set("error.stack", string(debug.Stack()))
			e.Error(err)
			_ = e.Emit()
			panic(recovered)
		}
		if err != nil {
			e.Error(err)
		} else {
			e.Success()
		}
		_ = e.Emit()
	}()
	err = fn(e.ctx, e)
	return err
}
