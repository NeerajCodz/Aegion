package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSocialHandlerMoreBranches(t *testing.T) {
	t.Run("provider listing and nil service branches", func(t *testing.T) {
		h := New(&stubSocialService{})
		mux := http.NewServeMux()
		h.RegisterRoutes(mux)

		h.svc = &stubSocialService{providers: nil}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/social/providers", nil)
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("providers success expected %d got %d", http.StatusOK, rec.Code)
		}

		h.svc = nil
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/api/v1/social/google/start", nil)
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("nil service expected %d got %d", http.StatusInternalServerError, rec.Code)
		}
	})

	t.Run("management auth and admin errors", func(t *testing.T) {
		h := New(&stubSocialService{configuredErr: errors.New("list failed")}, Config{ManagementToken: "secret"})
		mux := http.NewServeMux()
		h.RegisterRoutes(mux)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/social/admin/providers", nil)
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("missing token expected %d got %d", http.StatusUnauthorized, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/api/v1/social/admin/providers", nil)
		req.Header.Set("Authorization", "Bearer wrong")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("invalid token expected %d got %d", http.StatusUnauthorized, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/api/v1/social/admin/providers", nil)
		req.Header.Set("Authorization", "Bearer secret")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("list providers error expected %d got %d", http.StatusInternalServerError, rec.Code)
		}

		h.svc = &stubSocialService{getProviderErr: errors.New("missing")}
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/api/v1/social/admin/providers/google", nil)
		req.Header.Set("Authorization", "Bearer secret")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("get provider error expected %d got %d", http.StatusNotFound, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPatch, "/api/v1/social/admin/providers/google", strings.NewReader("{}"))
		req.Header.Set("Authorization", "Bearer secret")
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("admin provider method expected %d got %d", http.StatusMethodNotAllowed, rec.Code)
		}
	})
}

