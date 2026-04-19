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

type mfaFakeDB struct {
	queryRowFn func(context.Context, string, ...any) pgx.Row
	queryFn    func(context.Context, string, ...any) (pgx.Rows, error)
	execFn     func(context.Context, string, ...any) (pgconn.CommandTag, error)
	beginFn    func(context.Context) (pgx.Tx, error)
}

func (f *mfaFakeDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if f.queryRowFn != nil {
		return f.queryRowFn(ctx, sql, args...)
	}
	return mfaFakeRow{err: pgx.ErrNoRows}
}

func (f *mfaFakeDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if f.queryFn != nil {
		return f.queryFn(ctx, sql, args...)
	}
	return &mfaFakeRows{}, nil
}

func (f *mfaFakeDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if f.execFn != nil {
		return f.execFn(ctx, sql, args...)
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (f *mfaFakeDB) Begin(ctx context.Context) (pgx.Tx, error) {
	if f.beginFn != nil {
		return f.beginFn(ctx)
	}
	return &mfaFakeTx{}, nil
}

type mfaFakeTx struct {
	execFn    func(context.Context, string, ...any) (pgconn.CommandTag, error)
	queryRowFn func(context.Context, string, ...any) pgx.Row
	commitFn  func(context.Context) error
}

func (t *mfaFakeTx) Begin(context.Context) (pgx.Tx, error) { return nil, errors.New("not implemented") }
func (t *mfaFakeTx) Rollback(context.Context) error        { return nil }
func (t *mfaFakeTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, errors.New("not implemented")
}
func (t *mfaFakeTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults { return nil }
func (t *mfaFakeTx) LargeObjects() pgx.LargeObjects                          { return pgx.LargeObjects{} }
func (t *mfaFakeTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, errors.New("not implemented")
}
func (t *mfaFakeTx) Query(context.Context, string, ...any) (pgx.Rows, error) { return &mfaFakeRows{}, nil }
func (t *mfaFakeTx) Conn() *pgx.Conn                                         { return nil }
func (t *mfaFakeTx) Commit(ctx context.Context) error {
	if t.commitFn != nil {
		return t.commitFn(ctx)
	}
	return nil
}
func (t *mfaFakeTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if t.execFn != nil {
		return t.execFn(ctx, sql, args...)
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}
func (t *mfaFakeTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if t.queryRowFn != nil {
		return t.queryRowFn(ctx, sql, args...)
	}
	return mfaFakeRow{err: pgx.ErrNoRows}
}

type mfaFakeRow struct {
	vals []any
	err  error
}

func (r mfaFakeRow) Scan(dest ...any) error {
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
		case *time.Time:
			v, ok := r.vals[i].(time.Time)
			if !ok {
				return fmt.Errorf("expected time, got %T", r.vals[i])
			}
			*d = v
		case **time.Time:
			if r.vals[i] == nil {
				*d = nil
				continue
			}
			v, ok := r.vals[i].(time.Time)
			if !ok {
				return fmt.Errorf("expected *time, got %T", r.vals[i])
			}
			cp := v
			*d = &cp
		default:
			return fmt.Errorf("unsupported scan dest %T", dest[i])
		}
	}
	return nil
}

type mfaFakeRows struct {
	data [][]any
	idx  int
	err  error
}

func (r *mfaFakeRows) Close() {}
func (r *mfaFakeRows) Err() error {
	return r.err
}
func (r *mfaFakeRows) CommandTag() pgconn.CommandTag            { return pgconn.NewCommandTag("SELECT 0") }
func (r *mfaFakeRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *mfaFakeRows) Next() bool {
	if r.idx >= len(r.data) {
		return false
	}
	r.idx++
	return true
}
func (r *mfaFakeRows) Scan(dest ...any) error {
	if r.idx == 0 || r.idx > len(r.data) {
		return errors.New("scan called without row")
	}
	return mfaFakeRow{vals: r.data[r.idx-1]}.Scan(dest...)
}
func (r *mfaFakeRows) Values() ([]any, error) {
	if r.idx == 0 || r.idx > len(r.data) {
		return nil, errors.New("values without row")
	}
	return r.data[r.idx-1], nil
}
func (r *mfaFakeRows) RawValues() [][]byte { return nil }
func (r *mfaFakeRows) Conn() *pgx.Conn     { return nil }

func TestPostgresStoreStubCoverageBranches(t *testing.T) {
	now := time.Now().UTC()
	s := &PostgresStore{pool: &mfaFakeDB{}}

	if _, err := s.GetEnrollment("e1"); !errors.Is(err, ErrEnrollmentNotFound) {
		t.Fatalf("GetEnrollment(not found) = %v", err)
	}
	if _, err := s.GetTOTPFactor("i1"); !errors.Is(err, ErrTOTPFactorNotFound) {
		t.Fatalf("GetTOTPFactor(not found) = %v", err)
	}
	if _, err := s.GetTrustedDevice("i1", "hash", "pre"); !errors.Is(err, ErrTrustedDeviceNotFound) {
		t.Fatalf("GetTrustedDevice(not found) = %v", err)
	}

	s.pool = &mfaFakeDB{execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}}
	if err := s.UpdateTOTPLastUsed("i1", now); !errors.Is(err, ErrTOTPFactorNotFound) {
		t.Fatalf("UpdateTOTPLastUsed(not found) = %v", err)
	}
	if err := s.MarkBackupCodeUsed("i1", "b1", now); !errors.Is(err, ErrBackupCodeNotFound) {
		t.Fatalf("MarkBackupCodeUsed(not found) = %v", err)
	}
	if err := s.TouchTrustedDevice("i1", "d1", now); !errors.Is(err, ErrTrustedDeviceNotFound) {
		t.Fatalf("TouchTrustedDevice(not found) = %v", err)
	}
	if err := s.DeleteTrustedDevice("i1", "d1", now); !errors.Is(err, ErrTrustedDeviceNotFound) {
		t.Fatalf("DeleteTrustedDevice(not found) = %v", err)
	}

	s.pool = &mfaFakeDB{beginFn: func(context.Context) (pgx.Tx, error) {
		return nil, errors.New("begin failed")
	}}
	if err := s.ReplaceBackupCodes("i1", nil); err == nil || err.Error() != "begin failed" {
		t.Fatalf("ReplaceBackupCodes(begin error) = %v", err)
	}
	if err := s.DeleteAllIdentityData("i1"); err == nil || err.Error() != "begin failed" {
		t.Fatalf("DeleteAllIdentityData(begin error) = %v", err)
	}

	s.pool = &mfaFakeDB{beginFn: func(context.Context) (pgx.Tx, error) {
		return &mfaFakeTx{
			execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("exec failed")
			},
		}, nil
	}}
	if err := s.ReplaceBackupCodes("i1", []BackupCode{{ID: "b1", IdentityID: "i1", CodeHash: "h", BatchID: "batch", CreatedAt: now}}); err == nil || err.Error() != "exec failed" {
		t.Fatalf("ReplaceBackupCodes(exec error) = %v", err)
	}
	if err := s.DeleteAllIdentityData("i1"); err == nil || err.Error() != "exec failed" {
		t.Fatalf("DeleteAllIdentityData(exec error) = %v", err)
	}

	s.pool = &mfaFakeDB{beginFn: func(context.Context) (pgx.Tx, error) {
		return &mfaFakeTx{
			execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("UPDATE 1"), nil
			},
			commitFn: func(context.Context) error { return errors.New("commit failed") },
		}, nil
	}}
	if err := s.ReplaceBackupCodes("i1", nil); err == nil || err.Error() != "commit failed" {
		t.Fatalf("ReplaceBackupCodes(commit error) = %v", err)
	}
	if err := s.DeleteAllIdentityData("i1"); err == nil || err.Error() != "commit failed" {
		t.Fatalf("DeleteAllIdentityData(commit error) = %v", err)
	}

	s.pool = &mfaFakeDB{
		queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
			return &mfaFakeRows{data: [][]any{{"bad"}}}, nil
		},
	}
	if _, err := s.ListBackupCodes("i1"); err == nil {
		t.Fatal("ListBackupCodes(scan error) expected error")
	}
	if _, err := s.ListFactorsByIdentity("i1"); err == nil {
		t.Fatal("ListFactorsByIdentity(scan error) expected error")
	}

	s.pool = &mfaFakeDB{
		queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
			return &mfaFakeRows{err: errors.New("rows failed")}, nil
		},
	}
	if _, err := s.ListBackupCodes("i1"); err == nil || err.Error() != "rows failed" {
		t.Fatalf("ListBackupCodes(rows error) = %v", err)
	}
	if _, err := s.ListFactorsByIdentity("i1"); err == nil || err.Error() != "rows failed" {
		t.Fatalf("ListFactorsByIdentity(rows error) = %v", err)
	}

	s.pool = &mfaFakeDB{
		queryFn: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			if strings.Contains(sql, "FROM mfa_backup_codes") {
				return &mfaFakeRows{data: [][]any{{"b1", "i1", "h", "batch", nil, now}}}, nil
			}
			return &mfaFakeRows{data: [][]any{{"i1", now, now}}}, nil
		},
	}
	if codes, err := s.ListBackupCodes("i1"); err != nil || len(codes) != 1 {
		t.Fatalf("ListBackupCodes(success) len=%d err=%v", len(codes), err)
	}
	if factors, err := s.ListFactorsByIdentity("i1"); err != nil || len(factors) != 1 || factors[0].ID != "i1:totp" {
		t.Fatalf("ListFactorsByIdentity(success) factors=%#v err=%v", factors, err)
	}
}

