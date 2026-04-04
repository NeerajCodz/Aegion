package eventbus

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestPublish_JSONMarshalFailures(t *testing.T) {
	b := newTestBus()

	err := b.Publish(context.Background(), Event{
		Type:         "identity.created",
		SourceModule: "identity",
		Payload: map[string]interface{}{
			"bad": func() {},
		},
	})
	if err == nil {
		t.Fatalf("expected payload marshal error")
	}

	err = b.Publish(context.Background(), Event{
		Type:         "identity.created",
		SourceModule: "identity",
		Payload:      map[string]interface{}{"ok": true},
		Metadata: map[string]interface{}{
			"bad": func() {},
		},
	})
	if err == nil {
		t.Fatalf("expected metadata marshal error")
	}
}

func TestPublish_InsertEventError(t *testing.T) {
	b := newTestBus()
	tx := &stubTx{execErrOn: 1}
	b.beginTx = func(context.Context) (eventTx, error) { return tx, nil }

	err := b.Publish(context.Background(), Event{
		Type:         "identity.created",
		SourceModule: "identity",
		Payload:      map[string]interface{}{"ok": true},
	})
	if err == nil {
		t.Fatalf("expected insert event exec error")
	}
}

func TestProcessPending_ScanAndMetadataBranches(t *testing.T) {
	t.Run("scan error is skipped", func(t *testing.T) {
		b := newTestBus()
		b.Subscribe("audit", []string{"identity.created"}, func(context.Context, Event) error { return nil })

		rows := &stubRows{
			rows: [][]interface{}{
				{uuid.New(), uuid.New(), 0, "identity.created", "identity", "identity", "id-1", nil, []byte(`{}`), []byte(`{}`), time.Now().UTC()},
			},
			scanErr: map[int]error{0: errors.New("scan failed")},
		}
		b.queryRows = func(context.Context, string, ...interface{}) (eventRows, error) { return rows, nil }

		updateCalls := 0
		b.execStmt = func(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
			updateCalls++
			return pgconn.NewCommandTag("UPDATE 1"), nil
		}

		if err := b.ProcessPending(context.Background(), "audit"); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if updateCalls != 0 {
			t.Fatalf("expected no delivery updates on scan error, got %d", updateCalls)
		}
	})

	t.Run("invalid metadata marks failed", func(t *testing.T) {
		b := newTestBus()
		b.Subscribe("audit", []string{"identity.created"}, func(context.Context, Event) error { return nil })

		rows := &stubRows{
			rows: [][]interface{}{
				{uuid.New(), uuid.New(), 1, "identity.created", "identity", "identity", "id-1", nil, []byte(`{"ok":true}`), []byte("{bad"), time.Now().UTC()},
			},
		}
		b.queryRows = func(context.Context, string, ...interface{}) (eventRows, error) { return rows, nil }

		updateCalls := 0
		b.execStmt = func(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
			updateCalls++
			return pgconn.NewCommandTag("UPDATE 1"), nil
		}

		if err := b.ProcessPending(context.Background(), "audit"); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if updateCalls != 1 {
			t.Fatalf("expected one markFailed update, got %d", updateCalls)
		}
	})
}

func TestCleanup_ExecError(t *testing.T) {
	b := newTestBus()
	b.execStmt = func(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, errors.New("cleanup failed")
	}

	if _, err := b.Cleanup(context.Background(), time.Hour); err == nil || err.Error() != "cleanup failed" {
		t.Fatalf("expected cleanup failure, got %v", err)
	}
}
