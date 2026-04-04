package security

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCSRFProtection_InvalidTokenBranch(t *testing.T) {
	handler := CSRFProtection(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	getReq := httptest.NewRequest(http.MethodGet, "/api/admin/identities", nil)
	getRec := httptest.NewRecorder()
	handler.ServeHTTP(getRec, getReq)

	resp := getRec.Result()
	defer resp.Body.Close()

	var csrfCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == csrfCookieName {
			csrfCookie = c
			break
		}
	}
	if csrfCookie == nil || csrfCookie.Value == "" {
		t.Fatalf("expected CSRF cookie from safe method request")
	}

	postReq := httptest.NewRequest(http.MethodPost, "/api/admin/identities", nil)
	postReq.AddCookie(csrfCookie)
	postReq.Header.Set(csrfHeaderName, csrfCookie.Value+"-invalid")
	postRec := httptest.NewRecorder()
	handler.ServeHTTP(postRec, postReq)

	if postRec.Code != http.StatusForbidden {
		t.Fatalf("expected invalid CSRF token to return 403, got %d", postRec.Code)
	}
}

func TestGenerateCSRFTokenErrorPaths(t *testing.T) {
	origReadRandom := readRandom
	readRandom = func([]byte) (int, error) { return 0, errors.New("entropy failed") }
	t.Cleanup(func() { readRandom = origReadRandom })

	if _, err := generateCSRFToken(); err == nil {
		t.Fatalf("expected generateCSRFToken to fail when randomness source fails")
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	if _, err := ensureCSRFCookie(rec, req); err == nil {
		t.Fatalf("expected ensureCSRFCookie to fail when token generation fails")
	}

	handler := CSRFProtection(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	safeReq := httptest.NewRequest(http.MethodGet, "/api/admin/identities", nil)
	safeRec := httptest.NewRecorder()
	handler.ServeHTTP(safeRec, safeReq)
	if safeRec.Code != http.StatusInternalServerError {
		t.Fatalf("expected safe-method CSRF init failure to return 500, got %d", safeRec.Code)
	}
}

func TestRateLimiterCleanupLoopDeletesIdleBuckets(t *testing.T) {
	ticks := make(chan time.Time, 1)
	origTicker := newCleanupTicker
	newCleanupTicker = func() (<-chan time.Time, func()) {
		return ticks, func() {}
	}
	t.Cleanup(func() { newCleanupTicker = origTicker })

	limiter := &rateLimiter{
		rps:   1,
		burst: 1,
		buckets: map[string]*tokenBucket{
			"old": {
				capacity: 1,
				tokens:   1,
				refill:   1,
				lastFill: time.Now().Add(-11 * time.Minute),
			},
			"new": {
				capacity: 1,
				tokens:   1,
				refill:   1,
				lastFill: time.Now(),
			},
		},
	}

	done := make(chan struct{})
	go func() {
		limiter.cleanupLoop()
		close(done)
	}()

	ticks <- time.Now()
	close(ticks)
	<-done

	if _, ok := limiter.buckets["old"]; ok {
		t.Fatalf("expected old idle bucket to be deleted")
	}
	if _, ok := limiter.buckets["new"]; !ok {
		t.Fatalf("expected recently-used bucket to remain")
	}
}
