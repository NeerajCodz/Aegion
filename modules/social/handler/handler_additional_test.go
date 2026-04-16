package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aegion/aegion/modules/social/service"
)

func TestSocialHandlerAdditionalBranches(t *testing.T) {
	t.Run("providers and path routing branches", func(t *testing.T) {
		h := New(&stubSocialService{})
		mux := http.NewServeMux()
		h.RegisterRoutes(mux)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/social/providers", nil)
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected %d, got %d", http.StatusMethodNotAllowed, rec.Code)
		}

		h = New(&stubSocialService{configuredErr: nil, providers: nil})
		mux = http.NewServeMux()
		h.RegisterRoutes(mux)
		h.svc = &stubSocialService{startErr: nil}
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/api/v1/social/too-short", nil)
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected %d, got %d", http.StatusNotFound, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/api/v1/social/google/unknown", nil)
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected %d, got %d", http.StatusNotFound, rec.Code)
		}
	})

	t.Run("start and callback error mapping", func(t *testing.T) {
		h := New(&stubSocialService{}, Config{ManagementToken: "secret"})
		mux := http.NewServeMux()
		h.RegisterRoutes(mux)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/social/google/start", nil)
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected %d, got %d", http.StatusMethodNotAllowed, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/api/v1/social/google/start", strings.NewReader("{"))
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		h.svc = &stubSocialService{startErr: service.ErrProviderMisconfig}
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/api/v1/social/google/start", strings.NewReader(`{"redirect_to":"/home"}`))
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		h.svc = &stubSocialService{startErr: errors.New("provider failed")}
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/api/v1/social/google/start", strings.NewReader(`{"redirect_to":"/home"}`))
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPut, "/api/v1/social/google/callback", nil)
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected %d, got %d", http.StatusMethodNotAllowed, rec.Code)
		}

		h.svc = &stubSocialService{callbackErr: service.ErrInvalidState}
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/api/v1/social/google/callback?state=s1&code=c1", nil)
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}
	})

	t.Run("callback redirect and admin route branches", func(t *testing.T) {
		h := New(&stubSocialService{
			callbackRes: &service.CallbackResult{
				Provider:   "google",
				IdentityID: "identity-1",
				RedirectTo: "https://app.example.com/post-login",
			},
		}, Config{ManagementToken: "secret-token"})
		mux := http.NewServeMux()
		h.RegisterRoutes(mux)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/social/google/callback?state=s1&code=c1", nil)
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("expected %d, got %d", http.StatusSeeOther, rec.Code)
		}
		if !strings.Contains(rec.Header().Get("Location"), "social_status=authenticated") {
			t.Fatalf("expected social redirect query params, got %q", rec.Header().Get("Location"))
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPatch, "/api/v1/social/admin/providers", nil)
		req.Header.Set("Authorization", "Bearer secret-token")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected %d, got %d", http.StatusMethodNotAllowed, rec.Code)
		}

		h.svc = &stubSocialService{upsertErr: service.ErrProviderMisconfig}
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/api/v1/social/admin/providers", strings.NewReader(`{"slug":"x"}`))
		req.Header.Set("Authorization", "Bearer secret-token")
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/api/v1/social/admin/providers/", nil)
		req.Header.Set("Authorization", "Bearer secret-token")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected %d, got %d", http.StatusNotFound, rec.Code)
		}

		h.svc = &stubSocialService{deleteErr: service.ErrProviderUnsupported}
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodDelete, "/api/v1/social/admin/providers/missing", nil)
		req.Header.Set("Authorization", "Bearer secret-token")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected %d, got %d", http.StatusNotFound, rec.Code)
		}
	})
}

func TestSocialWithQueryHelper(t *testing.T) {
	if got := withQuery(":// bad", map[string]string{"a": "b"}); got != ":// bad" {
		t.Fatalf("expected invalid URL to passthrough, got %q", got)
	}

	got := withQuery("https://app.example.com/callback?x=1", map[string]string{
		"identity_id": "id-1",
		"empty":       "  ",
	})
	if !strings.Contains(got, "identity_id=id-1") || !strings.Contains(got, "x=1") || strings.Contains(got, "empty=") {
		t.Fatalf("unexpected query merge result: %q", got)
	}

	if got := firstNonEmpty("  ", "", "value", "other"); got != "value" {
		t.Fatalf("firstNonEmpty returned %q", got)
	}
	if got := firstNonEmpty(" ", ""); got != "" {
		t.Fatalf("firstNonEmpty empty returned %q", got)
	}

}

