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

func TestHeaders(t *testing.T) {
	handler := Headers(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Verify security headers are set
	if rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Error("X-Frame-Options header not set correctly")
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("X-Content-Type-Options header not set correctly")
	}
	if rec.Header().Get("X-XSS-Protection") != "1; mode=block" {
		t.Error("X-XSS-Protection header not set correctly")
	}
	if rec.Header().Get("Referrer-Policy") != "strict-origin-when-cross-origin" {
		t.Error("Referrer-Policy header not set correctly")
	}
	if rec.Header().Get("Cache-Control") != "no-cache, no-store, must-revalidate, private" {
		t.Error("Cache-Control header not set correctly")
	}
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Error("Content-Security-Policy header not set")
	}
}

func TestDevHeaders(t *testing.T) {
	handler := DevHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Verify dev headers are set with relaxed CSP
	csp := rec.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Error("Content-Security-Policy header not set")
	}
	// Dev mode should allow unsafe-eval
	if !contains(csp, "unsafe-eval") {
		t.Error("Dev CSP should allow unsafe-eval")
	}
	if rec.Header().Get("Cache-Control") != "public, max-age=3600" {
		t.Errorf("Cache-Control header not set correctly, got: %s", rec.Header().Get("Cache-Control"))
	}
}

func TestRequestID(t *testing.T) {
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// RequestID is set in response header
	responseID := rec.Header().Get("X-Request-ID")
	if responseID == "" {
		t.Error("Request ID not set in response header")
	}
}

func TestRequestIDWithExisting(t *testing.T) {
	existingID := "test-request-id-123"
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", existingID)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Verify existing request ID is preserved in response
	responseID := rec.Header().Get("X-Request-ID")
	if responseID != existingID {
		t.Errorf("Expected request ID %s, got %s", existingID, responseID)
	}
}

func TestSecurityAudit(t *testing.T) {
	handler := SecurityAudit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/admin/operators", nil)
	req.Header.Set("X-Aegion-Session-Identity-ID", "11111111-1111-1111-1111-111111111111")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestGetClientIP(t *testing.T) {
	tests := []struct {
		name       string
		headers    map[string]string
		remoteAddr string
		expected   string
	}{
		{
			name:       "direct connection",
			remoteAddr: "192.168.1.1:12345",
			expected:   "192.168.1.1",
		},
		{
			name:       "X-Real-IP header",
			headers:    map[string]string{"X-Real-IP": "10.0.0.1"},
			remoteAddr: "192.168.1.1:12345",
			expected:   "10.0.0.1",
		},
		{
			name:       "X-Forwarded-For header single IP",
			headers:    map[string]string{"X-Forwarded-For": "10.0.0.2"},
			remoteAddr: "192.168.1.1:12345",
			expected:   "10.0.0.2",
		},
		{
			name:       "X-Forwarded-For header multiple IPs",
			headers:    map[string]string{"X-Forwarded-For": "10.0.0.3, 10.0.0.4, 10.0.0.5"},
			remoteAddr: "192.168.1.1:12345",
			expected:   "10.0.0.3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			req.RemoteAddr = tt.remoteAddr

			ip := getClientIP(req)
			if ip != tt.expected {
				t.Errorf("Expected IP %s, got %s", tt.expected, ip)
			}
		})
	}
}

func TestMinFloat(t *testing.T) {
	tests := []struct {
		a, b     float64
		expected float64
	}{
		{1.0, 2.0, 1.0},
		{2.0, 1.0, 1.0},
		{5.5, 5.5, 5.5},
		{-1.0, 1.0, -1.0},
		{0.0, 1.0, 0.0},
	}

	for _, tt := range tests {
		result := minFloat(tt.a, tt.b)
		if result != tt.expected {
			t.Errorf("minFloat(%f, %f) = %f, want %f", tt.a, tt.b, result, tt.expected)
		}
	}
}

func TestGenerateRequestID(t *testing.T) {
	id1 := generateRequestID()
	id2 := generateRequestID()

	if id1 == "" {
		t.Error("generateRequestID returned empty string")
	}
	if id2 == "" {
		t.Error("generateRequestID returned empty string")
	}
	if id1 == id2 {
		t.Error("generateRequestID should generate unique IDs")
	}
}

func TestCSRFProtectionAPIKeyAuth(t *testing.T) {
	handler := CSRFProtection(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// POST with API key (Bearer aegion_*) should bypass CSRF
	req := httptest.NewRequest(http.MethodPost, "/api/admin/identities", nil)
	req.Header.Set("Authorization", "Bearer aegion_test_key_12345")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("API key auth should bypass CSRF, got status %d", rec.Code)
	}
}

func TestCSRFProtectionSafeMethods(t *testing.T) {
	handler := CSRFProtection(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	safeMethods := []string{http.MethodGet, http.MethodHead, http.MethodOptions}
	for _, method := range safeMethods {
		req := httptest.NewRequest(method, "/api/admin/identities", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Safe method %s should not require CSRF token, got status %d", method, rec.Code)
		}
	}
}

func TestRateLimitKeyGeneration(t *testing.T) {
	tests := []struct {
		name       string
		identityID string
		ip         string
		wantPrefix string
	}{
		{
			name:       "with identity ID",
			identityID: "test-identity-123",
			ip:         "192.168.1.1",
			wantPrefix: "id:test-identity-123",
		},
		{
			name:       "without identity ID",
			identityID: "",
			ip:         "10.0.0.1",
			wantPrefix: "ip:10.0.0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.identityID != "" {
				req.Header.Set("X-Aegion-Session-Identity-ID", tt.identityID)
			}
			req.RemoteAddr = tt.ip + ":12345"

			key := rateLimitKey(req)
			if key != tt.wantPrefix {
				t.Errorf("Expected key %s, got %s", tt.wantPrefix, key)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsLoop(s, substr))
}

func containsLoop(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
