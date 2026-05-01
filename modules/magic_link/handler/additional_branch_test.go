package handler

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	coresession "github.com/aegion/aegion/core/session"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func TestHandleVerifyCode_AdditionalBranches(t *testing.T) {
	h := New(&MockService{})

	t.Run("invalid json and missing fields", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/self-service/login/methods/link/verify", bytes.NewBufferString("{"))
		h.HandleVerifyCode(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/self-service/login/methods/link/verify", mustJSON(t, VerifyCodeRequest{Email: "user@example.com"}))
		h.HandleVerifyCode(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}
	})

	t.Run("verification and recovery service errors", func(t *testing.T) {
		svc := &MockService{}
		svc.On("VerifyVerificationCode", mock.Anything, "user@example.com", "111111").Return((*uuid.UUID)(nil), ErrInvalidCode).Once()
		svc.On("VerifyRecoveryCode", mock.Anything, "user@example.com", "222222").Return((*uuid.UUID)(nil), errors.New("recover failed")).Once()

		h := New(svc)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/self-service/verification/methods/link/verify", mustJSON(t, VerifyCodeRequest{
			Email: "user@example.com",
			Code:  "111111",
		}))
		h.HandleVerifyCode(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/self-service/recovery/methods/link/verify", mustJSON(t, VerifyCodeRequest{
			Email: "user@example.com",
			Code:  "222222",
		}))
		h.HandleVerifyCode(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
		svc.AssertExpectations(t)
	})
}

func TestHandleSendVerificationCode_AdditionalBranches(t *testing.T) {
	h := New(&MockService{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/self-service/verification/methods/link/send", bytes.NewBufferString("{"))
	h.HandleSendVerificationCode(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/self-service/verification/methods/link/send", mustJSON(t, SendCodeRequest{}))
	h.HandleSendVerificationCode(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
	}

	identityID := uuid.New()
	svc := &MockService{}
	svc.On("SendVerificationCode", mock.Anything, "user@example.com", identityID).Return(errors.New("send failed")).Once()
	h = New(svc, WithLegacyIdentityHeaderAuth(true))
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/self-service/verification/methods/link/send", mustJSON(t, SendCodeRequest{Email: "user@example.com"}))
	req.Header.Set("X-User-ID", identityID.String())
	h.HandleSendVerificationCode(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
	}
	svc.AssertExpectations(t)
}

func TestVerificationAndSessionSuccessHelpers_Branches(t *testing.T) {
	t.Run("verification success helper handles nil and store failure", func(t *testing.T) {
		h := New(&MockService{})

		rec := httptest.NewRecorder()
		h.handleVerificationSuccess(t.Context(), rec, "user@example.com", nil)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		identityID := uuid.New()
		store := &mockIdentityStore{}
		store.On("MarkEmailVerified", mock.Anything, identityID, "user@example.com").Return(errors.New("mark failed")).Once()
		h = New(&MockService{}, WithIdentityStore(store))
		rec = httptest.NewRecorder()
		h.handleVerificationSuccess(t.Context(), rec, "user@example.com", &identityID)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
		store.AssertExpectations(t)
	})

	t.Run("session success helper handles identity and session errors", func(t *testing.T) {
		store := &mockIdentityStore{}
		store.On("GetIdentityByEmail", mock.Anything, "user@example.com").Return((*uuid.UUID)(nil), errors.New("lookup failed")).Once()
		h := New(&MockService{}, WithIdentityStore(store))
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/verify", nil)
		h.handleSessionVerificationSuccess(rec, req, "user@example.com", nil)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
		store.AssertExpectations(t)

		identityID := uuid.New()
		sessions := &mockSessionManager{}
		sessions.On("Create", mock.Anything, identityID, coresession.AuthMethodMagicLink, mock.Anything).
			Return((*coresession.Session)(nil), errors.New("create failed")).Once()
		h = New(&MockService{}, WithSessionManager(sessions))
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/verify", nil)
		h.handleSessionVerificationSuccess(rec, req, "user@example.com", &identityID)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
		sessions.AssertExpectations(t)

		sessions = &mockSessionManager{}
		sessions.On("Create", mock.Anything, identityID, coresession.AuthMethodMagicLink, mock.Anything).
			Return((*coresession.Session)(nil), nil).Once()
		h = New(&MockService{}, WithSessionManager(sessions))
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/verify", nil)
		h.handleSessionVerificationSuccess(rec, req, "user@example.com", &identityID)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
		sessions.AssertExpectations(t)
	})
}

func TestRequestIP_AdditionalBranches(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Real-IP", "203.0.113.10")
	if got := requestIP(req); got != "203.0.113.10" {
		t.Fatalf("expected x-real-ip, got %q", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "198.51.100.20:443"
	if got := requestIP(req); got != "198.51.100.20" {
		t.Fatalf("expected host from remote addr, got %q", got)
	}
}
