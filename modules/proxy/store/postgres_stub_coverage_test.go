package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type proxyFakeDB struct {
	queryRowFn func(context.Context, string, ...any) pgx.Row
	queryFn    func(context.Context, string, ...any) (pgx.Rows, error)
	execFn     func(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func (f *proxyFakeDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if f.queryRowFn != nil {
		return f.queryRowFn(ctx, sql, args...)
	}
	return proxyFakeRow{err: pgx.ErrNoRows}
}
func (f *proxyFakeDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if f.queryFn != nil {
		return f.queryFn(ctx, sql, args...)
	}
	return &proxyFakeRows{}, nil
}
func (f *proxyFakeDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if f.execFn != nil {
		return f.execFn(ctx, sql, args...)
	}
	return pgconn.NewCommandTag("DELETE 1"), nil
}

type proxyFakeRow struct {
	vals []any
	err  error
}

func (r proxyFakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.vals) {
		return fmt.Errorf("scan mismatch: %d != %d", len(dest), len(r.vals))
	}
	for i := range dest {
		switch d := dest[i].(type) {
		case *uuid.UUID:
			v, ok := r.vals[i].(uuid.UUID)
			if !ok {
				return fmt.Errorf("expected uuid, got %T", r.vals[i])
			}
			*d = v
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
		case *int:
			v, ok := r.vals[i].(int)
			if !ok {
				return fmt.Errorf("expected int, got %T", r.vals[i])
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

type proxyFakeRows struct {
	data [][]any
	idx  int
	err  error
}

func (r *proxyFakeRows) Close()                                       {}
func (r *proxyFakeRows) Err() error                                   { return r.err }
func (r *proxyFakeRows) CommandTag() pgconn.CommandTag                { return pgconn.NewCommandTag("SELECT 0") }
func (r *proxyFakeRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *proxyFakeRows) Next() bool                                   { r.idx++; return r.idx <= len(r.data) }
func (r *proxyFakeRows) RawValues() [][]byte                          { return nil }
func (r *proxyFakeRows) Conn() *pgx.Conn                              { return nil }
func (r *proxyFakeRows) Values() ([]any, error)                       { return nil, errors.New("not implemented") }
func (r *proxyFakeRows) Scan(dest ...any) error {
	return proxyFakeRow{vals: r.data[r.idx-1]}.Scan(dest...)
}

func TestPostgresStoreProxyStubCoverage(t *testing.T) {
	now := time.Now().UTC()
	upstreamID := uuid.New()

	s := &PostgresStore{pool: &proxyFakeDB{
		queryFn: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			if strings.Contains(sql, "proxy_upstreams") {
				return &proxyFakeRows{data: [][]any{{
					upstreamID, "api", "https://api.example.com", "/healthz", "READY", "5s", 10,
					[]byte(`{"x-env":"prod"}`), []byte(`{"failure_threshold":5}`), true, now, now,
				}}}, nil
			}
			return &proxyFakeRows{data: [][]any{{
				"route-1", "/v1/profile", []byte(`["GET"]`), true, "aal2", []byte(`["profile:read"]`),
				[]byte(`{"requests_per_second":10}`), "api", 100, []byte(`{"x-test":"1"}`),
				[]byte(`{"strip_prefix":"/v1"}`), true, "profile route", now, now,
			}}}, nil
		},
		queryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
			switch {
			case strings.Contains(sql, "FROM proxy_upstreams") && strings.Contains(sql, "WHERE name"):
				return proxyFakeRow{vals: []any{
					upstreamID, "api", "https://api.example.com", "/healthz", "READY", "5s", 10,
					[]byte(`{"x-env":"prod"}`), []byte(`{"failure_threshold":5}`), true, now, now,
				}}
			case strings.Contains(sql, "INSERT INTO proxy_upstreams"):
				return proxyFakeRow{vals: []any{upstreamID, now, now}}
			case strings.Contains(sql, "SELECT COUNT(*) FROM proxy_routes"):
				return proxyFakeRow{vals: []any{0}}
			case strings.Contains(sql, "FROM proxy_routes") && strings.Contains(sql, "WHERE id"):
				return proxyFakeRow{vals: []any{
					"route-1", "/v1/profile", []byte(`["GET"]`), true, "aal2", []byte(`["profile:read"]`),
					[]byte(`{"requests_per_second":10}`), "api", 100, []byte(`{"x-test":"1"}`),
					[]byte(`{"strip_prefix":"/v1"}`), true, "profile route", now, now,
				}}
			case strings.Contains(sql, "INSERT INTO proxy_routes"):
				return proxyFakeRow{vals: []any{now, now}}
			default:
				return proxyFakeRow{err: pgx.ErrNoRows}
			}
		},
	}}

	if ups, err := s.ListUpstreams(context.Background()); err != nil || len(ups) != 1 || ups[0].Name != "api" {
		t.Fatalf("ListUpstreams(success) len=%d err=%v", len(ups), err)
	}
	if up, err := s.GetUpstreamByName(context.Background(), "api"); err != nil || up.Name != "api" {
		t.Fatalf("GetUpstreamByName(success) up=%#v err=%v", up, err)
	}
	if up, err := s.UpsertUpstream(context.Background(), Upstream{Name: "api", URL: "https://api.example.com", Enabled: true}); err != nil || up.ID == uuid.Nil {
		t.Fatalf("UpsertUpstream(success) up=%#v err=%v", up, err)
	}
	if err := s.DeleteUpstream(context.Background(), "api"); err != nil {
		t.Fatalf("DeleteUpstream(success) = %v", err)
	}
	if routes, err := s.ListRoutes(context.Background()); err != nil || len(routes) != 1 || routes[0].ID != "route-1" {
		t.Fatalf("ListRoutes(success) len=%d err=%v", len(routes), err)
	}
	if rt, err := s.GetRouteByID(context.Background(), "route-1"); err != nil || rt.ID != "route-1" {
		t.Fatalf("GetRouteByID(success) rt=%#v err=%v", rt, err)
	}
	if rt, err := s.UpsertRoute(context.Background(), Route{ID: "route-1", Path: "/v1/profile", Methods: []string{"GET"}, Target: "api", Enabled: true}); err != nil || rt.ID != "route-1" {
		t.Fatalf("UpsertRoute(success) rt=%#v err=%v", rt, err)
	}
	if err := s.DeleteRoute(context.Background(), "route-1"); err != nil {
		t.Fatalf("DeleteRoute(success) = %v", err)
	}

	s.pool = &proxyFakeDB{queryFn: func(context.Context, string, ...any) (pgx.Rows, error) { return nil, errors.New("query failed") }}
	if _, err := s.ListUpstreams(context.Background()); err == nil || err.Error() != "query failed" {
		t.Fatalf("ListUpstreams(query error) = %v", err)
	}
	if _, err := s.ListRoutes(context.Background()); err == nil || err.Error() != "query failed" {
		t.Fatalf("ListRoutes(query error) = %v", err)
	}

	s.pool = &proxyFakeDB{
		queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
			return &proxyFakeRows{data: [][]any{{"bad"}}}, nil
		},
		queryRowFn: func(context.Context, string, ...any) pgx.Row { return proxyFakeRow{err: pgx.ErrNoRows} },
	}
	if _, err := s.ListUpstreams(context.Background()); err == nil {
		t.Fatal("ListUpstreams(scan error) expected error")
	}
	if _, err := s.ListRoutes(context.Background()); err == nil {
		t.Fatal("ListRoutes(scan error) expected error")
	}
	if _, err := s.GetUpstreamByName(context.Background(), "missing"); !errors.Is(err, ErrUpstreamNotFound) {
		t.Fatalf("GetUpstreamByName(not found) = %v", err)
	}
	if _, err := s.GetRouteByID(context.Background(), "missing"); !errors.Is(err, ErrRouteNotFound) {
		t.Fatalf("GetRouteByID(not found) = %v", err)
	}

	s.pool = &proxyFakeDB{
		queryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
			switch {
			case strings.Contains(sql, "SELECT COUNT(*) FROM proxy_routes"):
				return proxyFakeRow{vals: []any{2}}
			case strings.Contains(sql, "INSERT INTO proxy_upstreams"):
				return proxyFakeRow{err: errors.New("upsert upstream failed")}
			case strings.Contains(sql, "INSERT INTO proxy_routes"):
				return proxyFakeRow{err: errors.New("upsert route failed")}
			default:
				return proxyFakeRow{err: pgx.ErrNoRows}
			}
		},
		execFn: func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
			if strings.Contains(sql, "proxy_routes") {
				return pgconn.NewCommandTag("DELETE 0"), nil
			}
			return pgconn.NewCommandTag("DELETE 0"), nil
		},
	}
	if err := s.DeleteUpstream(context.Background(), "api"); !errors.Is(err, ErrUpstreamInUse) {
		t.Fatalf("DeleteUpstream(in use) = %v", err)
	}
	if _, err := s.UpsertUpstream(context.Background(), Upstream{Name: "api"}); err == nil || err.Error() != "upsert upstream failed" {
		t.Fatalf("UpsertUpstream(error) = %v", err)
	}
	if _, err := s.UpsertRoute(context.Background(), Route{ID: "r1"}); err == nil || err.Error() != "upsert route failed" {
		t.Fatalf("UpsertRoute(error) = %v", err)
	}
	if err := s.DeleteRoute(context.Background(), "missing"); !errors.Is(err, ErrRouteNotFound) {
		t.Fatalf("DeleteRoute(not found) = %v", err)
	}

	s.pool = &proxyFakeDB{
		queryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
			if strings.Contains(sql, "SELECT COUNT(*) FROM proxy_routes") {
				return proxyFakeRow{vals: []any{0}}
			}
			return proxyFakeRow{err: pgx.ErrNoRows}
		},
		execFn: func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
			if strings.Contains(sql, "proxy_upstreams") {
				return pgconn.NewCommandTag("DELETE 0"), nil
			}
			return pgconn.CommandTag{}, errors.New("delete failed")
		},
	}
	if err := s.DeleteUpstream(context.Background(), "missing"); !errors.Is(err, ErrUpstreamNotFound) {
		t.Fatalf("DeleteUpstream(not found) = %v", err)
	}
	if err := s.DeleteRoute(context.Background(), "r1"); err == nil || err.Error() != "delete failed" {
		t.Fatalf("DeleteRoute(exec error) = %v", err)
	}

	if _, err := scanUpstream(proxyFakeRow{vals: []any{
		upstreamID, "api", "https://api.example.com", "/healthz", "READY", "5s", 10, []byte(`{`), []byte(`{}`), true, now, now,
	}}); err == nil {
		t.Fatal("scanUpstream(invalid headers json) expected error")
	}
	if _, err := scanRoute(proxyFakeRow{vals: []any{
		"route-1", "/v1/profile", []byte(`{`), true, "aal2", []byte(`[]`), []byte(`{}`), "api", 1, []byte(`{}`), []byte(`{}`), true, "", now, now,
	}}); err == nil {
		t.Fatal("scanRoute(invalid methods json) expected error")
	}

	if normalizeCircuitBreaker(nil) == nil || normalizeRateLimit(nil) == nil || normalizeRewrite(nil) == nil {
		t.Fatal("normalize* nil branches should return map values")
	}
	if uuidText(uuid.Nil) != "" {
		t.Fatal("uuidText(nil) should be empty")
	}
}
