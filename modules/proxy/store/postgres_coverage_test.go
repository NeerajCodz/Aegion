package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func newUnreachablePostgresPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), "postgres://postgres:postgres@127.0.0.1:1/postgres?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatalf("create unreachable pool: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

func TestPostgresStoreErrorPathsOnUnreachableDB(t *testing.T) {
	if _, err := NewPostgres(nil); err == nil {
		t.Fatal("NewPostgres(nil) expected error")
	}

	store, err := NewPostgres(newUnreachablePostgresPool(t))
	if err != nil {
		t.Fatalf("NewPostgres(unreachable pool) error = %v", err)
	}

	ctx := context.Background()
	if _, err := store.ListUpstreams(ctx); err == nil {
		t.Fatal("ListUpstreams expected error with unreachable DB")
	}
	if _, err := store.GetUpstreamByName(ctx, "api"); err == nil {
		t.Fatal("GetUpstreamByName expected error with unreachable DB")
	}
	if _, err := store.UpsertUpstream(ctx, Upstream{
		Name:           "api",
		URL:            "https://api.example.com",
		HealthCheck:    "/healthz",
		Timeout:        "5s",
		MaxConnections: 10,
		Headers:        map[string]string{"x-env": "prod"},
		CircuitBreaker: &CircuitBreaker{FailureThreshold: 5},
		Enabled:        true,
	}); err == nil {
		t.Fatal("UpsertUpstream expected error with unreachable DB")
	}
	if err := store.DeleteUpstream(ctx, "api"); err == nil {
		t.Fatal("DeleteUpstream expected error with unreachable DB")
	}

	if _, err := store.ListRoutes(ctx); err == nil {
		t.Fatal("ListRoutes expected error with unreachable DB")
	}
	if _, err := store.GetRouteByID(ctx, "route-1"); err == nil {
		t.Fatal("GetRouteByID expected error with unreachable DB")
	}
	if _, err := store.UpsertRoute(ctx, Route{
		ID:           "route-1",
		Path:         "/v1/profile",
		Methods:      []string{"GET"},
		RequireAuth:  true,
		RequiredAAL:  "aal2",
		Capabilities: []string{"profile:read"},
		RateLimit:    &RateLimit{RequestsPerSecond: 10},
		Target:       "api",
		Priority:     100,
		Headers:      map[string]string{"x-test": "1"},
		Rewrite:      &Rewrite{StripPrefix: "/v1"},
		Enabled:      true,
		Description:  "profile route",
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}); err == nil {
		t.Fatal("UpsertRoute expected error with unreachable DB")
	}
	if err := store.DeleteRoute(ctx, "route-1"); err == nil {
		t.Fatal("DeleteRoute expected error with unreachable DB")
	}
}
