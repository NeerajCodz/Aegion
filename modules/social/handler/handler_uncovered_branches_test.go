package handler

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSocialHandlerUncoveredBranches(t *testing.T) {
	t.Run("register routes accepts nil mux", func(t *testing.T) {
		New(&stubSocialService{}).RegisterRoutes(nil)
	})

	t.Run("providers route returns internal error on list failure", func(t *testing.T) {
		h := New(&stubSocialService{listErr: errors.New("list failed")})
		mux := http.NewServeMux()
		h.RegisterRoutes(mux)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/social/providers", nil)
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
	})

	t.Run("social path too short returns not found", func(t *testing.T) {
		h := New(&stubSocialService{})
		mux := http.NewServeMux()
		h.RegisterRoutes(mux)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/social/", nil)
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected %d, got %d", http.StatusNotFound, rec.Code)
		}
	})

	t.Run("post callback parse form failure returns bad request", func(t *testing.T) {
		h := New(&stubSocialService{})
		mux := http.NewServeMux()
		h.RegisterRoutes(mux)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/social/google/callback", bytes.NewBufferString("%zz"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}
	})

	t.Run("admin provider route requires auth and delete success", func(t *testing.T) {
		h := New(&stubSocialService{}, Config{ManagementToken: "secret-token"})
		mux := http.NewServeMux()
		h.RegisterRoutes(mux)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/social/admin/providers/google", nil)
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodDelete, "/api/v1/social/admin/providers/google", nil)
		req.Header.Set("Authorization", "Bearer secret-token")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected %d, got %d", http.StatusNoContent, rec.Code)
		}
	})

	t.Run("admin providers decode json rejects extra payload", func(t *testing.T) {
		h := New(&stubSocialService{}, Config{ManagementToken: "secret-token"})
		mux := http.NewServeMux()
		h.RegisterRoutes(mux)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/social/admin/providers", bytes.NewBufferString(`{"slug":"x"}{"extra":true}`))
		req.Header.Set("Authorization", "Bearer secret-token")
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}
	})
}
