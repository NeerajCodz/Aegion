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

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockService implements the service interface for testing
type MockService struct {
	mock.Mock
}

func (m *MockService) Register(ctx context.Context, identityID, identifier, password string) error {
	args := m.Called(ctx, identityID, identifier, password)
	return args.Error(0)
}

func (m *MockService) Verify(ctx context.Context, identifier, password string) (string, error) {
	args := m.Called(ctx, identifier, password)
	return args.String(0), args.Error(1)
}

func (m *MockService) ChangePassword(ctx context.Context, identityID, oldPassword, newPassword string) error {
	args := m.Called(ctx, identityID, oldPassword, newPassword)
	return args.Error(0)
}

func (m *MockService) ValidatePassword(ctx context.Context, password, identifier string) error {
	args := m.Called(ctx, password, identifier)
	return args.Error(0)
}

func (m *MockService) Delete(ctx context.Context, identityID string) error {
	args := m.Called(ctx, identityID)
	return args.Error(0)
}

func (m *MockService) ResetPassword(ctx context.Context, identityID, newPassword string) error {
	args := m.Called(ctx, identityID, newPassword)
	return args.Error(0)
}

// Service errors for testing
var (
	ErrPasswordTooShort   = errors.New("password_too_short")
	ErrPasswordTooWeak    = errors.New("password_too_weak")
	ErrPasswordBreached   = errors.New("password_breached")
	ErrPasswordReused     = errors.New("password_reused")
	ErrPasswordSimilar    = errors.New("password_similar")
	ErrInvalidCredentials = errors.New("invalid_credentials")
	ErrIdentityNotFound   = errors.New("identity_not_found")
)

func TestHandler_HandleRegistration(t *testing.T) {
	tests := []struct {
		name           string
		body           interface{}
		setupMocks     func(*MockService)
		expectedStatus int
		expectedError  string
	}{
		{
			name: "successful registration",
			body: RegisterRequest{
				Email:    "user@example.com",
				Password: "SecurePass123!",
			},
			setupMocks: func(service *MockService) {
				service.On("Register", mock.Anything, mock.AnythingOfType("string"), "user@example.com", "SecurePass123!").Return(nil)
			},
			expectedStatus: http.StatusCreated,
		},
		{
			name:           "invalid JSON",
			body:           "invalid json",
			setupMocks:     func(service *MockService) {},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "invalid_request",
		},
		{
			name: "missing email",
			body: RegisterRequest{
				Password: "SecurePass123!",
			},
			setupMocks:     func(service *MockService) {},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "missing_email",
		},
		{
			name: "missing password",
			body: RegisterRequest{
				Email: "user@example.com",
			},
			setupMocks:     func(service *MockService) {},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "missing_password",
		},
		{
			name: "password too short",
			body: RegisterRequest{
				Email:    "user@example.com",
				Password: "weak",
			},
			setupMocks: func(service *MockService) {
				service.On("Register", mock.Anything, mock.AnythingOfType("string"), "user@example.com", "weak").Return(ErrPasswordTooShort)
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "password_too_short",
		},
		{
			name: "password too weak",
			body: RegisterRequest{
				Email:    "user@example.com",
				Password: "weakpassword",
			},
			setupMocks: func(service *MockService) {
				service.On("Register", mock.Anything, mock.AnythingOfType("string"), "user@example.com", "weakpassword").Return(ErrPasswordTooWeak)
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "password_too_weak",
		},
		{
			name: "password breached",
			body: RegisterRequest{
				Email:    "user@example.com",
				Password: "password123",
			},
			setupMocks: func(service *MockService) {
				service.On("Register", mock.Anything, mock.AnythingOfType("string"), "user@example.com", "password123").Return(ErrPasswordBreached)
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "password_breached",
		},
		{
			name: "password similar",
			body: RegisterRequest{
				Email:    "user@example.com",
				Password: "user123",
			},
			setupMocks: func(service *MockService) {
				service.On("Register", mock.Anything, mock.AnythingOfType("string"), "user@example.com", "user123").Return(ErrPasswordSimilar)
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "password_similar",
		},
		{
			name: "internal server error",
			body: RegisterRequest{
				Email:    "user@example.com",
				Password: "SecurePass123!",
			},
			setupMocks: func(service *MockService) {
				service.On("Register", mock.Anything, mock.AnythingOfType("string"), "user@example.com", "SecurePass123!").Return(errors.New("database error"))
			},
			expectedStatus: http.StatusInternalServerError,
			expectedError:  "internal_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &MockService{}
			handler := &Handler{service: service}

			tt.setupMocks(service)

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
				assert.Equal(t, tt.expectedError, response.Error.Code)
			}

			service.AssertExpectations(t)
		})
	}
}

func TestHandler_HandleLogin(t *testing.T) {
	identityID := uuid.New().String()

	tests := []struct {
		name           string
		body           interface{}
		setupMocks     func(*MockService)
		expectedStatus int
		expectedError  string
	}{
		{
			name: "successful login",
			body: LoginRequest{
				Identifier: "user@example.com",
				Password:   "correctpassword",
			},
			setupMocks: func(service *MockService) {
				service.On("Verify", mock.Anything, "user@example.com", "correctpassword").Return(identityID, nil)
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "invalid JSON",
			body:           "invalid json",
			setupMocks:     func(service *MockService) {},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "invalid_request",
		},
		{
			name: "missing identifier",
			body: LoginRequest{
				Password: "password",
			},
			setupMocks:     func(service *MockService) {},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "missing_identifier",
		},
		{
			name: "missing password",
			body: LoginRequest{
				Identifier: "user@example.com",
			},
			setupMocks:     func(service *MockService) {},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "missing_password",
		},
		{
			name: "invalid credentials",
			body: LoginRequest{
				Identifier: "user@example.com",
				Password:   "wrongpassword",
			},
			setupMocks: func(service *MockService) {
				service.On("Verify", mock.Anything, "user@example.com", "wrongpassword").Return("", ErrInvalidCredentials)
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
			setupMocks: func(service *MockService) {
				service.On("Verify", mock.Anything, "user@example.com", "password").Return("", errors.New("database error"))
			},
			expectedStatus: http.StatusInternalServerError,
			expectedError:  "internal_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &MockService{}
			handler := &Handler{service: service}

			tt.setupMocks(service)

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
				assert.Equal(t, tt.expectedError, response.Error.Code)
			} else if recorder.Code == http.StatusOK {
				var response SuccessResponse
				err := json.NewDecoder(recorder.Body).Decode(&response)
				require.NoError(t, err)
				assert.NotNil(t, response.Session)
				assert.Equal(t, identityID, response.Session.Identity.ID)
			}

			service.AssertExpectations(t)
		})
	}
}

func TestHandler_HandleChangePassword(t *testing.T) {
	identityID := uuid.New().String()

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
			header: identityID,
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
			expectedStatus: http.StatusBadRequest,
			expectedError:  "missing_identity_id",
		},
		{
			name:           "invalid UUID in header",
			header:         "not-a-uuid",
			body:           ChangePasswordRequest{},
			setupMocks:     func(service *MockService) {},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "invalid_identity_id",
		},
		{
			name:           "invalid JSON",
			header:         identityID,
			body:           "invalid json",
			setupMocks:     func(service *MockService) {},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "invalid_request",
		},
		{
			name:   "missing old password",
			header: identityID,
			body: ChangePasswordRequest{
				NewPassword: "NewSecurePass123!",
			},
			setupMocks:     func(service *MockService) {},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "missing_old_password",
		},
		{
			name:   "missing new password",
			header: identityID,
			body: ChangePasswordRequest{
				OldPassword: "oldpassword",
			},
			setupMocks:     func(service *MockService) {},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "missing_new_password",
		},
		{
			name:   "identity not found",
			header: identityID,
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
			header: identityID,
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
			header: identityID,
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
			header: identityID,
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
			handler := &Handler{service: service}

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
				assert.Equal(t, tt.expectedError, response.Error.Code)
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
			assert.Equal(t, tt.expectedCode, response.Error.Code)
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
		handler := &Handler{service: service}

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

		assert.Equal(t, http.StatusBadRequest, recorder.Code)

		var response ErrorResponse
		err := json.NewDecoder(recorder.Body).Decode(&response)
		require.NoError(t, err)
		assert.Equal(t, "invalid_identity_id", response.Error.Code)
	})

	t.Run("missing content type", func(t *testing.T) {
		service := &MockService{}
		handler := &Handler{service: service}

		body := RegisterRequest{
			Email:    "user@example.com",
			Password: "SecurePass123!",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/register", bytes.NewReader(bodyBytes))
		// Don't set Content-Type header
		recorder := httptest.NewRecorder()

		handler.HandleRegistration(recorder, req)

		// Should still work, JSON decoding doesn't strictly require Content-Type
		// but let's verify the behavior
		assert.True(t, recorder.Code == http.StatusBadRequest || recorder.Code == http.StatusCreated)
	})
}

// Test concurrent request handling
func TestHandler_Concurrency(t *testing.T) {
	service := &MockService{}
	handler := &Handler{service: service}

	// Setup mock for multiple concurrent calls
	service.On("Register", mock.Anything, mock.AnythingOfType("string"), mock.AnythingOfType("string"), mock.AnythingOfType("string")).Return(nil).Times(10)

	// Run 10 concurrent registration requests
	done := make(chan bool, 10)
	
	for i := 0; i < 10; i++ {
		go func(id int) {
			body := RegisterRequest{
				Email:    "user" + string(rune(id)) + "@example.com",
				Password: "SecurePass123!",
			}
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
}

// Benchmark handler performance
func BenchmarkHandleRegistration(b *testing.B) {
	service := &MockService{}
	handler := &Handler{service: service}

	service.On("Register", mock.Anything, mock.AnythingOfType("string"), mock.AnythingOfType("string"), mock.AnythingOfType("string")).Return(nil)

	body := RegisterRequest{
		Email:    "user@example.com",
		Password: "SecurePass123!",
	}
	bodyBytes, _ := json.Marshal(body)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/register", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()

		handler.HandleRegistration(recorder, req)
	}
}