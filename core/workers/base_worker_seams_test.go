package workers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestWorkerErrorRowScan(t *testing.T) {
	expected := errors.New("sentinel scan error")
	if err := (workerErrorRow{err: expected}).Scan(new(int)); !errors.Is(err, expected) {
		t.Fatalf("expected workerErrorRow.Scan to return sentinel error, got %v", err)
	}
}

func TestNewBaseWorkerWithDBInitializesDelegates(t *testing.T) {
	worker := NewBaseWorker("with-db", &pgxpool.Pool{}, nil, time.Second)
	if worker.execFn == nil || worker.queryFn == nil || worker.queryRowFn == nil {
		t.Fatalf("expected DB-backed delegates to be initialized")
	}
}

func TestNewBaseWorkerNilDBQueryRowSeam(t *testing.T) {
	worker := NewBaseWorker("nil-db", nil, nil, time.Second)
	if err := worker.queryRowFn(context.Background(), "SELECT 1").Scan(new(int)); !errors.Is(err, errWorkerDBUnavailable) {
		t.Fatalf("expected errWorkerDBUnavailable from nil-db queryRow seam, got %v", err)
	}
}

func TestBaseWorkerMethodsWhenSeamsUnset(t *testing.T) {
	worker := NewBaseWorker("unset-seams", nil, nil, time.Second)
	worker.execFn = nil
	worker.queryFn = nil
	worker.queryRowFn = nil

	if _, err := worker.exec(context.Background(), "UPDATE x SET y = 1"); !errors.Is(err, errWorkerDBUnavailable) {
		t.Fatalf("expected errWorkerDBUnavailable from exec with nil seam, got %v", err)
	}
	if _, err := worker.query(context.Background(), "SELECT 1"); !errors.Is(err, errWorkerDBUnavailable) {
		t.Fatalf("expected errWorkerDBUnavailable from query with nil seam, got %v", err)
	}
	if err := worker.queryRow(context.Background(), "SELECT 1").Scan(new(int)); !errors.Is(err, errWorkerDBUnavailable) {
		t.Fatalf("expected errWorkerDBUnavailable from queryRow with nil seam, got %v", err)
	}
}
