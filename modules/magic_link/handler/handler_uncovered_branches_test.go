package handler

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	coresession "github.com/aegion/aegion/core/session"
	"github.com/aegion/aegion/modules/magic_link/store"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func TestMagicLinkHandlerUncoveredBranches(t *testing.T) {
	t.Run("send login code invalid body", func(t *testing.T) {
		h := New(&MockService{})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/self-service/login/methods/link/send", bytes.NewBufferString("{"))
		h.HandleSendLoginCode(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}
	})

	t.Run("verify code default path service error", func(t *testing.T) {
		svc := &MockService{}
		svc.On("VerifyCode", mock.Anything, "user@example.com", "111111").
			Return("", (*uuid.UUID)(nil), ErrInvalidCode).Once()
		h := New(svc)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/self-service/login/methods/link/verify", mustJSON(t, VerifyCodeRequest{
			Email: "user@example.com",
			Code:  "111111",
		}))
		h.HandleVerifyCode(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}
		svc.AssertExpectations(t)
	})

	t.Run("verify magic link error and recovery branch", func(t *testing.T) {
		svc := &MockService{}
		svc.On("VerifyMagicLink", mock.Anything, "bad-token").
			Return("", (*uuid.UUID)(nil), errors.New("verify failed")).Once()
		svc.On("VerifyMagicLinkForType", mock.Anything, "recovery-token", store.CodeTypeRecovery).
			Return("recover@example.com", (*uuid.UUID)(nil), nil).Once()

		h := New(svc)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/self-service/login/methods/link/verify?token=bad-token", nil)
		h.HandleVerifyMagicLink(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/self-service/recovery/methods/link/verify?token=recovery-token", nil)
		h.HandleVerifyMagicLink(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
		svc.AssertExpectations(t)
	})

	t.Run("send recovery code identity lookup error", func(t *testing.T) {
		svc := &MockService{}
		ids := &mockIdentityStore{}
		ids.On("GetIdentityByEmail", mock.Anything, "user@example.com").
			Return((*uuid.UUID)(nil), errors.New("lookup failed")).Once()
		h := New(svc, WithIdentityStore(ids))
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/self-service/recovery/methods/link/send", mustJSON(t, SendCodeRequest{
			Email: "user@example.com",
		}))
		h.HandleSendRecoveryCode(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
		ids.AssertExpectations(t)
	})

	t.Run("identity context branch and strict decode trailing body", func(t *testing.T) {
		h := New(&MockService{})

		identityID := uuid.New()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req = req.WithContext(coresession.WithContext(req.Context(), &coresession.Context{IdentityID: identityID}))
		got, err := h.identityIDFromRequest(req)
		if err != nil || got != identityID {
			t.Fatalf("expected identity from session context, got %v err=%v", got, err)
		}

		rec := httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/self-service/login/methods/link/send",
			bytes.NewBufferString(`{"email":"user@example.com"}{"extra":true}`))
		h.HandleSendLoginCode(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}
	})
}
