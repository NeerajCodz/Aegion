package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	coresession "github.com/aegion/aegion/core/session"
)

func TestMagicLinkHeaderIdentityAdditionalBranches(t *testing.T) {
	h := New(&MockService{}, WithSessionHeaderSecret([]byte("secret")))
	if len(h.sessionHeaderSecret) == 0 {
		t.Fatal("WithSessionHeaderSecret(non-empty) should set secret")
	}
	WithSessionHeaderSecret(nil)(h)
	if h.sessionHeaderSecret != nil {
		t.Fatal("WithSessionHeaderSecret(empty) should clear secret")
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if hasSignedSessionContextHeaders(req) {
		t.Fatal("hasSignedSessionContextHeaders(no headers) should be false")
	}

	h = New(&MockService{}, WithSessionHeaderSecret([]byte("secret")))
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(coresession.HeaderPrefix+"Signature", "bad")
	if _, err := h.identityIDFromRequest(req); err == nil {
		t.Fatal("identityIDFromRequest(invalid signed headers) expected error")
	}

	h = New(&MockService{}, WithLegacyIdentityHeaderAuth(true))
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Aegion-Identity-ID", "not-a-uuid")
	if _, err := h.identityIDFromRequest(req); err == nil {
		t.Fatal("identityIDFromRequest(invalid legacy uuid) expected error")
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	if _, err := h.identityIDFromRequest(req); err == nil {
		t.Fatal("identityIDFromRequest(missing legacy header) expected error")
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "raw-addr-without-port"
	if got := requestIP(req); got != "raw-addr-without-port" {
		t.Fatalf("requestIP(raw fallback) got %q", got)
	}
}

