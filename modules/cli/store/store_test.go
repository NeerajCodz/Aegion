package store

import (
	"context"
	"testing"
	"time"
)

func TestMemoryStorePersistsRuns(t *testing.T) {
	repo := New()
	run := CommandRun{
		ID:         "run-1",
		Command:    "status.summary",
		Success:    true,
		ExecutedAt: time.Now().UTC(),
	}
	if err := repo.SaveRun(context.Background(), run); err != nil {
		t.Fatalf("save run: %v", err)
	}

	runs, err := repo.ListRuns(context.Background(), 10)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 1 || runs[0].ID != run.ID {
		t.Fatalf("unexpected runs: %+v", runs)
	}
}
