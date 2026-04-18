package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	coresession "github.com/aegion/aegion/core/session"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func TestPasswordHandlerAdditionalHelperBranches(t *testing.T) {
	t.Run("with session header secret clears when empty", func(t *testing.T) {
		h := &Handler{}
		WithSessionHeaderSecret([]byte("0123456789abcdef0123456789abcdef"))(h)
		if len(h.sessionHeaderSecret) == 0 {
			t.Fatal("expected secret to be set")
		}

		WithSessionHeaderSecret(nil)(h)
		if h.sessionHeaderSecret != nil {
			t.Fatalf("expected empty secret option to clear header secret, got %#v", h.sessionHeaderSecret)
		}
	})

	t.Run("create session falls back when session manager returns nil session", func(t *testing.T) {
		svc := &MockService{}
		sessionManager := &MockSessionManager{}
		h := New(svc, WithSessionManager(sessionManager))
		identityID := uuid.New()

		sessionManager.On(
			"Create",
			mock.Anything,
			identityID,
			coresession.AuthMethodPassword,
			mock.AnythingOfType("session.DeviceInfo"),
		).Return((*coresession.Session)(nil), nil).Once()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/login", nil)
		req.RemoteAddr = "127.0.0.1:443"
		resp := &SuccessResponse{}

		if err := h.createSession(context.Background(), rec, req, identityID, resp); err != nil {
			t.Fatalf("createSession returned error: %v", err)
		}
		if resp.Session.IdentityID != identityID.String() {
			t.Fatalf("expected fallback identity id %q, got %q", identityID.String(), resp.Session.IdentityID)
		}
		if resp.Session.AAL != string(coresession.AAL1) {
			t.Fatalf("expected fallback aal %q, got %q", coresession.AAL1, resp.Session.AAL)
		}
		sessionManager.AssertNotCalled(t, "SetCookie", mock.Anything, mock.Anything)
		sessionManager.AssertExpectations(t)
	})

	t.Run("identity id from request rejects invalid signed session headers", func(t *testing.T) {
		h := New(&MockService{}, WithSessionHeaderSecret([]byte("0123456789abcdef0123456789abcdef")))
		req := httptest.NewRequest(http.MethodPost, "/change-password", nil)
		req.Header.Set(coresession.HeaderPrefix+"Session-ID", uuid.NewString())
		req.Header.Set(coresession.HeaderPrefix+"Identity-ID", uuid.NewString())
		req.Header.Set(coresession.HeaderPrefix+"AAL", string(coresession.AAL1))
		req.Header.Set(coresession.HeaderPrefix+"Signature", "not-a-valid-signature")

		if _, err := h.identityIDFromRequest(req); err == nil {
			t.Fatal("expected invalid signed session header error")
		}
	})

	t.Run("signed session header detector false branch", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if hasSignedSessionContextHeaders(req) {
			t.Fatal("expected false when no signed session headers are present")
		}
	})

	t.Run("request ip helper branches", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Forwarded-For", "198.51.100.10, 198.51.100.11")
		if got := requestIP(req); got != "198.51.100.10" {
			t.Fatalf("expected xff first hop, got %q", got)
		}

		req = httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Real-IP", "203.0.113.20")
		if got := requestIP(req); got != "203.0.113.20" {
			t.Fatalf("expected x-real-ip value, got %q", got)
		}

		req = httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "[2001:db8::1]:443"
		if got := requestIP(req); got != "2001:db8::1" {
			t.Fatalf("expected split host without brackets, got %q", got)
		}

		req = httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "198.51.100.99"
		if got := requestIP(req); got != "198.51.100.99" {
			t.Fatalf("expected raw remote addr fallback, got %q", got)
		}
	})

	t.Run("decode json body rejects multiple JSON objects", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewBufferString(`{"identifier":"a","password":"b"}{"extra":true}`))
		rec := httptest.NewRecorder()
		var payload LoginRequest

		if err := decodeJSONBody(rec, req, &payload); err == nil {
			t.Fatal("expected decodeJSONBody to reject multiple JSON objects")
		}
	})
}
