package workers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/aegion/aegion/core/courier"
)

func TestCourierDispatchWorker_DispatchCourierErrorBranch(t *testing.T) {
	w := NewCourierDispatchWorker(CourierDispatchConfig{
		Courier:   courier.New(courier.Config{}), // DB unavailable in courier path
		Interval:  time.Second,
		BatchSize: 1,
	})

	if err := w.dispatch(context.Background()); err == nil {
		t.Fatalf("expected dispatch error from courier ProcessQueue")
	}
}

func TestFlowCleanupWorker_ErrorBranches(t *testing.T) {
	w := NewFlowCleanupWorker(FlowCleanupConfig{Interval: time.Second})
	w.execFn = func(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, errors.New("db unavailable")
	}

	if _, err := w.CleanupOldFlows(context.Background(), time.Hour); err == nil || err.Error() != "db unavailable" {
		t.Fatalf("expected flow cleanup error, got %v", err)
	}
	if _, err := w.CleanupOldContainers(context.Background(), time.Hour); err == nil || err.Error() != "db unavailable" {
		t.Fatalf("expected container cleanup error, got %v", err)
	}
}

func TestEventProcessorWorker_CleanupOldEventsError(t *testing.T) {
	w := NewEventProcessorWorker(EventProcessorConfig{
		Subscriber: "audit",
		Interval:   time.Second,
	})
	w.execFn = func(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, errors.New("cleanup failed")
	}

	if _, err := w.CleanupOldEvents(context.Background(), time.Hour); err == nil || err.Error() != "cleanup failed" {
		t.Fatalf("expected cleanup failure, got %v", err)
	}
}
