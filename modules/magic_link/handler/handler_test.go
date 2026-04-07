package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	coresession "github.com/aegion/aegion/core/session"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/aegion/aegion/modules/magic_link/service"
	"github.com/aegion/aegion/modules/magic_link/store"
)

// MockService implements the handler service interface for testing.
type MockService struct {
	mock.Mock
}

func (m *MockService) SendLoginCode(ctx context.Context, email string) error {
	args := m.Called(ctx, email)
	return args.Error(0)
}

func (m *MockService) VerifyCode(ctx context.Context, email, otpCode string) (string, *uuid.UUID, error) {
	args := m.Called(ctx, email, otpCode)
	var id *uuid.UUID
	if args.Get(1) != nil {
		id = args.Get(1).(*uuid.UUID)
	}
	return args.String(0), id, args.Error(2)
}

func (m *MockService) VerifyMagicLink(ctx context.Context, token string) (string, *uuid.UUID, error) {
	args := m.Called(ctx, token)
	var id *uuid.UUID
	if args.Get(1) != nil {
		id = args.Get(1).(*uuid.UUID)
	}
	return args.String(0), id, args.Error(2)
}

func (m *MockService) VerifyMagicLinkForType(ctx context.Context, token string, expectedType store.CodeType) (string, *uuid.UUID, error) {
	args := m.Called(ctx, token, expectedType)
	var id *uuid.UUID
	if args.Get(1) != nil {
		id = args.Get(1).(*uuid.UUID)
	}
	return args.String(0), id, args.Error(2)
}

func (m *MockService) SendVerificationCode(ctx context.Context, email string, identityID uuid.UUID) error {
	args := m.Called(ctx, email, identityID)
	return args.Error(0)
}

func (m *MockService) VerifyVerificationCode(ctx context.Context, email, otpCode string) (*uuid.UUID, error) {
	args := m.Called(ctx, email, otpCode)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*uuid.UUID), args.Error(1)
}

func (m *MockService) SendRecoveryCodeIfIdentityExists(ctx context.Context, email string, identityID *uuid.UUID) error {
	args := m.Called(ctx, email, identityID)
	return args.Error(0)
}

func (m *MockService) VerifyRecoveryCode(ctx context.Context, email, otpCode string) (*uuid.UUID, error) {
	args := m.Called(ctx, email, otpCode)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*uuid.UUID), args.Error(1)
}

type mockIdentityStore struct {
	mock.Mock
}

func (m *mockIdentityStore) GetIdentityByEmail(ctx context.Context, email string) (*uuid.UUID, error) {
	args := m.Called(ctx, email)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*uuid.UUID), args.Error(1)
}

func (m *mockIdentityStore) MarkEmailVerified(ctx context.Context, identityID uuid.UUID, email string) error {
	args := m.Called(ctx, identityID, email)
	return args.Error(0)
}

type mockSessionManager struct {
	mock.Mock
}

func (m *mockSessionManager) Create(ctx context.Context, identityID uuid.UUID, method coresession.AuthMethod, device coresession.DeviceInfo) (*coresession.Session, error) {
	args := m.Called(ctx, identityID, method, device)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*coresession.Session), args.Error(1)
}

func (m *mockSessionManager) SetCookie(w http.ResponseWriter, session *coresession.Session) {
	m.Called(w, session)
}

// Service errors for testing.
var (
	ErrInvalidCode    = service.ErrInvalidCode
	ErrRateLimited    = service.ErrRateLimited
	ErrRecipientEmpty = service.ErrRecipientEmpty
)

func mustJSON(t *testing.T, v interface{}) *bytes.Reader {
	t.Helper()
	body, err := json.Marshal(v)
	require.NoError(t, err)
	return bytes.NewReader(body)
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) ErrorResponse {
	t.Helper()
	var resp ErrorResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	return resp
}

func decodeSuccess(t *testing.T, rec *httptest.ResponseRecorder) SuccessResponse {
	t.Helper()
	var resp SuccessResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	return resp
}

func TestHandler_HandleSendLoginCode(t *testing.T) {
	t.Run("success returns enumeration-safe response", func(t *testing.T) {
		svc := &MockService{}
		svc.On("SendLoginCode", mock.Anything, "user@example.com").Return(nil).Once()

		h := New(svc)
		req := httptest.NewRequest(http.MethodPost, "/login", mustJSON(t, SendCodeRequest{Email: "user@example.com"}))
		rec := httptest.NewRecorder()

		h.HandleSendLoginCode(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		resp := decodeSuccess(t, rec)
		assert.Contains(t, resp.Message, "If an account exists")
		svc.AssertExpectations(t)
	})

	t.Run("rate limited", func(t *testing.T) {
		svc := &MockService{}
		svc.On("SendLoginCode", mock.Anything, "user@example.com").Return(ErrRateLimited).Once()

		h := New(svc)
		req := httptest.NewRequest(http.MethodPost, "/login", mustJSON(t, SendCodeRequest{Email: "user@example.com"}))
		rec := httptest.NewRecorder()

		h.HandleSendLoginCode(rec, req)

		assert.Equal(t, http.StatusTooManyRequests, rec.Code)
		errResp := decodeError(t, rec)
		assert.Equal(t, "rate_limited", errResp.Error.Status)
		svc.AssertExpectations(t)
	})

	t.Run("missing email", func(t *testing.T) {
		h := New(&MockService{})
		req := httptest.NewRequest(http.MethodPost, "/login", mustJSON(t, SendCodeRequest{}))
		rec := httptest.NewRecorder()

		h.HandleSendLoginCode(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		errResp := decodeError(t, rec)
		assert.Equal(t, "missing_email", errResp.Error.Status)
	})
}

func TestHandler_HandleVerifyCode_LoginFlow(t *testing.T) {
	t.Run("creates session when identity is available", func(t *testing.T) {
		identityID := uuid.New()
		sessionID := uuid.New()
		svc := &MockService{}
		sessions := &mockSessionManager{}

		svc.On("VerifyCode", mock.Anything, "user@example.com", "123456").
			Return("user@example.com", &identityID, nil).Once()
		sessions.On(
			"Create",
			mock.Anything,
			identityID,
			coresession.AuthMethodMagicLink,
			mock.MatchedBy(func(device coresession.DeviceInfo) bool {
				return device.UserAgent == "test-agent" && device.IPAddress == "203.0.113.50"
			}),
		).Return(&coresession.Session{
			ID:         sessionID,
			IdentityID: identityID,
			AAL:        coresession.AAL1,
		}, nil).Once()
		sessions.On("SetCookie", mock.Anything, mock.MatchedBy(func(session *coresession.Session) bool {
			return session != nil && session.ID == sessionID
		})).Return().Once()

		h := New(svc, WithSessionManager(sessions))
		req := httptest.NewRequest(http.MethodPost, "/self-service/login/methods/link/verify", mustJSON(t, VerifyCodeRequest{
			Email: "user@example.com",
			Code:  "123456",
		}))
		req.Header.Set("User-Agent", "test-agent")
		req.Header.Set("X-Forwarded-For", "203.0.113.50, 10.0.0.2")
		rec := httptest.NewRecorder()

		h.HandleVerifyCode(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		resp := decodeSuccess(t, rec)
		assert.Equal(t, identityID.String(), resp.Session.IdentityID)
		assert.Equal(t, sessionID.String(), resp.Session.ID)
		assert.Equal(t, string(coresession.AAL1), resp.Session.AAL)
		svc.AssertExpectations(t)
		sessions.AssertExpectations(t)
	})

	t.Run("resolves identity via core integration when code has no identity", func(t *testing.T) {
		resolvedID := uuid.New()
		svc := &MockService{}
		identityStore := &mockIdentityStore{}

		svc.On("VerifyCode", mock.Anything, "user@example.com", "654321").
			Return("user@example.com", (*uuid.UUID)(nil), nil).Once()
		identityStore.On("GetIdentityByEmail", mock.Anything, "user@example.com").
			Return(&resolvedID, nil).Once()

		h := New(svc, WithIdentityStore(identityStore))
		req := httptest.NewRequest(http.MethodPost, "/self-service/login/methods/link/verify", mustJSON(t, VerifyCodeRequest{
			Email: "user@example.com",
			Code:  "654321",
		}))
		rec := httptest.NewRecorder()

		h.HandleVerifyCode(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		resp := decodeSuccess(t, rec)
		assert.Equal(t, resolvedID.String(), resp.Session.IdentityID)
		assert.Equal(t, string(coresession.AAL1), resp.Session.AAL)
		svc.AssertExpectations(t)
		identityStore.AssertExpectations(t)
	})

	t.Run("unknown identity remains enumeration-safe", func(t *testing.T) {
		svc := &MockService{}
		identityStore := &mockIdentityStore{}

		svc.On("VerifyCode", mock.Anything, "missing@example.com", "222222").
			Return("missing@example.com", (*uuid.UUID)(nil), nil).Once()
		identityStore.On("GetIdentityByEmail", mock.Anything, "missing@example.com").
			Return((*uuid.UUID)(nil), nil).Once()

		h := New(svc, WithIdentityStore(identityStore))
		req := httptest.NewRequest(http.MethodPost, "/self-service/login/methods/link/verify", mustJSON(t, VerifyCodeRequest{
			Email: "missing@example.com",
			Code:  "222222",
		}))
		rec := httptest.NewRecorder()

		h.HandleVerifyCode(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		resp := decodeSuccess(t, rec)
		assert.Empty(t, resp.Session.IdentityID)
		assert.Contains(t, resp.Message, "verified")
		svc.AssertExpectations(t)
		identityStore.AssertExpectations(t)
	})
}

func TestHandler_HandleVerifyCode_FlowSpecificPaths(t *testing.T) {
	t.Run("verification path marks email verified", func(t *testing.T) {
		identityID := uuid.New()
		svc := &MockService{}
		identityStore := &mockIdentityStore{}

		svc.On("VerifyVerificationCode", mock.Anything, "user@example.com", "111111").
			Return(&identityID, nil).Once()
		identityStore.On("MarkEmailVerified", mock.Anything, identityID, "user@example.com").
			Return(nil).Once()

		h := New(svc, WithIdentityStore(identityStore))
		req := httptest.NewRequest(http.MethodPost, "/self-service/verification/methods/link/verify", mustJSON(t, VerifyCodeRequest{
			Email: "user@example.com",
			Code:  "111111",
		}))
		rec := httptest.NewRecorder()

		h.HandleVerifyCode(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		resp := decodeSuccess(t, rec)
		assert.Equal(t, "Verification complete.", resp.Message)
		svc.AssertExpectations(t)
		identityStore.AssertExpectations(t)
	})

	t.Run("recovery path creates session", func(t *testing.T) {
		identityID := uuid.New()
		sessionID := uuid.New()
		svc := &MockService{}
		sessions := &mockSessionManager{}

		svc.On("VerifyRecoveryCode", mock.Anything, "recover@example.com", "333333").
			Return(&identityID, nil).Once()
		sessions.On("Create", mock.Anything, identityID, coresession.AuthMethodMagicLink, mock.Anything).
			Return(&coresession.Session{
				ID:         sessionID,
				IdentityID: identityID,
				AAL:        coresession.AAL1,
			}, nil).Once()
		sessions.On("SetCookie", mock.Anything, mock.Anything).Return().Once()

		h := New(svc, WithSessionManager(sessions))
		req := httptest.NewRequest(http.MethodPost, "/self-service/recovery/methods/link/verify", mustJSON(t, VerifyCodeRequest{
			Email: "recover@example.com",
			Code:  "333333",
		}))
		rec := httptest.NewRecorder()

		h.HandleVerifyCode(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		resp := decodeSuccess(t, rec)
		assert.Equal(t, sessionID.String(), resp.Session.ID)
		assert.Equal(t, identityID.String(), resp.Session.IdentityID)
		svc.AssertExpectations(t)
		sessions.AssertExpectations(t)
	})
}

func TestHandler_HandleVerifyMagicLink(t *testing.T) {
	t.Run("login token verification uses login verifier", func(t *testing.T) {
		identityID := uuid.New()
		svc := &MockService{}
		svc.On("VerifyMagicLink", mock.Anything, "login-token").Return("user@example.com", &identityID, nil).Once()

		h := New(svc)
		req := httptest.NewRequest(http.MethodGet, "/self-service/login/methods/link/verify?token=login-token", nil)
		rec := httptest.NewRecorder()

		h.HandleVerifyMagicLink(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		resp := decodeSuccess(t, rec)
		assert.Equal(t, identityID.String(), resp.Session.IdentityID)
		svc.AssertExpectations(t)
	})

	t.Run("verification token enforces verification flow type", func(t *testing.T) {
		identityID := uuid.New()
		svc := &MockService{}
		identityStore := &mockIdentityStore{}
		svc.On("VerifyMagicLinkForType", mock.Anything, "verify-token", store.CodeTypeVerification).
			Return("user@example.com", &identityID, nil).Once()
		identityStore.On("MarkEmailVerified", mock.Anything, identityID, "user@example.com").
			Return(nil).Once()

		h := New(svc, WithIdentityStore(identityStore))
		req := httptest.NewRequest(http.MethodGet, "/self-service/verification/methods/link/verify?token=verify-token", nil)
		rec := httptest.NewRecorder()

		h.HandleVerifyMagicLink(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		resp := decodeSuccess(t, rec)
		assert.Equal(t, "Verification complete.", resp.Message)
		svc.AssertExpectations(t)
		identityStore.AssertExpectations(t)
	})

	t.Run("missing token", func(t *testing.T) {
		h := New(&MockService{})
		req := httptest.NewRequest(http.MethodGet, "/verify", nil)
		rec := httptest.NewRecorder()

		h.HandleVerifyMagicLink(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		errResp := decodeError(t, rec)
		assert.Equal(t, "missing_token", errResp.Error.Status)
	})
}

func TestHandler_HandleSendVerificationCode(t *testing.T) {
	t.Run("requires active session identity header", func(t *testing.T) {
		h := New(&MockService{})
		req := httptest.NewRequest(http.MethodPost, "/verification/send", mustJSON(t, SendCodeRequest{Email: "user@example.com"}))
		rec := httptest.NewRecorder()

		h.HandleSendVerificationCode(rec, req)

		assert.Equal(t, http.StatusUnauthorized, rec.Code)
		errResp := decodeError(t, rec)
		assert.Equal(t, "unauthorized", errResp.Error.Status)
	})

	t.Run("invalid identity header", func(t *testing.T) {
		h := New(&MockService{})
		req := httptest.NewRequest(http.MethodPost, "/verification/send", mustJSON(t, SendCodeRequest{Email: "user@example.com"}))
		req.Header.Set("X-User-ID", "not-a-uuid")
		rec := httptest.NewRecorder()

		h.HandleSendVerificationCode(rec, req)

		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("success", func(t *testing.T) {
		identityID := uuid.New()
		svc := &MockService{}
		svc.On("SendVerificationCode", mock.Anything, "user@example.com", identityID).Return(nil).Once()

		h := New(svc)
		req := httptest.NewRequest(http.MethodPost, "/verification/send", mustJSON(t, SendCodeRequest{Email: "user@example.com"}))
		req.Header.Set("X-User-ID", identityID.String())
		rec := httptest.NewRecorder()

		h.HandleSendVerificationCode(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		resp := decodeSuccess(t, rec)
		assert.Contains(t, resp.Message, "verification")
		svc.AssertExpectations(t)
	})
}

func TestHandler_HandleSendRecoveryCode(t *testing.T) {
	t.Run("unknown identity still returns success", func(t *testing.T) {
		svc := &MockService{}
		identityStore := &mockIdentityStore{}

		identityStore.On("GetIdentityByEmail", mock.Anything, "missing@example.com").
			Return((*uuid.UUID)(nil), nil).Once()
		svc.On("SendRecoveryCodeIfIdentityExists", mock.Anything, "missing@example.com", (*uuid.UUID)(nil)).
			Return(nil).Once()

		h := New(svc, WithIdentityStore(identityStore))
		req := httptest.NewRequest(http.MethodPost, "/recovery/send", mustJSON(t, SendCodeRequest{Email: "missing@example.com"}))
		rec := httptest.NewRecorder()

		h.HandleSendRecoveryCode(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		resp := decodeSuccess(t, rec)
		assert.Contains(t, resp.Message, "If an account exists")
		svc.AssertExpectations(t)
		identityStore.AssertExpectations(t)
	})

	t.Run("rate limited identity recovery request", func(t *testing.T) {
		identityID := uuid.New()
		svc := &MockService{}
		identityStore := &mockIdentityStore{}

		identityStore.On("GetIdentityByEmail", mock.Anything, "user@example.com").
			Return(&identityID, nil).Once()
		svc.On("SendRecoveryCodeIfIdentityExists", mock.Anything, "user@example.com", &identityID).
			Return(ErrRateLimited).Once()

		h := New(svc, WithIdentityStore(identityStore))
		req := httptest.NewRequest(http.MethodPost, "/recovery/send", mustJSON(t, SendCodeRequest{Email: "user@example.com"}))
		rec := httptest.NewRecorder()

		h.HandleSendRecoveryCode(rec, req)

		assert.Equal(t, http.StatusTooManyRequests, rec.Code)
		errResp := decodeError(t, rec)
		assert.Equal(t, "rate_limited", errResp.Error.Status)
		svc.AssertExpectations(t)
		identityStore.AssertExpectations(t)
	})
}

func TestHandler_ErrorMapping(t *testing.T) {
	svc := &MockService{}
	h := New(svc)

	t.Run("recipient empty", func(t *testing.T) {
		svc.On("SendLoginCode", mock.Anything, "user@example.com").Return(ErrRecipientEmpty).Once()

		req := httptest.NewRequest(http.MethodPost, "/login", mustJSON(t, SendCodeRequest{Email: "user@example.com"}))
		rec := httptest.NewRecorder()
		h.HandleSendLoginCode(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		errResp := decodeError(t, rec)
		assert.Equal(t, "missing_recipient", errResp.Error.Status)
	})

	t.Run("unknown internal error", func(t *testing.T) {
		svc.On("SendLoginCode", mock.Anything, "user2@example.com").Return(errors.New("db down")).Once()

		req := httptest.NewRequest(http.MethodPost, "/login", mustJSON(t, SendCodeRequest{Email: "user2@example.com"}))
		rec := httptest.NewRecorder()
		h.HandleSendLoginCode(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		errResp := decodeError(t, rec)
		assert.Equal(t, "internal_error", errResp.Error.Status)
	})

	svc.AssertExpectations(t)
}

func TestHandler_QueryParameterExtraction(t *testing.T) {
	svc := &MockService{}
	identityID := uuid.New()
	svc.On("VerifyMagicLink", mock.Anything, "token+with/special=chars").Return("user@example.com", &identityID, nil).Once()

	h := New(svc)
	req := httptest.NewRequest(http.MethodGet, "/verify?token="+url.QueryEscape("token+with/special=chars"), nil)
	rec := httptest.NewRecorder()

	h.HandleVerifyMagicLink(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	svc.AssertExpectations(t)
}
