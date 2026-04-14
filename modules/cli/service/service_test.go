package service

import (
	"context"
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
