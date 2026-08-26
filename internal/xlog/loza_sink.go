package xlog

import (
	"context"
	"errors"
	"time"

	aegionloza "github.com/aegion/aegion/internal/platform/loza"
	lozasdk "github.com/astraive/loza/sdks/go"
)

// lozaSink keeps legacy xlog callsites on the process-wide Loza pipeline.
// Explicit sinks remain available to isolated tests and tooling.
type lozaSink struct{}

func (lozaSink) Emit(ctx context.Context, record Record) error {
	fields := record.Fields
	name, _ := fields["event.name"].(string)
	kind, _ := fields["event.kind"].(string)
	outcome, _ := fields["event.outcome"].(string)
	startedAt := time.Now()
	if timestamp, ok := fields["timestamp"].(string); ok {
		if parsed, err := time.Parse(time.RFC3339Nano, timestamp); err == nil {
			startedAt = parsed
		}
	}

	logger := lozasdk.Default()
	eventCtx := aegionloza.Start(ctx, logger, lozasdk.Params{
		Event:     name,
		Kind:      kind,
		StartedAt: startedAt,
	})
	attrs := make([]lozasdk.Attr, 0, len(fields))
	for key, value := range fields {
		switch key {
		case "event.name", "event.kind", "event.outcome", "timestamp", "duration_ms":
			continue
		default:
			attrs = append(attrs, lozasdk.Any(key, value))
		}
	}
	if err := logger.Set(eventCtx, attrs...); err != nil {
		return err
	}
	normalizedOutcome := aegionloza.NormalizeOutcome(outcome)
	if normalizedOutcome == string(OutcomeError) {
		message, _ := fields["error.message"].(string)
		var eventErr error
		if message != "" {
			eventErr = errors.New(message)
		}
		if err := logger.FinishError(eventCtx, eventErr); err != nil {
			return err
		}
	} else if err := logger.Finish(eventCtx, normalizedOutcome); err != nil {
		return err
	}
	return logger.Emit(eventCtx)
}
