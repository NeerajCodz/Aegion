package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	coresession "github.com/aegion/aegion/core/session"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/aegion/aegion/modules/password/service"
)

// MockService implements the service interface for testing
type MockService struct {
	mock.Mock
}

func (m *MockService) Register(ctx context.Context, identityID uuid.UUID, identifier, password string) error {
	args := m.Called(ctx, identityID, identifier, password)
	return args.Error(0)
}

func (m *MockService) Verify(ctx context.Context, identifier, password string) (uuid.UUID, error) {
	args := m.Called(ctx, identifier, password)
	return args.Get(0).(uuid.UUID), args.Error(1)
}

func (m *MockService) ChangePassword(ctx context.Context, identityID uuid.UUID, oldPassword, newPassword string) error {
	args := m.Called(ctx, identityID, oldPassword, newPassword)
	return args.Error(0)
}

func (m *MockService) ValidatePassword(ctx context.Context, password, identifier string) error {
	args := m.Called(ctx, password, identifier)
	return args.Error(0)
}

func registerRequest(email, password string) RegisterRequest {
	var req RegisterRequest
	req.Traits.Email = email
	req.Password = password
	return req
}

func (m *MockService) Delete(ctx context.Context, identityID string) error {
	args := m.Called(ctx, identityID)
	return args.Error(0)
}

func (m *MockService) ResetPassword(ctx context.Context, identityID, newPassword string) error {
	args := m.Called(ctx, identityID, newPassword)
	return args.Error(0)
}

type MockIdentityStore struct {
	mock.Mock
}

func (m *MockIdentityStore) CreateIdentity(ctx context.Context, traits map[string]interface{}) (uuid.UUID, error) {
	args := m.Called(ctx, traits)
	return args.Get(0).(uuid.UUID), args.Error(1)
}

type MockSessionManager struct {
	mock.Mock
}

func (m *MockSessionManager) Create(ctx context.Context, identityID uuid.UUID, method coresession.AuthMethod, device coresession.DeviceInfo) (*coresession.Session, error) {
	args := m.Called(ctx, identityID, method, device)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*coresession.Session), args.Error(1)
}

func (m *MockSessionManager) SetCookie(w http.ResponseWriter, session *coresession.Session) {
	m.Called(w, session)
}

// Service errors for testing
var (
	ErrPasswordTooShort   = service.ErrPasswordTooShort
	ErrPasswordTooWeak    = service.ErrPasswordTooWeak
	ErrPasswordBreached   = service.ErrPasswordBreached
	ErrPasswordReused     = service.ErrPasswordReused
	ErrPasswordSimilar    = service.ErrPasswordSimilar
	ErrInvalidCredentials = service.ErrInvalidCredentials
	ErrIdentityNotFound   = service.ErrIdentityNotFound
)

func TestHandler_HandleRegistration(t *testing.T) {
	identityID := uuid.New()
	sessionID := uuid.New()

	tests := []struct {
		name           string
		body           interface{}
		setupMocks     func(*MockService, *MockIdentityStore, *MockSessionManager)
		expectedStatus int
		expectedError  string
	}{
		{
			name: "successful registration",
			body: registerRequest("user@example.com", "SecurePass123!"),
			setupMocks: func(service *MockService, identityStore *MockIdentityStore, sessionManager *MockSessionManager) {
				service.On("ValidatePassword", mock.Anything, "SecurePass123!", "user@example.com").Return(nil).Once()
				identityStore.On("CreateIdentity", mock.Anything, map[string]interface{}{"email": "user@example.com"}).
					Return(identityID, nil).Once()
				service.On("Register", mock.Anything, identityID, "user@example.com", "SecurePass123!").Return(nil).Once()
				sessionManager.On(
					"Create",
					mock.Anything,
					identityID,
					coresession.AuthMethodPassword,
					mock.AnythingOfType("session.DeviceInfo"),
				).Return(&coresession.Session{
					ID:         sessionID,
					IdentityID: identityID,
					AAL:        coresession.AAL1,
				}, nil).Once()
				sessionManager.On("SetCookie", mock.Anything, mock.MatchedBy(func(session *coresession.Session) bool {
					return session != nil && session.ID == sessionID
				})).Return().Once()
			},
			expectedStatus: http.StatusCreated,
		},
		{
			name:           "invalid JSON",
			body:           "invalid json",
			setupMocks:     func(service *MockService, identityStore *MockIdentityStore, sessionManager *MockSessionManager) {},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "invalid_request",
		},
		{
			name:           "missing email",
			body:           registerRequest("", "SecurePass123!"),
			setupMocks:     func(service *MockService, identityStore *MockIdentityStore, sessionManager *MockSessionManager) {},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "missing_email",
		},
		{
			name:           "missing password",
			body:           registerRequest("user@example.com", ""),
			setupMocks:     func(service *MockService, identityStore *MockIdentityStore, sessionManager *MockSessionManager) {},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "missing_password",
		},
		{
			name: "password too short",
			body: registerRequest("user@example.com", "weak"),
			setupMocks: func(service *MockService, identityStore *MockIdentityStore, sessionManager *MockSessionManager) {
				service.On("ValidatePassword", mock.Anything, "weak", "user@example.com").Return(ErrPasswordTooShort).Once()
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "password_too_short",
		},
		{
			name: "password too weak",
			body: registerRequest("user@example.com", "weakpassword"),
			setupMocks: func(service *MockService, identityStore *MockIdentityStore, sessionManager *MockSessionManager) {
				service.On("ValidatePassword", mock.Anything, "weakpassword", "user@example.com").Return(ErrPasswordTooWeak).Once()
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "password_too_weak",
		},
		{
			name: "password breached",
			body: registerRequest("user@example.com", "password123"),
			setupMocks: func(service *MockService, identityStore *MockIdentityStore, sessionManager *MockSessionManager) {
				service.On("ValidatePassword", mock.Anything, "password123", "user@example.com").Return(ErrPasswordBreached).Once()
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "password_breached",
		},
		{
			name: "password similar",
			body: registerRequest("user@example.com", "user123"),
			setupMocks: func(service *MockService, identityStore *MockIdentityStore, sessionManager *MockSessionManager) {
				service.On("ValidatePassword", mock.Anything, "user123", "user@example.com").Return(ErrPasswordSimilar).Once()
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "password_similar",
		},
		{
			name: "internal server error",
			body: registerRequest("user@example.com", "SecurePass123!"),
			setupMocks: func(service *MockService, identityStore *MockIdentityStore, sessionManager *MockSessionManager) {
				service.On("ValidatePassword", mock.Anything, "SecurePass123!", "user@example.com").Return(nil).Once()
				identityStore.On("CreateIdentity", mock.Anything, map[string]interface{}{"email": "user@example.com"}).
					Return(identityID, nil).Once()
				service.On("Register", mock.Anything, identityID, "user@example.com", "SecurePass123!").
					Return(errors.New("database error")).Once()
			},
			expectedStatus: http.StatusInternalServerError,
			expectedError:  "internal_error",
		},
		{
			name: "identity store failure",
			body: registerRequest("user@example.com", "SecurePass123!"),
			setupMocks: func(service *MockService, identityStore *MockIdentityStore, sessionManager *MockSessionManager) {
				service.On("ValidatePassword", mock.Anything, "SecurePass123!", "user@example.com").Return(nil).Once()
				identityStore.On("CreateIdentity", mock.Anything, map[string]interface{}{"email": "user@example.com"}).
					Return(uuid.Nil, errors.New("identity unavailable")).Once()
			},
			expectedStatus: http.StatusInternalServerError,
			expectedError:  "internal_error",
		},
		{
			name: "session creation failure",
			body: registerRequest("user@example.com", "SecurePass123!"),
			setupMocks: func(service *MockService, identityStore *MockIdentityStore, sessionManager *MockSessionManager) {
				service.On("ValidatePassword", mock.Anything, "SecurePass123!", "user@example.com").Return(nil).Once()
				identityStore.On("CreateIdentity", mock.Anything, map[string]interface{}{"email": "user@example.com"}).
					Return(identityID, nil).Once()
				service.On("Register", mock.Anything, identityID, "user@example.com", "SecurePass123!").Return(nil).Once()
				sessionManager.On(
					"Create",
					mock.Anything,
					identityID,
					coresession.AuthMethodPassword,
					mock.AnythingOfType("session.DeviceInfo"),
				).Return((*coresession.Session)(nil), errors.New("session unavailable")).Once()
			},
			expectedStatus: http.StatusInternalServerError,
			expectedError:  "internal_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &MockService{}
			identityStore := &MockIdentityStore{}
			sessionManager := &MockSessionManager{}
			handler := New(service, WithIdentityStore(identityStore), WithSessionManager(sessionManager))

			tt.setupMocks(service, identityStore, sessionManager)

			var body []byte
			var err error
			if str, ok := tt.body.(string); ok {
				body = []byte(str)
			} else {
				body, err = json.Marshal(tt.body)
				require.NoError(t, err)
			}

			req := httptest.NewRequest(http.MethodPost, "/register", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			handler.HandleRegistration(recorder, req)

			assert.Equal(t, tt.expectedStatus, recorder.Code)

			if tt.expectedError != "" {
				var response ErrorResponse
				err := json.NewDecoder(recorder.Body).Decode(&response)
				require.NoError(t, err)
				assert.Equal(t, tt.expectedError, response.Error.Status)
			} else if recorder.Code == http.StatusCreated {
				var response SuccessResponse
				err := json.NewDecoder(recorder.Body).Decode(&response)
				require.NoError(t, err)
				assert.Equal(t, identityID.String(), response.Identity.ID)
				assert.Equal(t, sessionID.String(), response.Session.ID)
				assert.Equal(t, identityID.String(), response.Session.IdentityID)
				assert.Equal(t, string(coresession.AAL1), response.Session.AAL)
			}

			service.AssertExpectations(t)
			identityStore.AssertExpectations(t)
			sessionManager.AssertExpectations(t)
		})
	}
}

func TestHandler_HandleLogin(t *testing.T) {
	identityID := uuid.New()
	sessionID := uuid.New()

	tests := []struct {
		name           string
		body           interface{}
		setupMocks     func(*MockService, *MockSessionManager)
		expectedStatus int
		expectedError  string
	}{
		{
			name: "successful login",
			body: LoginRequest{
				Identifier: "user@example.com",
				Password:   "correctpassword",
			},
			setupMocks: func(service *MockService, sessionManager *MockSessionManager) {
				service.On("Verify", mock.Anything, "user@example.com", "correctpassword").Return(identityID, nil).Once()
				sessionManager.On(
					"Create",
					mock.Anything,
					identityID,
					coresession.AuthMethodPassword,
					mock.AnythingOfType("session.DeviceInfo"),
				).Return(&coresession.Session{
					ID:         sessionID,
					IdentityID: identityID,
					AAL:        coresession.AAL1,
				}, nil).Once()
				sessionManager.On("SetCookie", mock.Anything, mock.MatchedBy(func(session *coresession.Session) bool {
					return session != nil && session.ID == sessionID
				})).Return().Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "invalid JSON",
			body:           "invalid json",
			setupMocks:     func(service *MockService, sessionManager *MockSessionManager) {},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "invalid_request",
		},
		{
			name: "missing identifier",
			body: LoginRequest{
				Password: "password",
			},
			setupMocks:     func(service *MockService, sessionManager *MockSessionManager) {},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "missing_credentials",
		},
		{
			name: "missing password",
			body: LoginRequest{
				Identifier: "user@example.com",
			},
			setupMocks:     func(service *MockService, sessionManager *MockSessionManager) {},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "missing_credentials",
		},
		{
			name: "invalid credentials",
			body: LoginRequest{
				Identifier: "user@example.com",
				Password:   "wrongpassword",
			},
			setupMocks: func(service *MockService, sessionManager *MockSessionManager) {
				service.On("Verify", mock.Anything, "user@example.com", "wrongpassword").Return(uuid.Nil, ErrInvalidCredentials).Once()
			},
			expectedStatus: http.StatusUnauthorized,
			expectedError:  "invalid_credentials",
		},
		{
			name: "internal server error",
			body: LoginRequest{
				Identifier: "user@example.com",
				Password:   "password",
			},
			setupMocks: func(service *MockService, sessionManager *MockSessionManager) {
				service.On("Verify", mock.Anything, "user@example.com", "password").Return(uuid.Nil, errors.New("database error")).Once()
			},
			expectedStatus: http.StatusInternalServerError,
			expectedError:  "internal_error",
		},
		{
			name: "session creation failure",
			body: LoginRequest{
				Identifier: "user@example.com",
				Password:   "correctpassword",
			},
			setupMocks: func(service *MockService, sessionManager *MockSessionManager) {
				service.On("Verify", mock.Anything, "user@example.com", "correctpassword").Return(identityID, nil).Once()
				sessionManager.On(
					"Create",
					mock.Anything,
					identityID,
					coresession.AuthMethodPassword,
					mock.AnythingOfType("session.DeviceInfo"),
				).Return((*coresession.Session)(nil), errors.New("session unavailable")).Once()
			},
			expectedStatus: http.StatusInternalServerError,
			expectedError:  "internal_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &MockService{}
			sessionManager := &MockSessionManager{}
			handler := New(service, WithSessionManager(sessionManager))

			tt.setupMocks(service, sessionManager)

			var body []byte
			var err error
			if str, ok := tt.body.(string); ok {
				body = []byte(str)
			} else {
				body, err = json.Marshal(tt.body)
				require.NoError(t, err)
			}

			req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			handler.HandleLogin(recorder, req)

			assert.Equal(t, tt.expectedStatus, recorder.Code)

			if tt.expectedError != "" {
				var response ErrorResponse
				err := json.NewDecoder(recorder.Body).Decode(&response)
				require.NoError(t, err)
				assert.Equal(t, tt.expectedError, response.Error.Status)
			} else if recorder.Code == http.StatusOK {
				var response SuccessResponse
				err := json.NewDecoder(recorder.Body).Decode(&response)
				require.NoError(t, err)
				assert.Equal(t, sessionID.String(), response.Session.ID)
				assert.Equal(t, identityID.String(), response.Session.IdentityID)
				assert.Equal(t, string(coresession.AAL1), response.Session.AAL)
			}

			service.AssertExpectations(t)
			sessionManager.AssertExpectations(t)
		})
	}
}

func TestHandler_HandleRegistration_IntegrationUnavailable(t *testing.T) {
	service := &MockService{}
	service.On("ValidatePassword", mock.Anything, "SecurePass123!", "user@example.com").Return(nil).Once()
	handler := New(service)

	reqBody, err := json.Marshal(registerRequest("user@example.com", "SecurePass123!"))
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/register", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.HandleRegistration(recorder, req)

	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
	var response ErrorResponse
	require.NoError(t, json.NewDecoder(recorder.Body).Decode(&response))
	assert.Equal(t, "internal_error", response.Error.Status)
	service.AssertExpectations(t)
}

func TestHandler_HandleLogin_IntegrationUnavailable(t *testing.T) {
	service := &MockService{}
	identityID := uuid.New()
	service.On("Verify", mock.Anything, "user@example.com", "correctpassword").Return(identityID, nil).Once()
	handler := New(service)

	reqBody, err := json.Marshal(LoginRequest{
		Identifier: "user@example.com",
		Password:   "correctpassword",
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.HandleLogin(recorder, req)

	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
	var response ErrorResponse
	require.NoError(t, json.NewDecoder(recorder.Body).Decode(&response))
	assert.Equal(t, "internal_error", response.Error.Status)
	service.AssertExpectations(t)
}

func TestHandler_HandleChangePassword(t *testing.T) {
	identityID := uuid.New()
	identityIDStr := identityID.String()

	tests := []struct {
		name           string
		header         string
		body           interface{}
		setupMocks     func(*MockService)
		expectedStatus int
		expectedError  string
	}{
		{
			name:   "successful password change",
			header: identityIDStr,
			body: ChangePasswordRequest{
				OldPassword: "oldpassword",
				NewPassword: "NewSecurePass123!",
			},
			setupMocks: func(service *MockService) {
				service.On("ChangePassword", mock.Anything, identityID, "oldpassword", "NewSecurePass123!").Return(nil)
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing identity header",
			header:         "",
			body:           ChangePasswordRequest{},
			setupMocks:     func(service *MockService) {},
			expectedStatus: http.StatusUnauthorized,
			expectedError:  "unauthorized",
		},
		{
			name:           "invalid UUID in header",
			header:         "not-a-uuid",
			body:           ChangePasswordRequest{},
			setupMocks:     func(service *MockService) {},
			expectedStatus: http.StatusUnauthorized,
			expectedError:  "unauthorized",
		},
		{
			name:           "invalid JSON",
			header:         identityIDStr,
			body:           "invalid json",
			setupMocks:     func(service *MockService) {},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "invalid_request",
		},
		{
			name:   "missing old password",
			header: identityIDStr,
			body: ChangePasswordRequest{
				NewPassword: "NewSecurePass123!",
			},
			setupMocks: func(service *MockService) {
				service.On("ChangePassword", mock.Anything, identityID, "", "NewSecurePass123!").Return(ErrInvalidCredentials)
			},
			expectedStatus: http.StatusUnauthorized,
			expectedError:  "invalid_credentials",
		},
		{
			name:   "missing new password",
			header: identityIDStr,
			body: ChangePasswordRequest{
				OldPassword: "oldpassword",
			},
			setupMocks: func(service *MockService) {
				service.On("ChangePassword", mock.Anything, identityID, "oldpassword", "").Return(ErrPasswordTooShort)
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "password_too_short",
		},
		{
			name:   "identity not found",
			header: identityIDStr,
			body: ChangePasswordRequest{
				OldPassword: "oldpassword",
				NewPassword: "NewSecurePass123!",
			},
			setupMocks: func(service *MockService) {
				service.On("ChangePassword", mock.Anything, identityID, "oldpassword", "NewSecurePass123!").Return(ErrIdentityNotFound)
			},
			expectedStatus: http.StatusNotFound,
			expectedError:  "identity_not_found",
		},
		{
			name:   "invalid old password",
			header: identityIDStr,
			body: ChangePasswordRequest{
				OldPassword: "wrongpassword",
				NewPassword: "NewSecurePass123!",
			},
			setupMocks: func(service *MockService) {
				service.On("ChangePassword", mock.Anything, identityID, "wrongpassword", "NewSecurePass123!").Return(ErrInvalidCredentials)
			},
			expectedStatus: http.StatusUnauthorized,
			expectedError:  "invalid_credentials",
		},
		{
			name:   "password reused",
			header: identityIDStr,
			body: ChangePasswordRequest{
				OldPassword: "oldpassword",
				NewPassword: "oldpassword",
			},
			setupMocks: func(service *MockService) {
				service.On("ChangePassword", mock.Anything, identityID, "oldpassword", "oldpassword").Return(ErrPasswordReused)
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "password_reused",
		},
		{
			name:   "internal server error",
			header: identityIDStr,
			body: ChangePasswordRequest{
				OldPassword: "oldpassword",
				NewPassword: "NewSecurePass123!",
			},
			setupMocks: func(service *MockService) {
				service.On("ChangePassword", mock.Anything, identityID, "oldpassword", "NewSecurePass123!").Return(errors.New("database error"))
			},
			expectedStatus: http.StatusInternalServerError,
			expectedError:  "internal_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &MockService{}
			handler := New(service, WithLegacyIdentityHeaderAuth(true))

			tt.setupMocks(service)

			var body []byte
			var err error
			if str, ok := tt.body.(string); ok {
				body = []byte(str)
			} else {
				body, err = json.Marshal(tt.body)
				require.NoError(t, err)
			}

			req := httptest.NewRequest(http.MethodPost, "/change-password", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			if tt.header != "" {
				req.Header.Set("X-Aegion-Session-Identity-ID", tt.header)
			}
			recorder := httptest.NewRecorder()

			handler.HandleChangePassword(recorder, req)

			assert.Equal(t, tt.expectedStatus, recorder.Code)

			if tt.expectedError != "" {
				var response ErrorResponse
				err := json.NewDecoder(recorder.Body).Decode(&response)
				require.NoError(t, err)
				assert.Equal(t, tt.expectedError, response.Error.Status)
			}

			service.AssertExpectations(t)
		})
	}
}

func TestHandler_handleServiceError(t *testing.T) {
	tests := []struct {
		name           string
		err            error
		expectedStatus int
		expectedCode   string
	}{
		{
			name:           "password too short",
			err:            ErrPasswordTooShort,
			expectedStatus: http.StatusBadRequest,
			expectedCode:   "password_too_short",
		},
		{
			name:           "password too weak",
			err:            ErrPasswordTooWeak,
			expectedStatus: http.StatusBadRequest,
			expectedCode:   "password_too_weak",
		},
		{
			name:           "password breached",
			err:            ErrPasswordBreached,
			expectedStatus: http.StatusBadRequest,
			expectedCode:   "password_breached",
		},
		{
			name:           "password reused",
			err:            ErrPasswordReused,
			expectedStatus: http.StatusBadRequest,
			expectedCode:   "password_reused",
		},
		{
			name:           "password similar",
			err:            ErrPasswordSimilar,
			expectedStatus: http.StatusBadRequest,
			expectedCode:   "password_similar",
		},
		{
			name:           "invalid credentials",
			err:            ErrInvalidCredentials,
			expectedStatus: http.StatusUnauthorized,
			expectedCode:   "invalid_credentials",
		},
		{
			name:           "identity not found",
			err:            ErrIdentityNotFound,
			expectedStatus: http.StatusNotFound,
			expectedCode:   "identity_not_found",
		},
		{
			name:           "unknown error",
			err:            errors.New("unknown error"),
			expectedStatus: http.StatusInternalServerError,
			expectedCode:   "internal_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := &Handler{}
			recorder := httptest.NewRecorder()

			handler.handleServiceError(recorder, tt.err)

			assert.Equal(t, tt.expectedStatus, recorder.Code)

			var response ErrorResponse
			err := json.NewDecoder(recorder.Body).Decode(&response)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedCode, response.Error.Status)
		})
	}
}

func TestHandler_EdgeCases(t *testing.T) {
	t.Run("empty request body", func(t *testing.T) {
		service := &MockService{}
		handler := &Handler{service: service}

		req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(""))
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()

		handler.HandleRegistration(recorder, req)

		assert.Equal(t, http.StatusBadRequest, recorder.Code)
	})

	t.Run("malformed UUID in header", func(t *testing.T) {
		service := &MockService{}
		handler := New(service, WithLegacyIdentityHeaderAuth(true))

		body := ChangePasswordRequest{
			OldPassword: "old",
			NewPassword: "new",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/change-password", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Aegion-Session-Identity-ID", "malformed-uuid")
		recorder := httptest.NewRecorder()

		handler.HandleChangePassword(recorder, req)

		assert.Equal(t, http.StatusUnauthorized, recorder.Code)

		var response ErrorResponse
		err := json.NewDecoder(recorder.Body).Decode(&response)
		require.NoError(t, err)
		assert.Equal(t, "unauthorized", response.Error.Status)
	})

	t.Run("missing content type", func(t *testing.T) {
		service := &MockService{}
		identityStore := &MockIdentityStore{}
		sessionManager := &MockSessionManager{}
		handler := New(service, WithIdentityStore(identityStore), WithSessionManager(sessionManager))

		identityID := uuid.New()
		sessionID := uuid.New()
		service.On("ValidatePassword", mock.Anything, "SecurePass123!", "user@example.com").Return(nil).Once()
		identityStore.On("CreateIdentity", mock.Anything, map[string]interface{}{"email": "user@example.com"}).
			Return(identityID, nil).Once()
		service.On("Register", mock.Anything, identityID, "user@example.com", "SecurePass123!").Return(nil).Once()
		sessionManager.On(
			"Create",
			mock.Anything,
			identityID,
			coresession.AuthMethodPassword,
			mock.AnythingOfType("session.DeviceInfo"),
		).Return(&coresession.Session{
			ID:         sessionID,
			IdentityID: identityID,
			AAL:        coresession.AAL1,
		}, nil).Once()
		sessionManager.On("SetCookie", mock.Anything, mock.AnythingOfType("*session.Session")).Return().Once()

		body := registerRequest("user@example.com", "SecurePass123!")
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/register", bytes.NewReader(bodyBytes))
		// Don't set Content-Type header
		recorder := httptest.NewRecorder()

		handler.HandleRegistration(recorder, req)

		assert.Equal(t, http.StatusCreated, recorder.Code)
		service.AssertExpectations(t)
		identityStore.AssertExpectations(t)
		sessionManager.AssertExpectations(t)
	})

	t.Run("change password accepts X-User-ID header", func(t *testing.T) {
		service := &MockService{}
		handler := New(service, WithLegacyIdentityHeaderAuth(true))
		identityID := uuid.New()

		body := ChangePasswordRequest{
			OldPassword: "oldpassword",
			NewPassword: "newpassword",
		}
		bodyBytes, _ := json.Marshal(body)

		service.On("ChangePassword", mock.Anything, identityID, "oldpassword", "newpassword").Return(nil).Once()

		req := httptest.NewRequest(http.MethodPost, "/change-password", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-User-ID", identityID.String())
		recorder := httptest.NewRecorder()

		handler.HandleChangePassword(recorder, req)
		assert.Equal(t, http.StatusOK, recorder.Code)
		service.AssertExpectations(t)
	})

	t.Run("change password resolves identity from session context", func(t *testing.T) {
		service := &MockService{}
		handler := &Handler{service: service}
		identityID := uuid.New()

		body := ChangePasswordRequest{
			OldPassword: "oldpassword",
			NewPassword: "newpassword",
		}
		bodyBytes, _ := json.Marshal(body)

		service.On("ChangePassword", mock.Anything, identityID, "oldpassword", "newpassword").Return(nil).Once()

		req := httptest.NewRequest(http.MethodPost, "/change-password", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(coresession.WithContext(req.Context(), &coresession.Context{IdentityID: identityID}))
		recorder := httptest.NewRecorder()

		handler.HandleChangePassword(recorder, req)
		assert.Equal(t, http.StatusOK, recorder.Code)
		service.AssertExpectations(t)
	})

	t.Run("change password rejects unsigned identity headers by default", func(t *testing.T) {
		service := &MockService{}
		handler := New(service)
		identityID := uuid.New()

		body := ChangePasswordRequest{
			OldPassword: "oldpassword",
			NewPassword: "newpassword",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/change-password", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-User-ID", identityID.String())
		recorder := httptest.NewRecorder()

		handler.HandleChangePassword(recorder, req)
		assert.Equal(t, http.StatusUnauthorized, recorder.Code)
	})

	t.Run("change password accepts signed session headers", func(t *testing.T) {
		service := &MockService{}
		secret := []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
		handler := New(service, WithSessionHeaderSecret(secret))
		identityID := uuid.New()
		sessionID := uuid.New()

		body := ChangePasswordRequest{
			OldPassword: "oldpassword",
			NewPassword: "newpassword",
		}
		bodyBytes, _ := json.Marshal(body)

		service.On("ChangePassword", mock.Anything, identityID, "oldpassword", "newpassword").Return(nil).Once()

		req := httptest.NewRequest(http.MethodPost, "/change-password", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		coresession.InjectHeaders(req, &coresession.Session{
			ID:         sessionID,
			IdentityID: identityID,
			AAL:        coresession.AAL1,
		}, secret)
		recorder := httptest.NewRecorder()

		handler.HandleChangePassword(recorder, req)
		assert.Equal(t, http.StatusOK, recorder.Code)
		service.AssertExpectations(t)
	})

	t.Run("registration rejects unknown fields", func(t *testing.T) {
		service := &MockService{}
		handler := New(service)
		req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(`{"traits":{"email":"user@example.com","x":"y"},"password":"SecurePass123!"}`))
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()

		handler.HandleRegistration(recorder, req)
		assert.Equal(t, http.StatusBadRequest, recorder.Code)
	})
}

// Test concurrent request handling
func TestHandler_Concurrency(t *testing.T) {
	service := &MockService{}
	identityStore := &MockIdentityStore{}
	sessionManager := &MockSessionManager{}
	handler := New(service, WithIdentityStore(identityStore), WithSessionManager(sessionManager))

	identityID := uuid.New()
	sessionID := uuid.New()

	// Setup mock for multiple concurrent calls
	identityStore.On("CreateIdentity", mock.Anything, mock.Anything).Return(identityID, nil).Times(10)
	service.On("ValidatePassword", mock.Anything, mock.AnythingOfType("string"), mock.AnythingOfType("string")).Return(nil).Times(10)
	service.On("Register", mock.Anything, identityID, mock.AnythingOfType("string"), mock.AnythingOfType("string")).Return(nil).Times(10)
	sessionManager.On(
		"Create",
		mock.Anything,
		identityID,
		coresession.AuthMethodPassword,
		mock.AnythingOfType("session.DeviceInfo"),
	).Return(&coresession.Session{
		ID:         sessionID,
		IdentityID: identityID,
		AAL:        coresession.AAL1,
	}, nil).Times(10)
	sessionManager.On("SetCookie", mock.Anything, mock.AnythingOfType("*session.Session")).Return().Times(10)

	// Run 10 concurrent registration requests
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func(id int) {
			body := registerRequest("user"+string(rune(id))+"@example.com", "SecurePass123!")
			bodyBytes, _ := json.Marshal(body)

			req := httptest.NewRequest(http.MethodPost, "/register", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			handler.HandleRegistration(recorder, req)

			assert.Equal(t, http.StatusCreated, recorder.Code)
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}

	service.AssertExpectations(t)
	identityStore.AssertExpectations(t)
	sessionManager.AssertExpectations(t)
}

// Benchmark handler performance
func BenchmarkHandleRegistration(b *testing.B) {
	service := &MockService{}
	identityStore := &MockIdentityStore{}
	sessionManager := &MockSessionManager{}
	handler := New(service, WithIdentityStore(identityStore), WithSessionManager(sessionManager))

	identityID := uuid.New()
	sessionID := uuid.New()
	identityStore.On("CreateIdentity", mock.Anything, mock.Anything).Return(identityID, nil)
	service.On("ValidatePassword", mock.Anything, mock.AnythingOfType("string"), mock.AnythingOfType("string")).Return(nil)
	service.On("Register", mock.Anything, identityID, mock.AnythingOfType("string"), mock.AnythingOfType("string")).Return(nil)
	sessionManager.On(
		"Create",
		mock.Anything,
		identityID,
		coresession.AuthMethodPassword,
		mock.AnythingOfType("session.DeviceInfo"),
	).Return(&coresession.Session{
		ID:         sessionID,
		IdentityID: identityID,
		AAL:        coresession.AAL1,
	}, nil)
	sessionManager.On("SetCookie", mock.Anything, mock.AnythingOfType("*session.Session")).Return()

	body := registerRequest("user@example.com", "SecurePass123!")
	bodyBytes, _ := json.Marshal(body)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/register", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()

		handler.HandleRegistration(recorder, req)
	}
}
