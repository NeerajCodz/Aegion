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
	if err := store.SaveRun(ctx, CommandRun{
		ID:         "run-1",
		Command:    "status.summary",
		Arguments:  map[string]any{"limit": 5},
		Result:     map[string]any{"ok": true},
		Success:    true,
		ExecutedAt: time.Now().UTC(),
	}); err == nil {
		t.Fatal("SaveRun expected error with unreachable DB")
	}
	if _, err := store.ListRuns(ctx, 10); err == nil {
		t.Fatal("ListRuns expected error with unreachable DB")
	}
	if _, err := store.GetRun(ctx, "run-1"); err == nil {
		t.Fatal("GetRun expected error with unreachable DB")
	}

	status, err := store.StatusSummary(ctx)
	if err != nil {
		t.Fatalf("StatusSummary should tolerate missing tables, got %v", err)
	}
	if status["database"] != "connected" {
		t.Fatalf("StatusSummary unexpected payload: %#v", status)
	}

	runtimeCfg, err := store.RuntimeConfig(ctx)
	if err != nil {
		t.Fatalf("RuntimeConfig should tolerate missing config table, got %v", err)
	}
	if runtimeCfg["database"] != "connected" {
		t.Fatalf("RuntimeConfig unexpected payload: %#v", runtimeCfg)
	}

	courier, err := store.CourierSummary(ctx)
	if err != nil {
		t.Fatalf("CourierSummary should tolerate missing courier table, got %v", err)
	}
	if courier["database"] != "connected" {
		t.Fatalf("CourierSummary unexpected payload: %#v", courier)
	}
}
