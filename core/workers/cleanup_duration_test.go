package workers

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestFlowCleanupWorker_UsesExactDurationsInSeconds(t *testing.T) {
	w := NewFlowCleanupWorker(FlowCleanupConfig{
		ExpiredAfter:   30 * time.Minute,
		CompletedAfter: 90 * time.Minute,
	})

	var capturedSQL string
	var capturedArgs []interface{}
	w.execFn = func(_ context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
		capturedSQL = sql
		capturedArgs = args
		return pgconn.CommandTag{}, nil
	}

	if _, err := w.cleanupFlows(context.Background()); err != nil {
		t.Fatalf("cleanupFlows returned error: %v", err)
	}

	if !strings.Contains(capturedSQL, "$1 * INTERVAL '1 second'") || !strings.Contains(capturedSQL, "$2 * INTERVAL '1 second'") {
		t.Fatalf("expected cleanupFlows SQL to use second-precision interval args, got: %s", capturedSQL)
	}
	if got, ok := capturedArgs[0].(float64); !ok || got != (30*time.Minute).Seconds() {
		t.Fatalf("unexpected expiredAfter arg: %#v", capturedArgs[0])
	}
	if got, ok := capturedArgs[1].(float64); !ok || got != (90*time.Minute).Seconds() {
		t.Fatalf("unexpected completedAfter arg: %#v", capturedArgs[1])
	}
}

func TestSessionCleanupWorker_DefaultsNegativeDurations(t *testing.T) {
	w := NewSessionCleanupWorker(SessionCleanupConfig{
		ExpiredAfter:  -12 * time.Hour,
		InactiveAfter: -30 * time.Minute,
	})

	var firstExecArgs []interface{}
	call := 0
	w.execFn = func(_ context.Context, _ string, args ...interface{}) (pgconn.CommandTag, error) {
		call++
		if call == 1 {
			firstExecArgs = args
		}
		return pgconn.CommandTag{}, nil
	}

	if err := w.cleanup(context.Background()); err != nil {
		t.Fatalf("cleanup returned error: %v", err)
	}

	if got, ok := firstExecArgs[0].(float64); !ok || got != (7*24*time.Hour).Seconds() {
		t.Fatalf("unexpected expiredAfter arg for default fallback: %#v", firstExecArgs[0])
	}
	if got, ok := firstExecArgs[1].(float64); !ok || got != (24*time.Hour).Seconds() {
		t.Fatalf("unexpected inactiveAfter arg for default fallback: %#v", firstExecArgs[1])
	}
}
