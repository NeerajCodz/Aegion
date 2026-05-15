// Package xlog implements Aegion wide-event logging.
package xlog

import (
	"context"
	"time"
)

// Kind describes the kind of operation captured by an event.
type Kind string

const (
	KindRequest  Kind = "request"
	KindJob      Kind = "job"
	KindWorkflow Kind = "workflow"
	KindMessage  Kind = "message"
	KindAgent    Kind = "agent"
	KindSystem   Kind = "system"
	KindSecurity Kind = "security"
	KindAudit    Kind = "audit"
)

// Outcome describes the final result of an event.
type Outcome string

const (
	OutcomeSuccess   Outcome = "success"
	OutcomeError     Outcome = "error"
	OutcomeTimeout   Outcome = "timeout"
	OutcomeCancelled Outcome = "cancelled"
	OutcomeRejected  Outcome = "rejected"
	OutcomeUnknown   Outcome = "unknown"
)

// Config controls xlog setup.
type Config struct {
	ServiceName    string
	ServiceNamespace string
	ServiceVersion string
	Environment    string
	Region         string
	CloudRegion    string
	DeploymentID   string
	CommitHash     string
	InstanceID     string
	Developer      string
	Level          string
	Format         string
	Output         string
	SchemaMode     string
	RedactFields   []string
	Sinks          []Sink
	Clock          func() time.Time
}

// Record is the serialized wide event sent to sinks.
type Record struct {
	Fields map[string]any
}

// Sink receives completed xlog records.
type Sink interface {
	Emit(context.Context, Record) error
}

// SinkFunc adapts a function into a sink.
type SinkFunc func(context.Context, Record) error

// Emit implements Sink.
func (f SinkFunc) Emit(ctx context.Context, r Record) error {
	return f(ctx, r)
}

// Logger is the configured xlog singleton handle.
type Logger struct {
	cfg      Config
	sink     Sink
	redactor *Redactor
	schema   *Schema
	now      func() time.Time
	fields   map[string]any
}

// Option adjusts a new event.
type Option func(*Event)

// WithKind sets event.kind.
func WithKind(kind Kind) Option {
	return func(e *Event) {
		e.kind = kind
	}
}

// WithField adds a field when the event starts.
func WithField(key string, value any) Option {
	return func(e *Event) {
		e.Set(key, value)
	}
}
