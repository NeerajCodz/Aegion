package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/mock"
)

func TestNew_ConstructorSetsService(t *testing.T) {
	svc := &MockService{}
	h := New(svc)

	if h == nil {
		t.Fatalf("expected non-nil handler")
		return
	}
	if h.service != svc {
		t.Fatalf("expected handler service to match constructor argument")
	}
}

func TestHandleVerifyMagicLink_NewIdentityPath(t *testing.T) {
	svc := &MockService{}
	svc.On("VerifyMagicLink", mock.Anything, "token-123").Return("new@example.com", nil, nil).Once()

	h := New(svc)
	req := httptest.NewRequest(http.MethodGet, "/verify?token=token-123", nil)
	rec := httptest.NewRecorder()

	h.HandleVerifyMagicLink(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var resp SuccessResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Message == "" {
		t.Fatalf("expected response message for nil identity path")
	}
}

func TestHandleSendRecoveryCode_ErrorBranches(t *testing.T) {
	h := New(&MockService{})

	t.Run("invalid JSON", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/recover", bytes.NewBufferString(`{invalid`))
		rec := httptest.NewRecorder()

		h.HandleSendRecoveryCode(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", rec.Code)
		}
	})

	t.Run("missing email", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/recover", bytes.NewBufferString(`{"email":""}`))
		rec := httptest.NewRecorder()

		h.HandleSendRecoveryCode(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", rec.Code)
		}
	})
}
