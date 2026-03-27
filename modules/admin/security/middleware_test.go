package security

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCSRFProtectionSetsCookie(t *testing.T) {
	handler := CSRFProtection(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/admin/identities", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()

	var csrfCookie *http.Cookie
	for _, cookie := range resp.Cookies() {
		if cookie.Name == csrfCookieName {
			csrfCookie = cookie
			break
		}
	}

	if csrfCookie == nil || csrfCookie.Value == "" {
		t.Fatalf("expected CSRF cookie to be set")
	}

	if header := resp.Header.Get(csrfHeaderName); header != csrfCookie.Value {
		t.Fatalf("expected CSRF header %q to match cookie", csrfHeaderName)
	}
}

func TestCSRFProtectionRejectsMissingToken(t *testing.T) {
	handler := CSRFProtection(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/admin/identities", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d", http.StatusForbidden, rec.Code)
	}
}

func TestCSRFProtectionAcceptsValidToken(t *testing.T) {
	handler := CSRFProtection(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	getReq := httptest.NewRequest(http.MethodGet, "/api/admin/identities", nil)
	getRec := httptest.NewRecorder()
	handler.ServeHTTP(getRec, getReq)

	resp := getRec.Result()
	defer resp.Body.Close()

	var csrfCookie *http.Cookie
	for _, cookie := range resp.Cookies() {
		if cookie.Name == csrfCookieName {
			csrfCookie = cookie
			break
		}
	}

	if csrfCookie == nil || csrfCookie.Value == "" {
		t.Fatalf("expected CSRF cookie to be set")
	}

	postReq := httptest.NewRequest(http.MethodPost, "/api/admin/identities", nil)
	postReq.AddCookie(csrfCookie)
	postReq.Header.Set(csrfHeaderName, csrfCookie.Value)

	postRec := httptest.NewRecorder()
	handler.ServeHTTP(postRec, postReq)

	if postRec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, postRec.Code)
	}
}

func TestRateLimitAdminBlocksAfterBurst(t *testing.T) {
	t.Setenv("AEGION_ADMIN_RATE_LIMIT_RPS", "1")
	t.Setenv("AEGION_ADMIN_RATE_LIMIT_BURST", "1")

	handler := RateLimitAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req1 := httptest.NewRequest(http.MethodGet, "/api/admin/identities", nil)
	req1.Header.Set("X-Aegion-Session-Identity-ID", "11111111-1111-1111-1111-111111111111")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec1.Code)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/admin/identities", nil)
	req2.Header.Set("X-Aegion-Session-Identity-ID", "11111111-1111-1111-1111-111111111111")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status %d, got %d", http.StatusTooManyRequests, rec2.Code)
	}
}
