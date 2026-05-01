package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aegion/aegion/modules/cli/service"
	"github.com/aegion/aegion/modules/cli/store"
)

type cliBehaviorService struct {
	commands    []service.CommandDescriptor
	listRuns    []store.CommandRun
	listRunsErr error
	getRun      *store.CommandRun
	getRunErr   error
	execRun     *store.CommandRun
	execErr     error
}

func (s *cliBehaviorService) Commands() []service.CommandDescriptor { return s.commands }

func (s *cliBehaviorService) Execute(context.Context, service.ExecuteRequest) (*store.CommandRun, error) {
	return s.execRun, s.execErr
}

func (s *cliBehaviorService) ListRuns(context.Context, int) ([]store.CommandRun, error) {
	return s.listRuns, s.listRunsErr
}

func (s *cliBehaviorService) GetRun(context.Context, string) (*store.CommandRun, error) {
	return s.getRun, s.getRunErr
}

func TestCLIHandlersAdditionalBranches(t *testing.T) {
	svc := &cliBehaviorService{
		commands: []service.CommandDescriptor{{Name: "status.summary"}},
		listRuns: []store.CommandRun{{ID: "run-1"}},
		getRun:   &store.CommandRun{ID: "run-1"},
		execRun:  &store.CommandRun{ID: "run-1", Success: true},
	}
	h := New(svc, Config{ManagementToken: "secret"})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	authHeader := http.Header{}
	authHeader.Set("Authorization", "Bearer secret")

	t.Run("handleRun branches", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/cli/runs/run-1", nil)
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/api/v1/cli/runs/run-1", nil)
		req.Header = authHeader.Clone()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected %d, got %d", http.StatusMethodNotAllowed, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/api/v1/cli/runs/", nil)
		req.Header = authHeader.Clone()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected %d, got %d", http.StatusNotFound, rec.Code)
		}

		svc.getRunErr = errors.New("missing")
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/api/v1/cli/runs/missing", nil)
		req.Header = authHeader.Clone()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected %d, got %d", http.StatusNotFound, rec.Code)
		}

		svc.getRunErr = nil
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/api/v1/cli/runs/run-1", nil)
		req.Header = authHeader.Clone()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
	})

	t.Run("handleRuns branches", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/cli/runs", nil)
		req.Header = authHeader.Clone()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected %d, got %d", http.StatusMethodNotAllowed, rec.Code)
		}

		svc.listRunsErr = errors.New("list failed")
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/api/v1/cli/runs?limit=999", nil)
		req.Header = authHeader.Clone()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		svc.listRunsErr = nil
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/api/v1/cli/runs?limit=abc", nil)
		req.Header = authHeader.Clone()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
	})

	t.Run("handleExecute branches", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/cli/execute", nil)
		req.Header = authHeader.Clone()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected %d, got %d", http.StatusMethodNotAllowed, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/api/v1/cli/execute", bytes.NewBufferString("{"))
		req.Header = authHeader.Clone()
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		svc.execRun = nil
		svc.execErr = service.ErrUnsupportedCommand
		rec = httptest.NewRecorder()
		body, _ := json.Marshal(service.ExecuteRequest{Command: "unknown"})
		req = httptest.NewRequest(http.MethodPost, "/api/v1/cli/execute", bytes.NewReader(body))
		req.Header = authHeader.Clone()
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		svc.execErr = errors.New("backend failed")
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/api/v1/cli/execute", bytes.NewReader(body))
		req.Header = authHeader.Clone()
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		svc.execRun = &store.CommandRun{ID: "run-2"}
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/api/v1/cli/execute", bytes.NewReader(body))
		req.Header = authHeader.Clone()
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}
	})
}
