package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type ssoFakeDB struct {
	queryRowFn func(context.Context, string, ...any) pgx.Row
	queryFn    func(context.Context, string, ...any) (pgx.Rows, error)
	execFn     func(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func (f *ssoFakeDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if f.queryRowFn != nil {
		return f.queryRowFn(ctx, sql, args...)
	}
	return ssoFakeRow{err: pgx.ErrNoRows}
}
func (f *ssoFakeDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if f.queryFn != nil {
		return f.queryFn(ctx, sql, args...)
	}
	return &ssoFakeRows{}, nil
}
func (f *ssoFakeDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if f.execFn != nil {
		return f.execFn(ctx, sql, args...)
	}
	return pgconn.NewCommandTag("DELETE 1"), nil
}

type ssoFakeRow struct {
	vals []any
	err  error
}

func assignValue(dest any, src any) error {
	dv := reflect.ValueOf(dest)
	if dv.Kind() != reflect.Ptr || dv.IsNil() {
		return fmt.Errorf("destination must be pointer, got %T", dest)
	}
	ev := dv.Elem()
	if src == nil {
		ev.Set(reflect.Zero(ev.Type()))
		return nil
	}
	sv := reflect.ValueOf(src)
	if sv.Type().AssignableTo(ev.Type()) {
		ev.Set(sv)
		return nil
	}
	if sv.Type().ConvertibleTo(ev.Type()) {
		ev.Set(sv.Convert(ev.Type()))
		return nil
	}
	return fmt.Errorf("cannot assign %T to %T", src, dest)
}

func (r ssoFakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.vals) {
		return fmt.Errorf("scan mismatch %d != %d", len(dest), len(r.vals))
	}
	for i := range dest {
		if err := assignValue(dest[i], r.vals[i]); err != nil {
			return err
		}
	}
	return nil
}

type ssoFakeRows struct {
	data [][]any
	idx  int
	err  error
}

func (r *ssoFakeRows) Close()                                       {}
func (r *ssoFakeRows) Err() error                                   { return r.err }
func (r *ssoFakeRows) CommandTag() pgconn.CommandTag                { return pgconn.NewCommandTag("SELECT 0") }
func (r *ssoFakeRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *ssoFakeRows) Next() bool {
	if r.idx >= len(r.data) {
		return false
	}
	r.idx++
	return true
}
func (r *ssoFakeRows) Scan(dest ...any) error {
	if r.idx == 0 || r.idx > len(r.data) {
		return errors.New("scan without row")
	}
	return ssoFakeRow{vals: r.data[r.idx-1]}.Scan(dest...)
}
func (r *ssoFakeRows) Values() ([]any, error) { return nil, errors.New("not implemented") }
func (r *ssoFakeRows) RawValues() [][]byte    { return nil }
func (r *ssoFakeRows) Conn() *pgx.Conn        { return nil }

func ssoConnectionRow(now time.Time, id uuid.UUID) []any {
	return []any{
		id,
		"acme",
		"Acme",
		"urn:acme:idp",
		"https://idp.example.com/sso",
		"cert",
		"https://idp.example.com/metadata",
		[]byte(`["example.com"]`),
		[]byte(`{"subject":"sub","email":"email","display_name":"name"}`),
		true,
		"/dashboard",
		[]byte(`{"prompt":"login"}`),
		true,
		now,
		now,
	}
}

func TestPostgresStoreSSOStubCoverage(t *testing.T) {
	now := time.Now().UTC()
	id := uuid.New()
	s := &PostgresStore{pool: &ssoFakeDB{}}

	s.pool = &ssoFakeDB{queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
		return nil, errors.New("query failed")
	}}
	if _, err := s.ListConnections(context.Background(), true); err == nil || err.Error() != "query failed" {
		t.Fatalf("ListConnections(query error) = %v", err)
	}

	s.pool = &ssoFakeDB{queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
		return &ssoFakeRows{data: [][]any{{"bad"}}}, nil
	}}
	if _, err := s.ListConnections(context.Background(), false); err == nil {
		t.Fatal("ListConnections(scan error) expected error")
	}

	s.pool = &ssoFakeDB{queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
		return &ssoFakeRows{err: errors.New("rows failed")}, nil
	}}
	if _, err := s.ListConnections(context.Background(), true); err == nil || err.Error() != "rows failed" {
		t.Fatalf("ListConnections(rows err) = %v", err)
	}

	s.pool = &ssoFakeDB{queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
		return &ssoFakeRows{data: [][]any{ssoConnectionRow(now, id)}}, nil
	}}
	if list, err := s.ListConnections(context.Background(), true); err != nil || len(list) != 1 || list[0].Slug != "acme" {
		t.Fatalf("ListConnections(success) list=%#v err=%v", list, err)
	}

	s.pool = &ssoFakeDB{queryRowFn: func(context.Context, string, ...any) pgx.Row { return ssoFakeRow{err: pgx.ErrNoRows} }}
	if _, err := s.GetConnectionBySlug(context.Background(), "missing"); !errors.Is(err, ErrConnectionNotFound) {
		t.Fatalf("GetConnectionBySlug(not found) = %v", err)
	}
	if _, err := s.GetConnectionByDomain(context.Background(), "missing.example.com"); !errors.Is(err, ErrConnectionNotFound) {
		t.Fatalf("GetConnectionByDomain(not found) = %v", err)
	}

	s.pool = &ssoFakeDB{queryRowFn: func(context.Context, string, ...any) pgx.Row { return ssoFakeRow{vals: ssoConnectionRow(now, id)} }}
	if conn, err := s.GetConnectionBySlug(context.Background(), "acme"); err != nil || conn.ID != id {
		t.Fatalf("GetConnectionBySlug(success) conn=%#v err=%v", conn, err)
	}
	if conn, err := s.GetConnectionByDomain(context.Background(), "example.com"); err != nil || conn.ID != id {
		t.Fatalf("GetConnectionByDomain(success) conn=%#v err=%v", conn, err)
	}

	s.pool = &ssoFakeDB{queryRowFn: func(context.Context, string, ...any) pgx.Row { return ssoFakeRow{err: errors.New("upsert failed")} }}
	if _, err := s.UpsertConnection(context.Background(), Connection{Slug: "acme"}); err == nil || err.Error() != "upsert failed" {
		t.Fatalf("UpsertConnection(error) = %v", err)
	}

	s.pool = &ssoFakeDB{queryRowFn: func(context.Context, string, ...any) pgx.Row { return ssoFakeRow{vals: []any{id, now, now}} }}
	if conn, err := s.UpsertConnection(context.Background(), Connection{Slug: "acme", DisplayName: "Acme"}); err != nil || conn.ID != id {
		t.Fatalf("UpsertConnection(success) conn=%#v err=%v", conn, err)
	}

	s.pool = &ssoFakeDB{execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, errors.New("delete failed")
	}}
	if err := s.DeleteConnection(context.Background(), "acme"); err == nil || err.Error() != "delete failed" {
		t.Fatalf("DeleteConnection(exec error) = %v", err)
	}
	s.pool = &ssoFakeDB{execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
		return pgconn.NewCommandTag("DELETE 0"), nil
	}}
	if err := s.DeleteConnection(context.Background(), "acme"); !errors.Is(err, ErrConnectionNotFound) {
		t.Fatalf("DeleteConnection(not found) = %v", err)
	}
	s.pool = &ssoFakeDB{execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
		return pgconn.NewCommandTag("DELETE 1"), nil
	}}
	if err := s.DeleteConnection(context.Background(), "acme"); err != nil {
		t.Fatalf("DeleteConnection(success) = %v", err)
	}

	if _, err := scanConnection(ssoFakeRow{vals: []any{
		id, "acme", "Acme", "ent", "sso", "cert", "meta", []byte(`{`), []byte(`{}`), true, "", []byte(`{}`), true, now, now,
	}}); err == nil {
		t.Fatal("scanConnection(domains json error) expected error")
	}
	if _, err := scanConnection(ssoFakeRow{vals: []any{
		id, "acme", "Acme", "ent", "sso", "cert", "meta", []byte(`[]`), []byte(`{`), true, "", []byte(`{}`), true, now, now,
	}}); err == nil {
		t.Fatal("scanConnection(mapping json error) expected error")
	}
	if _, err := scanConnection(ssoFakeRow{vals: []any{
		id, "acme", "Acme", "ent", "sso", "cert", "meta", []byte(`[]`), []byte(`{}`), true, "", []byte(`{`), true, now, now,
	}}); err == nil {
		t.Fatal("scanConnection(extra json error) expected error")
	}
}
