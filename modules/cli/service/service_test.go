package service

import (
	"context"
	"errors"
	"testing"

	"github.com/aegion/aegion/modules/cli/store"
)

func TestCommandsAreExposedAndExecutable(t *testing.T) {
	svc := New(store.New())
	commands := svc.Commands()
	if len(commands) != 3 {
		t.Fatalf("expected 3 commands, got %d", len(commands))
	}

	run, err := svc.Execute(context.Background(), ExecuteRequest{Command: "status.summary"})
	if err != nil {
		t.Fatalf("execute status.summary: %v", err)
	}
	if !run.Success {
		t.Fatal("expected successful command run")
	}
	if run.Result["database"] != "unavailable" {
		t.Fatalf("unexpected result: %+v", run.Result)
	}
}

func TestExecuteRejectsUnsupportedCommand(t *testing.T) {
	svc := New(store.New())
	run, err := svc.Execute(context.Background(), ExecuteRequest{Command: "not.real"})
	if err == nil {
		t.Fatal("expected unsupported command error")
	}
	if run == nil || run.Success {
		t.Fatalf("expected failed stored run, got %+v", run)
	}
}

func TestExecuteEmptyCommandAndSaveFailure(t *testing.T) {
	svc := New(store.New())
	run, err := svc.Execute(context.Background(), ExecuteRequest{Command: "   "})
	if !errors.Is(err, ErrUnsupportedCommand) {
		t.Fatalf("expected unsupported command error, got %v", err)
	}
	if run != nil {
		t.Fatalf("expected no run for empty command, got %+v", run)
	}

	failingSvc := New(&failingSaveRepo{Repository: store.New()})
	run, err = failingSvc.Execute(context.Background(), ExecuteRequest{Command: "status.summary"})
	if err == nil || err.Error() != "save failed" {
		t.Fatalf("expected save failure error, got run=%+v err=%v", run, err)
	}
	if run != nil {
		t.Fatalf("expected nil run on save failure, got %+v", run)
	}
}

func TestListRunsAndGetRunWrappers(t *testing.T) {
	repo := store.New()
	svc := New(repo)
	ctx := context.Background()

	args := map[string]any{"scope": "full"}
	first, err := svc.Execute(ctx, ExecuteRequest{Command: "status.summary", Arguments: args})
	if err != nil {
		t.Fatalf("execute first command: %v", err)
	}
	args["scope"] = "mutated"
	second, err := svc.Execute(ctx, ExecuteRequest{Command: "runtime.config"})
	if err != nil {
		t.Fatalf("execute second command: %v", err)
	}

	runs, err := svc.ListRuns(ctx, 1)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected one run with limit=1, got %d", len(runs))
	}

	storedFirst, err := svc.GetRun(ctx, first.ID)
	if err != nil {
		t.Fatalf("get first run: %v", err)
	}
	if storedFirst.Arguments["scope"] != "full" {
		t.Fatalf("expected cloned arguments map, got %+v", storedFirst.Arguments)
	}

	storedSecond, err := svc.GetRun(ctx, second.ID)
	if err != nil {
		t.Fatalf("get second run: %v", err)
	}
	if storedSecond.Command != "runtime.config" {
		t.Fatalf("unexpected second run: %+v", storedSecond)
	}
}

func TestCloneMapCopiesInput(t *testing.T) {
	empty := cloneMap(nil)
	if len(empty) != 0 {
		t.Fatalf("expected empty map for nil input, got %+v", empty)
	}

	original := map[string]any{"a": 1}
	cloned := cloneMap(original)
	cloned["a"] = 2
	if original["a"] != 1 {
		t.Fatalf("expected clone to be independent, got original=%+v clone=%+v", original, cloned)
	}
}

type failingSaveRepo struct {
	store.Repository
}

func (f *failingSaveRepo) SaveRun(context.Context, store.CommandRun) error {
	return errors.New("save failed")
}
