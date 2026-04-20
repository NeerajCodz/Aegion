package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type passkeysFakeDB struct {
	queryRowFn func(context.Context, string, ...any) pgx.Row
	queryFn    func(context.Context, string, ...any) (pgx.Rows, error)
	execFn     func(context.Context, string, ...any) (pgconn.CommandTag, error)
	beginFn    func(context.Context) (pgx.Tx, error)
}

func (f *passkeysFakeDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if f.queryRowFn != nil {
		return f.queryRowFn(ctx, sql, args...)
	}
	return passkeysFakeRow{err: pgx.ErrNoRows}
}
func (f *passkeysFakeDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if f.queryFn != nil {
		return f.queryFn(ctx, sql, args...)
	}
	return &passkeysFakeRows{}, nil
}
func (f *passkeysFakeDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if f.execFn != nil {
		return f.execFn(ctx, sql, args...)
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}
func (f *passkeysFakeDB) Begin(ctx context.Context) (pgx.Tx, error) {
	if f.beginFn != nil {
		return f.beginFn(ctx)
	}
	return &passkeysFakeTx{}, nil
}

type passkeysFakeTx struct {
	queryRowFn func(context.Context, string, ...any) pgx.Row
	execFn     func(context.Context, string, ...any) (pgconn.CommandTag, error)
	commitFn   func(context.Context) error
}

func (t *passkeysFakeTx) Begin(context.Context) (pgx.Tx, error) { return nil, errors.New("not implemented") }
func (t *passkeysFakeTx) Rollback(context.Context) error        { return nil }
func (t *passkeysFakeTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, errors.New("not implemented")
}
func (t *passkeysFakeTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults { return nil }
func (t *passkeysFakeTx) LargeObjects() pgx.LargeObjects                          { return pgx.LargeObjects{} }
func (t *passkeysFakeTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, errors.New("not implemented")
}
func (t *passkeysFakeTx) Query(context.Context, string, ...any) (pgx.Rows, error) { return &passkeysFakeRows{}, nil }
func (t *passkeysFakeTx) Conn() *pgx.Conn                                         { return nil }
func (t *passkeysFakeTx) Commit(ctx context.Context) error {
	if t.commitFn != nil {
		return t.commitFn(ctx)
	}
	return nil
}
func (t *passkeysFakeTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if t.execFn != nil {
		return t.execFn(ctx, sql, args...)
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}
func (t *passkeysFakeTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if t.queryRowFn != nil {
		return t.queryRowFn(ctx, sql, args...)
	}
	return passkeysFakeRow{err: pgx.ErrNoRows}
}

type passkeysFakeRow struct {
	vals []any
	err  error
}

func (r passkeysFakeRow) Scan(dest ...any) error {
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
		case *uint32:
			v, ok := r.vals[i].(uint32)
			if !ok {
				return fmt.Errorf("expected uint32, got %T", r.vals[i])
			}
			*d = v
		case *time.Time:
			v, ok := r.vals[i].(time.Time)
			if !ok {
				return fmt.Errorf("expected time, got %T", r.vals[i])
			}
			*d = v
		default:
			return fmt.Errorf("unsupported scan dest %T", dest[i])
		}
	}
	return nil
}

type passkeysFakeRows struct {
	data [][]any
	idx  int
	err  error
}

func (r *passkeysFakeRows) Close() {}
func (r *passkeysFakeRows) Err() error { return r.err }
func (r *passkeysFakeRows) CommandTag() pgconn.CommandTag { return pgconn.NewCommandTag("SELECT 0") }
func (r *passkeysFakeRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *passkeysFakeRows) Next() bool {
	if r.idx >= len(r.data) {
		return false
	}
	r.idx++
	return true
}
func (r *passkeysFakeRows) Scan(dest ...any) error {
	if r.idx == 0 || r.idx > len(r.data) {
		return errors.New("scan without row")
	}
	return passkeysFakeRow{vals: r.data[r.idx-1]}.Scan(dest...)
}
func (r *passkeysFakeRows) Values() ([]any, error) {
	if r.idx == 0 || r.idx > len(r.data) {
		return nil, errors.New("values without row")
	}
	return r.data[r.idx-1], nil
}
func (r *passkeysFakeRows) RawValues() [][]byte { return nil }
func (r *passkeysFakeRows) Conn() *pgx.Conn     { return nil }

func TestPostgresStorePasskeysStubCoverage(t *testing.T) {
	now := time.Now().UTC()
	s := &PostgresStore{pool: &passkeysFakeDB{beginFn: func(context.Context) (pgx.Tx, error) {
		return nil, errors.New("begin failed")
	}}}
	if _, err := s.ConsumeChallenge("c1"); err == nil || err.Error() != "begin failed" {
		t.Fatalf("ConsumeChallenge(begin error) = %v", err)
	}

	s.pool = &passkeysFakeDB{beginFn: func(context.Context) (pgx.Tx, error) {
		return &passkeysFakeTx{
			queryRowFn: func(context.Context, string, ...any) pgx.Row { return passkeysFakeRow{err: pgx.ErrNoRows} },
		}, nil
	}}
	if _, err := s.ConsumeChallenge("c1"); !errors.Is(err, ErrChallengeNotFound) {
		t.Fatalf("ConsumeChallenge(not found) = %v", err)
	}

	s.pool = &passkeysFakeDB{beginFn: func(context.Context) (pgx.Tx, error) {
		return &passkeysFakeTx{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return passkeysFakeRow{vals: []any{"c1", "i1", "login", now.Add(time.Hour)}}
			},
			execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("delete failed")
			},
		}, nil
	}}
	if _, err := s.ConsumeChallenge("c1"); err == nil || err.Error() != "delete failed" {
		t.Fatalf("ConsumeChallenge(delete error) = %v", err)
	}

	s.pool = &passkeysFakeDB{beginFn: func(context.Context) (pgx.Tx, error) {
		return &passkeysFakeTx{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return passkeysFakeRow{vals: []any{"c1", "i1", "login", now.Add(time.Hour)}}
			},
			execFn:   func(context.Context, string, ...any) (pgconn.CommandTag, error) { return pgconn.NewCommandTag("DELETE 1"), nil },
			commitFn: func(context.Context) error { return errors.New("commit failed") },
		}, nil
	}}
	if _, err := s.ConsumeChallenge("c1"); err == nil || err.Error() != "commit failed" {
		t.Fatalf("ConsumeChallenge(commit error) = %v", err)
	}

	s.pool = &passkeysFakeDB{beginFn: func(context.Context) (pgx.Tx, error) {
		return &passkeysFakeTx{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return passkeysFakeRow{vals: []any{"c1", "i1", "login", now.Add(-time.Minute)}}
			},
			execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) { return pgconn.NewCommandTag("DELETE 1"), nil },
		}, nil
	}}
	if _, err := s.ConsumeChallenge("c1"); !errors.Is(err, ErrChallengeExpired) {
		t.Fatalf("ConsumeChallenge(expired) = %v", err)
	}

	s.pool = &passkeysFakeDB{queryRowFn: func(context.Context, string, ...any) pgx.Row { return passkeysFakeRow{err: pgx.ErrNoRows} }}
	if _, err := s.GetCredential("cred-1"); !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("GetCredential(not found) = %v", err)
	}

	s.pool = &passkeysFakeDB{execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) { return pgconn.NewCommandTag("UPDATE 0"), nil }}
	if err := s.UpdateCredentialSignCount("cred-1", 1); !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("UpdateCredentialSignCount(not found) = %v", err)
	}

	s.pool = &passkeysFakeDB{queryFn: func(context.Context, string, ...any) (pgx.Rows, error) { return nil, errors.New("query failed") }}
	if got := s.ListCredentialsByIdentity("i1"); len(got) != 0 {
		t.Fatalf("ListCredentialsByIdentity(query error) len=%d", len(got))
	}

	s.pool = &passkeysFakeDB{queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
		return &passkeysFakeRows{data: [][]any{{"bad"}}}, nil
	}}
	if got := s.ListCredentialsByIdentity("i1"); len(got) != 0 {
		t.Fatalf("ListCredentialsByIdentity(scan error) len=%d", len(got))
	}

	s.pool = &passkeysFakeDB{queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
		return &passkeysFakeRows{data: [][]any{{"cred-1", "i1", "pk", uint32(7), now}}}, nil
	}}
	if got := s.ListCredentialsByIdentity("i1"); len(got) != 1 || got[0].ID != "cred-1" {
		t.Fatalf("ListCredentialsByIdentity(success) got=%#v", got)
	}
}

