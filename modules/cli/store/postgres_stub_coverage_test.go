package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type cliFakeDB struct {
	queryRowFn func(context.Context, string, ...any) pgx.Row
	queryFn    func(context.Context, string, ...any) (pgx.Rows, error)
	execFn     func(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func (f *cliFakeDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if f.queryRowFn != nil {
		return f.queryRowFn(ctx, sql, args...)
	}
	return cliFakeRow{err: pgx.ErrNoRows}
}
func (f *cliFakeDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if f.queryFn != nil {
		return f.queryFn(ctx, sql, args...)
	}
	return &cliFakeRows{}, nil
}
func (f *cliFakeDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if f.execFn != nil {
		return f.execFn(ctx, sql, args...)
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

type cliFakeRow struct {
	vals []any
	err  error
}

func (r cliFakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.vals) {
		return fmt.Errorf("scan mismatch: %d != %d", len(dest), len(r.vals))
	}
	for i := range dest {
		switch d := dest[i].(type) {
		case *string:
			v, ok := r.vals[i].(string)
			if !ok {
				return fmt.Errorf("expected string, got %T", r.vals[i])
			}
			*d = v
		case *[]byte:
			v, ok := r.vals[i].([]byte)
			if !ok {
				return fmt.Errorf("expected bytes, got %T", r.vals[i])
			}
			*d = append((*d)[:0], v...)
		case *bool:
			v, ok := r.vals[i].(bool)
			if !ok {
				return fmt.Errorf("expected bool, got %T", r.vals[i])
			}
			*d = v
		case *time.Time:
			v, ok := r.vals[i].(time.Time)
			if !ok {
				return fmt.Errorf("expected time, got %T", r.vals[i])
			}
			*d = v
		case *int64:
			v, ok := r.vals[i].(int64)
			if !ok {
				return fmt.Errorf("expected int64, got %T", r.vals[i])
			}
			*d = v
		default:
			return fmt.Errorf("unsupported scan dest %T", dest[i])
		}
	}
	return nil
}

type cliFakeRows struct {
	data [][]any
	idx  int
	err  error
}

func (r *cliFakeRows) Close()                                       {}
func (r *cliFakeRows) Err() error                                   { return r.err }
func (r *cliFakeRows) CommandTag() pgconn.CommandTag                { return pgconn.NewCommandTag("SELECT 0") }
func (r *cliFakeRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *cliFakeRows) Next() bool {
	if r.idx >= len(r.data) {
		return false
	}
	r.idx++
	return true
}
func (r *cliFakeRows) Scan(dest ...any) error {
	if r.idx == 0 || r.idx > len(r.data) {
		return errors.New("scan without row")
	}
	return cliFakeRow{vals: r.data[r.idx-1]}.Scan(dest...)
}
func (r *cliFakeRows) Values() ([]any, error) {
	if r.idx == 0 || r.idx > len(r.data) {
		return nil, errors.New("values without row")
	}
	return r.data[r.idx-1], nil
}
func (r *cliFakeRows) RawValues() [][]byte { return nil }
func (r *cliFakeRows) Conn() *pgx.Conn     { return nil }

func TestPostgresStoreCLIStubCoverage(t *testing.T) {
	now := time.Now().UTC()
	s := &PostgresStore{pool: &cliFakeDB{}}

	if err := s.SaveRun(context.Background(), CommandRun{Arguments: map[string]any{"bad": make(chan int)}}); err == nil {
		t.Fatal("SaveRun(arguments marshal error) expected error")
	}
	if err := s.SaveRun(context.Background(), CommandRun{
		Arguments: map[string]any{"ok": true},
		Result:    map[string]any{"bad": make(chan int)},
	}); err == nil {
		t.Fatal("SaveRun(result marshal error) expected error")
	}

	s.pool = &cliFakeDB{queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
		return nil, errors.New("query failed")
	}}
	if _, err := s.ListRuns(context.Background(), 0); err == nil || err.Error() != "query failed" {
		t.Fatalf("ListRuns(query error) = %v", err)
	}

	s.pool = &cliFakeDB{queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
		return &cliFakeRows{data: [][]any{{"bad"}}}, nil
	}}
	if _, err := s.ListRuns(context.Background(), 10); err == nil {
		t.Fatal("ListRuns(scan error) expected error")
	}

	s.pool = &cliFakeDB{queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
		return &cliFakeRows{err: errors.New("rows failed")}, nil
	}}
	if _, err := s.ListRuns(context.Background(), 10); err == nil || err.Error() != "rows failed" {
		t.Fatalf("ListRuns(rows error) = %v", err)
	}

	s.pool = &cliFakeDB{queryRowFn: func(context.Context, string, ...any) pgx.Row { return cliFakeRow{err: pgx.ErrNoRows} }}
	if _, err := s.GetRun(context.Background(), "missing"); !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("GetRun(not found) = %v", err)
	}

	s.pool = &cliFakeDB{
		queryRowFn: func(_ context.Context, sql string, args ...any) pgx.Row {
			switch {
			case strings.Contains(sql, "to_regclass"):
				table := ""
				if len(args) > 0 {
					table, _ = args[0].(string)
				}
				return cliFakeRow{vals: []any{table == "public.core_courier_messages" || table == "public.core_system_config"}}
			case strings.Contains(sql, "WHERE status = $1"):
				status := args[0].(string)
				if status == "processing" {
					return cliFakeRow{err: errors.New("count failed")}
				}
				return cliFakeRow{vals: []any{int64(2)}}
			case strings.Contains(sql, "next_retry_at"):
				return cliFakeRow{vals: []any{int64(1)}}
			case strings.Contains(sql, "MAX(updated_at)"):
				return cliFakeRow{vals: []any{now}}
			case strings.Contains(sql, "core_system_config"):
				return cliFakeRow{vals: []any{[]byte(`{"enabled":true}`)}}
			case strings.Contains(sql, "COUNT(*)"):
				return cliFakeRow{err: errors.New("count error")}
			default:
				return cliFakeRow{vals: []any{int64(0)}}
			}
		},
		queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
			return &cliFakeRows{data: [][]any{{
				"run-1", "status.summary", []byte(`{"limit":5}`), []byte(`{"ok":true}`), true, "", now,
			}}}, nil
		},
	}
	if runs, err := s.ListRuns(context.Background(), 10); err != nil || len(runs) != 1 {
		t.Fatalf("ListRuns(success) len=%d err=%v", len(runs), err)
	}
	if summary, err := s.CourierSummary(context.Background()); err != nil || summary["queued"] == nil || summary["processing"] != nil {
		t.Fatalf("CourierSummary(summary branch) summary=%#v err=%v", summary, err)
	}
	if got := s.countIfExists(context.Background(), "core_identities", "SELECT COUNT(*) FROM core_identities"); got != 0 {
		t.Fatalf("countIfExists(query error) = %d, want 0", got)
	}
	if cfg := s.systemConfig(context.Background(), "policy.settings"); cfg["enabled"] != true {
		t.Fatalf("systemConfig(success) = %#v", cfg)
	}

	s.pool = &cliFakeDB{
		queryRowFn: func(_ context.Context, sql string, args ...any) pgx.Row {
			if strings.Contains(sql, "to_regclass") {
				return cliFakeRow{vals: []any{true}}
			}
			return cliFakeRow{vals: []any{[]byte("{")}}
		},
	}
	if cfg := s.systemConfig(context.Background(), "policy.settings"); len(cfg) != 0 {
		t.Fatalf("systemConfig(invalid json) = %#v", cfg)
	}
}
