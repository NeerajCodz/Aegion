package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aegion/aegion/modules/cli/service"
	"github.com/aegion/aegion/modules/cli/store"
)

type stubCLIService struct{}

func (stubCLIService) Commands() []service.CommandDescriptor {
	return []service.CommandDescriptor{{Name: "status.summary"}}
}

func (stubCLIService) Execute(context.Context, service.ExecuteRequest) (*store.CommandRun, error) {
	return &store.CommandRun{ID: "run-1", Command: "status.summary", Success: true}, nil
}

func (stubCLIService) ListRuns(context.Context, int) ([]store.CommandRun, error) {
	return []store.CommandRun{{ID: "run-1", Command: "status.summary", Success: true}}, nil
}

func (stubCLIService) GetRun(context.Context, string) (*store.CommandRun, error) {
	return &store.CommandRun{ID: "run-1", Command: "status.summary", Success: true}, nil
}

func TestCommandsRequireManagementToken(t *testing.T) {
	h := New(stubCLIService{})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cli/commands", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}
}

func TestCLIHandlersServeCommandsRunsAndExecution(t *testing.T) {
	h := New(stubCLIService{}, Config{ManagementToken: "secret"})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	authHeader := http.Header{}
	authHeader.Set("Authorization", "Bearer secret")

	t.Run("commands", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/cli/commands", nil)
		req.Header = authHeader.Clone()
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
	})

	t.Run("runs", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/cli/runs?limit=5", nil)
		req.Header = authHeader.Clone()
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
	})

	t.Run("execute", func(t *testing.T) {
		body, _ := json.Marshal(service.ExecuteRequest{Command: "status.summary"})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/cli/execute", bytes.NewReader(body))
		req.Header = authHeader.Clone()
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
	})
}
