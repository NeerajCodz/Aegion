package workers

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/aegion/aegion/core/eventbus"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type dbPathRows struct {
	rows    [][]interface{}
	scanErr map[int]error
	err     error
	nextIdx int
	closed  bool
}

func (s *dbPathRows) Close()     { s.closed = true }
func (s *dbPathRows) Err() error { return s.err }
func (s *dbPathRows) Next() bool { return s.nextIdx < len(s.rows) }
func (s *dbPathRows) Scan(dest ...interface{}) error {
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
				v := row[i].(uuid.UUID)
				*d = &v
			}
		case **string:
			if row[i] == nil {
				*d = nil
			} else {
				v := row[i].(string)
				*d = &v
			}
		default:
			return errors.New("unsupported scan destination")
		}
	}
	return nil
}

func (s *dbPathRows) CommandTag() pgconn.CommandTag { return pgconn.NewCommandTag("SELECT 0") }
func (s *dbPathRows) FieldDescriptions() []pgconn.FieldDescription {
	return nil
}
func (s *dbPathRows) Values() ([]interface{}, error) { return nil, nil }
func (s *dbPathRows) RawValues() [][]byte            { return nil }
func (s *dbPathRows) Conn() *pgx.Conn                { return nil }

func (s *dbPathRows) NextRow() bool { return s.Next() }

func (s *dbPathRows) ScanRow(dest ...interface{}) error { return s.Scan(dest...) }

func (s *dbPathRows) TypeMap() *pgtype.Map { return nil }

func (s *dbPathRows) ValuesRaw() [][]byte { return nil }

type dbPathRow struct {
	values []interface{}
	err    error
}

func (r dbPathRow) Scan(dest ...interface{}) error {
	if r.err != nil {
		return r.err
	}
	for i := range dest {
		switch d := dest[i].(type) {
		case *int64:
			*d = r.values[i].(int64)
		default:
			return errors.New("unsupported row scan destination")
		}
	}
	return nil
}

func TestSessionCleanupWorker_DBSeams(t *testing.T) {
	ctx := context.Background()

	t.Run("success path", func(t *testing.T) {
		worker := NewSessionCleanupWorker(SessionCleanupConfig{Interval: time.Minute})
		execCalls := 0
		worker.execFn = func(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
			execCalls++
			if execCalls == 1 {
				return pgconn.NewCommandTag("DELETE 2"), nil
			}
			return pgconn.NewCommandTag("DELETE 1"), nil
		}
		if err := worker.cleanup(ctx); err != nil {
			t.Fatalf("cleanup returned error: %v", err)
		}
		if execCalls != 2 {
			t.Fatalf("expected 2 exec calls, got %d", execCalls)
		}
	})

	t.Run("initial delete fails", func(t *testing.T) {
		worker := NewSessionCleanupWorker(SessionCleanupConfig{Interval: time.Minute})
		worker.execFn = func(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, errors.New("delete failed")
		}
		if err := worker.cleanup(ctx); err == nil || err.Error() != "delete failed" {
			t.Fatalf("expected delete failure, got %v", err)
		}
	})

	t.Run("orphan cleanup failure is tolerated", func(t *testing.T) {
		worker := NewSessionCleanupWorker(SessionCleanupConfig{Interval: time.Minute})
		execCalls := 0
		worker.execFn = func(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
			execCalls++
			if execCalls == 1 {
				return pgconn.NewCommandTag("DELETE 0"), nil
			}
			return pgconn.CommandTag{}, errors.New("orphan delete failed")
		}
		if err := worker.cleanup(ctx); err != nil {
			t.Fatalf("cleanup should tolerate orphan delete failures, got %v", err)
		}
	})
}

func TestFlowCleanupWorker_DBSeams(t *testing.T) {
	ctx := context.Background()

	t.Run("cleanup combines errors and succeeds", func(t *testing.T) {
		worker := NewFlowCleanupWorker(FlowCleanupConfig{Interval: time.Minute})
		execCalls := 0
		worker.execFn = func(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
			execCalls++
			switch execCalls {
			case 1:
				return pgconn.CommandTag{}, errors.New("flows failed")
			case 2:
				return pgconn.CommandTag{}, errors.New("containers failed")
			default:
				return pgconn.NewCommandTag("DELETE 0"), nil
			}
		}
		if err := worker.cleanup(ctx); err != nil {
			t.Fatalf("cleanup should return nil even when sub-cleanups fail, got %v", err)
		}
	})

	t.Run("cleanupFlows and cleanupContinuityContainers helpers", func(t *testing.T) {
		worker := NewFlowCleanupWorker(FlowCleanupConfig{Interval: time.Minute})
		worker.execFn = func(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("DELETE 4"), nil
		}
		deleted, err := worker.cleanupFlows(ctx)
		if err != nil || deleted != 4 {
			t.Fatalf("cleanupFlows returned deleted=%d err=%v", deleted, err)
		}
		deleted, err = worker.cleanupContinuityContainers(ctx)
		if err != nil || deleted != 4 {
			t.Fatalf("cleanupContinuityContainers returned deleted=%d err=%v", deleted, err)
		}
	})

	t.Run("cleanup old helpers", func(t *testing.T) {
		worker := NewFlowCleanupWorker(FlowCleanupConfig{Interval: time.Minute})
		worker.execFn = func(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("DELETE 3"), nil
		}
		deleted, err := worker.CleanupOldFlows(ctx, 24*time.Hour)
		if err != nil || deleted != 3 {
			t.Fatalf("CleanupOldFlows returned deleted=%d err=%v", deleted, err)
		}
		deleted, err = worker.CleanupOldContainers(ctx, 24*time.Hour)
		if err != nil || deleted != 3 {
			t.Fatalf("CleanupOldContainers returned deleted=%d err=%v", deleted, err)
		}
	})
}

func TestEventProcessorWorker_DBSeams(t *testing.T) {
	ctx := context.Background()

	t.Run("process uses event bus path", func(t *testing.T) {
		worker := NewEventProcessorWorker(EventProcessorConfig{
			Interval:   time.Second,
			Subscriber: "sub",
			EventBus:   eventbus.New(eventbus.Config{}),
		})
		worker.eventBus = eventbus.New(eventbus.Config{})
		if err := worker.process(ctx); err != nil {
			t.Fatalf("process with event bus should succeed without handler, got %v", err)
		}
	})

	t.Run("process falls back to direct path without event bus", func(t *testing.T) {
		worker := NewEventProcessorWorker(EventProcessorConfig{
			Interval:   time.Second,
			Subscriber: "sub",
			BatchSize:  10,
		})
		worker.queryFn = func(context.Context, string, ...interface{}) (pgx.Rows, error) {
			return &dbPathRows{}, nil
		}
		if err := worker.process(ctx); err != nil {
			t.Fatalf("process fallback should succeed, got %v", err)
		}
	})

	t.Run("processDirectly handles scan errors, invalid JSON and row errors", func(t *testing.T) {
		worker := NewEventProcessorWorker(EventProcessorConfig{
			Interval:   time.Second,
			Subscriber: "sub",
			BatchSize:  10,
		})
		deliveryID1 := uuid.New()
		eventID1 := uuid.New()
		deliveryID2 := uuid.New()
		eventID2 := uuid.New()
		now := time.Now()
		worker.queryFn = func(context.Context, string, ...interface{}) (pgx.Rows, error) {
			return &dbPathRows{
				rows: [][]interface{}{
					{deliveryID1, eventID1, 0, "evt", "src", nil, nil, nil, []byte(`{"a":`), []byte(`{"m":1}`), now},
					{deliveryID2, eventID2, 1, "evt2", "src2", nil, nil, nil, []byte(`{"a":1}`), []byte(`{"m":2}`), now},
				},
				scanErr: map[int]error{
					0: errors.New("scan failed"),
				},
				err: errors.New("rows failed"),
			}, nil
		}
		marked := 0
		worker.execFn = func(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
			marked++
			return pgconn.NewCommandTag("UPDATE 1"), nil
		}
		if err := worker.processDirectly(ctx); err == nil || err.Error() != "rows failed" {
			t.Fatalf("expected rows error, got %v", err)
		}
		if marked != 1 {
			t.Fatalf("expected one markDelivered call, got %d", marked)
		}
	})

	t.Run("processDirectly query error", func(t *testing.T) {
		worker := NewEventProcessorWorker(EventProcessorConfig{Subscriber: "sub"})
		worker.queryFn = func(context.Context, string, ...interface{}) (pgx.Rows, error) {
			return nil, errors.New("query failed")
		}
		if err := worker.processDirectly(ctx); err == nil || err.Error() != "query failed" {
			t.Fatalf("expected query error, got %v", err)
		}
	})

	t.Run("markDelivered tolerates exec error", func(t *testing.T) {
		worker := NewEventProcessorWorker(EventProcessorConfig{Subscriber: "sub"})
		worker.execFn = func(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, errors.New("update failed")
		}
		worker.markDelivered(ctx, uuid.New())
	})

	t.Run("cleanup and counters", func(t *testing.T) {
		worker := NewEventProcessorWorker(EventProcessorConfig{Subscriber: "sub"})
		worker.execFn = func(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("DELETE 2"), nil
		}
		deleted, err := worker.CleanupOldEvents(ctx, 48*time.Hour)
		if err != nil || deleted != 2 {
			t.Fatalf("CleanupOldEvents returned deleted=%d err=%v", deleted, err)
		}

		worker.queryRowFn = func(context.Context, string, ...interface{}) pgx.Row {
			return dbPathRow{values: []interface{}{int64(7)}}
		}
		pending, err := worker.GetPendingCount(ctx)
		if err != nil || pending != 7 {
			t.Fatalf("GetPendingCount returned count=%d err=%v", pending, err)
		}
		dead, err := worker.GetDeadLetteredCount(ctx)
		if err != nil || dead != 7 {
			t.Fatalf("GetDeadLetteredCount returned count=%d err=%v", dead, err)
		}
	})

	t.Run("counter query errors", func(t *testing.T) {
		worker := NewEventProcessorWorker(EventProcessorConfig{Subscriber: "sub"})
		worker.queryRowFn = func(context.Context, string, ...interface{}) pgx.Row {
			return dbPathRow{err: errors.New("scan err")}
		}
		if _, err := worker.GetPendingCount(ctx); err == nil || err.Error() != "scan err" {
			t.Fatalf("expected pending count scan error, got %v", err)
		}
		if _, err := worker.GetDeadLetteredCount(ctx); err == nil || err.Error() != "scan err" {
			t.Fatalf("expected dead-letter count scan error, got %v", err)
		}
	})
}

func TestCourierDispatchWorker_DBSeams(t *testing.T) {
	ctx := context.Background()

	t.Run("dispatch with courier path", func(t *testing.T) {
		worker := NewCourierDispatchWorker(CourierDispatchConfig{
			Interval:  time.Second,
			BatchSize: 5,
		})
		if err := worker.dispatch(ctx); err == nil || err != errWorkerDBUnavailable {
			t.Fatalf("expected direct-path DB unavailable without courier, got %v", err)
		}
	})

	t.Run("processDirectly handles retry and abandon branches", func(t *testing.T) {
		worker := NewCourierDispatchWorker(CourierDispatchConfig{
			Interval:   time.Second,
			BatchSize:  10,
			MaxRetries: 2,
		})
		worker.queryFn = func(context.Context, string, ...interface{}) (pgx.Rows, error) {
			return &dbPathRows{
				rows: [][]interface{}{
					{"m1", "email", "a@example.com", "subject", "body", 0}, // queued retry path
					{"m2", "email", "b@example.com", "subject", "body", 1}, // abandon path
				},
			}, nil
		}
		execCalls := 0
		worker.execFn = func(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
			execCalls++
			return pgconn.NewCommandTag("UPDATE 1"), nil
		}
		if err := worker.processDirectly(ctx); err != nil {
			t.Fatalf("processDirectly returned error: %v", err)
		}
		if execCalls != 2 {
			t.Fatalf("expected two status update exec calls, got %d", execCalls)
		}
	})

	t.Run("processDirectly query and rows errors", func(t *testing.T) {
		worker := NewCourierDispatchWorker(CourierDispatchConfig{BatchSize: 5, MaxRetries: 3})
		worker.queryFn = func(context.Context, string, ...interface{}) (pgx.Rows, error) {
			return nil, errors.New("query failed")
		}
		if err := worker.processDirectly(ctx); err == nil || err.Error() != "query failed" {
			t.Fatalf("expected query failure, got %v", err)
		}

		worker.queryFn = func(context.Context, string, ...interface{}) (pgx.Rows, error) {
			return &dbPathRows{
				rows: [][]interface{}{{"m1", "email", "a@example.com", "subject", "body", 0}},
				err:  errors.New("rows failed"),
			}, nil
		}
		if err := worker.processDirectly(ctx); err == nil || err.Error() != "rows failed" {
			t.Fatalf("expected rows failure, got %v", err)
		}
	})

	t.Run("processDirectly scan error continues", func(t *testing.T) {
		worker := NewCourierDispatchWorker(CourierDispatchConfig{BatchSize: 5, MaxRetries: 3})
		worker.queryFn = func(context.Context, string, ...interface{}) (pgx.Rows, error) {
			return &dbPathRows{
				rows: [][]interface{}{{"m1", "email", "a@example.com", "subject", "body", 0}},
				scanErr: map[int]error{
					0: errors.New("scan failed"),
				},
			}, nil
		}
		if err := worker.processDirectly(ctx); err != nil {
			t.Fatalf("scan errors should be tolerated in loop, got %v", err)
		}
	})

	t.Run("cleanup old messages", func(t *testing.T) {
		worker := NewCourierDispatchWorker(CourierDispatchConfig{BatchSize: 5, MaxRetries: 3})
		worker.execFn = func(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("DELETE 4"), nil
		}
		deleted, err := worker.CleanupOldMessages(ctx, 72*time.Hour)
		if err != nil || deleted != 4 {
			t.Fatalf("CleanupOldMessages returned deleted=%d err=%v", deleted, err)
		}
	})
}

func TestEventProcessorWorker_DBPath_JSONBranches(t *testing.T) {
	ctx := context.Background()
	worker := NewEventProcessorWorker(EventProcessorConfig{
		Interval:   time.Second,
		Subscriber: "sub",
		BatchSize:  10,
	})

	payload, _ := json.Marshal(map[string]interface{}{"ok": true})
	worker.queryFn = func(context.Context, string, ...interface{}) (pgx.Rows, error) {
		return &dbPathRows{
			rows: [][]interface{}{
				{uuid.New(), uuid.New(), 0, "evt", "src", nil, nil, nil, payload, []byte(`{"meta":`), time.Now()},
			},
		}, nil
	}
	calls := 0
	worker.execFn = func(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
		calls++
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	if err := worker.processDirectly(ctx); err != nil {
		t.Fatalf("processDirectly returned error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected markDelivered to be called once, got %d", calls)
	}
}
