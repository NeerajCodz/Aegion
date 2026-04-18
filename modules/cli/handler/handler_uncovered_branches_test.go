package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aegion/aegion/modules/cli/service"
)

func TestCLIHandlerUncoveredBranches(t *testing.T) {
	svc := &cliBehaviorService{
		commands: []service.CommandDescriptor{{Name: "status.summary"}},
	}
	h := New(svc, Config{ManagementToken: "secret"})

	t.Run("register routes nil mux", func(t *testing.T) {
		h.RegisterRoutes(nil)
	})

	t.Run("commands method guard and invalid token", func(t *testing.T) {
		mux := http.NewServeMux()
		h.RegisterRoutes(mux)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/cli/commands", nil)
		req.Header.Set("Authorization", "Bearer secret")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected %d, got %d", http.StatusMethodNotAllowed, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/api/v1/cli/commands", nil)
		req.Header.Set("Authorization", "Bearer wrong-token")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rec.Code)
		}
	})

	t.Run("execute unauthorized and strict trailing decode", func(t *testing.T) {
		mux := http.NewServeMux()
		h.RegisterRoutes(mux)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/cli/runs", nil)
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/api/v1/cli/execute", bytes.NewBufferString(`{"command":"status.summary"}`))
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/api/v1/cli/execute",
			bytes.NewBufferString(`{"command":"status.summary"}{"extra":true}`))
		req.Header.Set("Authorization", "Bearer secret")
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}
	})
}
