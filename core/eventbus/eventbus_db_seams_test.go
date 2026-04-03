package eventbus

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type stubTx struct {
	execErrOn  int
	execCalls  int
	committed  bool
	rolledBack bool
	lastSQL    []string
	lastArgs   [][]interface{}
}

func (s *stubTx) Exec(_ context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error) {
	s.execCalls++
	s.lastSQL = append(s.lastSQL, sql)
	s.lastArgs = append(s.lastArgs, arguments)
	if s.execErrOn > 0 && s.execCalls == s.execErrOn {
		return pgconn.CommandTag{}, errors.New("exec failed")
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func (s *stubTx) Commit(_ context.Context) error {
	s.committed = true
	return nil
}

func (s *stubTx) Rollback(_ context.Context) error {
	s.rolledBack = true
	return nil
}

type stubRows struct {
	rows    [][]interface{}
	scanErr map[int]error
	err     error
	nextIdx int
	closed  bool
}

func (s *stubRows) Close()     { s.closed = true }
func (s *stubRows) Err() error { return s.err }
func (s *stubRows) Next() bool {
	return s.nextIdx < len(s.rows)
}
func (s *stubRows) Scan(dest ...interface{}) error {
	rowIdx := s.nextIdx
	s.nextIdx++
	if e, ok := s.scanErr[rowIdx]; ok {
		return e
	}
	row := s.rows[rowIdx]
	for i := range dest {
		switch d := dest[i].(type) {
		case *uuid.UUID:
			*d = row[i].(uuid.UUID)
		case *int:
			*d = row[i].(int)
		case *string:
			*d = row[i].(string)
		case *[]byte:
			*d = row[i].([]byte)
		case *time.Time:
			*d = row[i].(time.Time)
		case **uuid.UUID:
			if row[i] == nil {
				*d = nil
			} else {
				id := row[i].(uuid.UUID)
				*d = &id
			}
		default:
			return errors.New("unsupported scan destination")
		}
	}
	return nil
}

func newTestBus() *Bus {
	b := New(Config{MaxRetries: 3, RetryDelay: 10 * time.Millisecond})
	b.now = func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	return b
}

func TestPublish_WithSeams(t *testing.T) {
	t.Run("success inserts event and deliveries", func(t *testing.T) {
		b := newTestBus()
		tx := &stubTx{}
		b.beginTx = func(context.Context) (eventTx, error) { return tx, nil }

		b.Subscribe("audit", []string{"identity.created"}, func(context.Context, Event) error { return nil })
		b.Subscribe("notifications", []string{"identity.created"}, func(context.Context, Event) error { return nil })

		err := b.Publish(context.Background(), Event{
			Type:         "identity.created",
			SourceModule: "identity",
			Payload:      map[string]interface{}{"id": "123"},
		})
		if err != nil {
			t.Fatalf("Publish returned error: %v", err)
		}
		if !tx.committed {
			t.Fatalf("expected transaction commit")
		}
		if tx.execCalls != 3 {
			t.Fatalf("expected 3 exec calls (event + 2 deliveries), got %d", tx.execCalls)
		}
	})

	t.Run("begin transaction failure", func(t *testing.T) {
		b := newTestBus()
		b.beginTx = func(context.Context) (eventTx, error) { return nil, errors.New("begin failed") }
		err := b.Publish(context.Background(), Event{
			Type:         "identity.created",
			SourceModule: "identity",
			Payload:      map[string]interface{}{"id": "123"},
		})
		if err == nil || err.Error() != "begin failed" {
			t.Fatalf("expected begin failure, got %v", err)
		}
	})

	t.Run("exec failure during delivery insert", func(t *testing.T) {
		b := newTestBus()
		tx := &stubTx{execErrOn: 2}
		b.beginTx = func(context.Context) (eventTx, error) { return tx, nil }
		b.Subscribe("audit", []string{"identity.created"}, func(context.Context, Event) error { return nil })

		err := b.Publish(context.Background(), Event{
			Type:         "identity.created",
			SourceModule: "identity",
			Payload:      map[string]interface{}{"id": "123"},
		})
		if err == nil {
			t.Fatalf("expected error on second exec call")
		}
		if tx.committed {
			t.Fatalf("did not expect commit on failure")
		}
	})
}

func TestProcessPending_WithSeams(t *testing.T) {
	t.Run("no handler configured", func(t *testing.T) {
		b := newTestBus()
		if err := b.ProcessPending(context.Background(), "unknown"); err != nil {
			t.Fatalf("expected nil error when no handler exists, got %v", err)
		}
	})

	t.Run("query failure", func(t *testing.T) {
		b := newTestBus()
		b.Subscribe("audit", []string{"identity.created"}, func(context.Context, Event) error { return nil })
		b.queryRows = func(context.Context, string, ...interface{}) (eventRows, error) {
			return nil, errors.New("query failed")
		}
		err := b.ProcessPending(context.Background(), "audit")
		if err == nil || err.Error() != "query failed" {
			t.Fatalf("expected query failure, got %v", err)
		}
	})

	t.Run("handler success and failure paths", func(t *testing.T) {
		b := newTestBus()
		deliverySuccess := uuid.New()
		deliveryFail := uuid.New()
		eventID := uuid.New()
		now := time.Now().UTC()
		payload, _ := json.Marshal(map[string]interface{}{"ok": true})
		metadata, _ := json.Marshal(map[string]interface{}{"source": "test"})

		rows := &stubRows{
			rows: [][]interface{}{
				{deliverySuccess, eventID, 0, "identity.created", "identity", "identity", "id-1", nil, payload, metadata, now},
				{deliveryFail, eventID, 1, "identity.created", "identity", "identity", "id-1", nil, payload, metadata, now},
			},
		}
		b.queryRows = func(context.Context, string, ...interface{}) (eventRows, error) { return rows, nil }

		var execCalls []string
		b.execStmt = func(_ context.Context, query string, _ ...interface{}) (pgconn.CommandTag, error) {
			execCalls = append(execCalls, query)
			return pgconn.NewCommandTag("UPDATE 1"), nil
		}

		calls := 0
		b.Subscribe("audit", []string{"identity.created"}, func(context.Context, Event) error {
			calls++
			if calls == 2 {
				return errors.New("handler failed")
			}
			return nil
		})

		err := b.ProcessPending(context.Background(), "audit")
		if err != nil {
			t.Fatalf("ProcessPending returned error: %v", err)
		}
		if !rows.closed {
			t.Fatalf("expected rows to be closed")
		}
		if calls != 2 {
			t.Fatalf("expected handler called twice, got %d", calls)
		}
		if len(execCalls) != 2 {
			t.Fatalf("expected two update calls, got %d", len(execCalls))
		}
	})

	t.Run("invalid payload triggers markFailed", func(t *testing.T) {
		b := newTestBus()
		deliveryID := uuid.New()
		eventID := uuid.New()
		now := time.Now().UTC()
		rows := &stubRows{
			rows: [][]interface{}{
				{deliveryID, eventID, 0, "identity.created", "identity", "identity", "id-1", nil, []byte("{bad"), []byte("{}"), now},
			},
		}
		b.queryRows = func(context.Context, string, ...interface{}) (eventRows, error) { return rows, nil }
		called := 0
		b.execStmt = func(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
			called++
			return pgconn.NewCommandTag("UPDATE 1"), nil
		}
		b.Subscribe("audit", []string{"identity.created"}, func(context.Context, Event) error { return nil })

		if err := b.ProcessPending(context.Background(), "audit"); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if called != 1 {
			t.Fatalf("expected markFailed update once, got %d", called)
		}
	})
}

func TestMarkFailedAndCleanup_WithSeams(t *testing.T) {
	b := newTestBus()
	var calledArgs [][]interface{}
	b.execStmt = func(_ context.Context, _ string, args ...interface{}) (pgconn.CommandTag, error) {
		calledArgs = append(calledArgs, args)
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}

	b.markFailed(context.Background(), uuid.New(), 0, errors.New("temporary"))
	if len(calledArgs) != 1 {
		t.Fatalf("expected one update call")
	}
	if calledArgs[0][1].(int) != 1 {
		t.Fatalf("expected incremented attempt count 1")
	}

	b.markFailed(context.Background(), uuid.New(), 2, errors.New("fatal"))
	if calledArgs[1][1].(int) != 3 {
		t.Fatalf("expected dead-letter attempt count 3")
	}

	cleanupCount := int64(4)
	b.execStmt = func(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
		return pgconn.NewCommandTag("DELETE 4"), nil
	}
	deleted, err := b.Cleanup(context.Background(), time.Hour)
	if err != nil {
		t.Fatalf("cleanup returned error: %v", err)
	}
	if deleted != cleanupCount {
		t.Fatalf("expected %d rows deleted, got %d", cleanupCount, deleted)
	}
}

func TestPublishHelperMethods_CallPublish(t *testing.T) {
	b := newTestBus()
	tx := &stubTx{}
	b.beginTx = func(context.Context) (eventTx, error) { return tx, nil }

	identityID := uuid.New()
	sessionID := uuid.New()

	if err := b.PublishIdentityCreated(context.Background(), identityID, "identity"); err != nil {
		t.Fatalf("PublishIdentityCreated failed: %v", err)
	}
	if err := b.PublishLoginSucceeded(context.Background(), identityID, sessionID, "password", "auth"); err != nil {
		t.Fatalf("PublishLoginSucceeded failed: %v", err)
	}
	if err := b.PublishLoginFailed(context.Background(), "user@example.com", "invalid", "auth", &identityID); err != nil {
		t.Fatalf("PublishLoginFailed failed: %v", err)
	}
}

func TestBusDefaults_DBUnavailableSentinel(t *testing.T) {
	b := New(Config{})
	if _, err := b.beginTx(context.Background()); !errors.Is(err, errDatabaseUnavailable) {
		t.Fatalf("expected errDatabaseUnavailable from beginTx, got %v", err)
	}
	if _, err := b.queryRows(context.Background(), "select 1"); !errors.Is(err, errDatabaseUnavailable) {
		t.Fatalf("expected errDatabaseUnavailable from queryRows, got %v", err)
	}
	if _, err := b.execStmt(context.Background(), "select 1"); !errors.Is(err, errDatabaseUnavailable) {
		t.Fatalf("expected errDatabaseUnavailable from execStmt, got %v", err)
	}
}

func TestStubRows_ScanUnsupportedDestination(t *testing.T) {
	rows := &stubRows{
		rows: [][]interface{}{
			{uuid.New()},
		},
	}
	var unsupported pgtype.UUID
	if !rows.Next() {
		t.Fatalf("expected row")
	}
	if err := rows.Scan(&unsupported); err == nil {
		t.Fatalf("expected unsupported destination error")
	}
}
