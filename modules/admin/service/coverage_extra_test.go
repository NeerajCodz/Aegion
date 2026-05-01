package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestBootstrap_PropagatesCanBootstrapError(t *testing.T) {
	mem := newMemoryStore()
	mem.listOperatorsErr = errors.New("list operators failed")
	svc := New(mem, Config{BootstrapEnabled: true})

	_, err := svc.Bootstrap(context.Background(), uuid.New(), "127.0.0.1")
	if err == nil || err.Error() != "list operators failed" {
		t.Fatalf("expected bootstrap to propagate list error, got %v", err)
	}
}

func TestLogAction_InitializesNilDetails(t *testing.T) {
	mem := newMemoryStore()
	svc := New(mem, Config{})
	operatorID := uuid.New()

	svc.logAction(context.Background(), &operatorID, "read", "operator", "resource-1", nil, "127.0.0.1")

	if len(mem.auditLogs) != 1 {
		t.Fatalf("expected one audit log entry, got %d", len(mem.auditLogs))
	}
	if mem.auditLogs[0].Details == nil {
		t.Fatalf("expected details map to be initialized when nil is provided")
	}
}
