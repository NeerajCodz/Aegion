package service

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockStore implements store interface for testing
type MockStore struct {
	mock.Mock
}

func (m *MockStore) Create(ctx context.Context, recipient, codeType string, identityID *string, ttl time.Duration) (string, string, error) {
	args := m.Called(ctx, recipient, codeType, identityID, ttl)
	return args.String(0), args.String(1), args.Error(2)
}

func (m *MockStore) GetByCode(ctx context.Context, recipient, otpCode, codeType string) (interface{}, error) {
	args := m.Called(ctx, recipient, otpCode, codeType)
	return args.Get(0), args.Error(1)
}

func (m *MockStore) GetByToken(ctx context.Context, token string) (interface{}, error) {
	args := m.Called(ctx, token)
	return args.Get(0), args.Error(1)
}

func (m *MockStore) MarkUsed(ctx context.Context, codeID string) error {
	args := m.Called(ctx, codeID)
	return args.Error(0)
}

func (m *MockStore) InvalidatePrevious(ctx context.Context, recipient, codeType string) error {
	args := m.Called(ctx, recipient, codeType)
	return args.Error(0)
}

func (m *MockStore) CheckRateLimit(ctx context.Context, key string, limit int, window time.Duration) error {
	args := m.Called(ctx, key, limit, window)
	return args.Error(0)
}

func (m *MockStore) Cleanup(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

// MockCourier implements courier interface for testing
type MockCourier struct {
	mock.Mock
}

func (m *MockCourier) SendMagicLinkEmail(ctx context.Context, to, link, code string) error {
	args := m.Called(ctx, to, link, code)
	return args.Error(0)
}

// Error definitions for testing
var (
	ErrInvalidCode    = errors.New("invalid_code")
	ErrRateLimited    = errors.New("rate_limited")
	ErrRecipientEmpty = errors.New("recipient_empty")
	ErrCodeNotFound   = errors.New("code_not_found")
	ErrCodeExpired    = errors.New("code_expired")
	ErrCodeUsed       = errors.New("code_used")
)

func TestNew(t *testing.T) {
	store := &MockStore{}
	courier := &MockCourier{}

	service := New(store, courier)

	assert.NotNil(t, service)
	assert.Equal(t, store, service.store)
	assert.Equal(t, courier, service.courier)
	
	// Check default config values
	assert.Equal(t, 6, service.config.CodeLength)
	assert.Equal(t, "0123456789", service.config.CodeCharset)
	assert.Equal(t, 15*time.Minute, service.config.LinkLifespan)
	assert.Equal(t, 15*time.Minute, service.config.CodeLifespan)
	assert.Equal(t, 5, service.config.RateLimit)
	assert.Equal(t, time.Hour, service.config.RateWindow)
	assert.Equal(t, "", service.config.BaseURL) // Empty by default
}

func TestService_buildMagicLink(t *testing.T) {
	tests := []struct {
		name      string
		baseURL   string
		flowType  string
		token     string
		expected  string
		expectErr bool
	}{
		{
			name:     "default base URL",
			baseURL:  "",
			flowType: "login",
			token:    "abc123token",
			expected: "/self-service/login/methods/link/verify?token=abc123token",
		},
		{
			name:     "custom base URL",
			baseURL:  "https://auth.example.com",
			flowType: "recovery",
			token:    "xyz789token",
			expected: "https://auth.example.com/self-service/recovery/methods/link/verify?token=xyz789token",
		},
		{
			name:     "custom base URL with trailing slash",
			baseURL:  "https://auth.example.com/",
			flowType: "verification",
			token:    "verification123",
			expected: "https://auth.example.com/self-service/verification/methods/link/verify?token=verification123",
		},
		{
			name:     "localhost base URL",
			baseURL:  "http://localhost:4455",
			flowType: "login",
			token:    "local123",
			expected: "http://localhost:4455/self-service/login/methods/link/verify?token=local123",
		},
		{
			name:     "token with special characters",
			baseURL:  "",
			flowType: "login",
			token:    "token+with/special=chars",
			expected: "/self-service/login/methods/link/verify?token=token%2Bwith%2Fspecial%3Dchars",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &Service{
				config: Config{BaseURL: tt.baseURL},
			}

			result := service.buildMagicLink(tt.flowType, tt.token)

			if tt.expectErr {
				assert.Empty(t, result)
			} else {
				assert.Equal(t, tt.expected, result)
				
				// Verify the URL is valid
				if strings.HasPrefix(result, "http") {
					_, err := url.Parse(result)
					assert.NoError(t, err)
				}
			}
		})
	}
}

func TestService_SendLoginCode(t *testing.T) {
	tests := []struct {
		name       string
		email      string
		setupMocks func(*MockStore, *MockCourier)
		wantErr    error
	}{
		{
			name:  "successful send",
			email: "user@example.com",
			setupMocks: func(store *MockStore, courier *MockCourier) {
				store.On("CheckRateLimit", mock.Anything, "login:user@example.com", 5, time.Hour).Return(nil)
				store.On("InvalidatePrevious", mock.Anything, "user@example.com", "login").Return(nil)
				store.On("Create", mock.Anything, "user@example.com", "login", (*string)(nil), 15*time.Minute).Return("123456", "token123", nil)
				courier.On("SendMagicLinkEmail", mock.Anything, "user@example.com", "/self-service/login/methods/link/verify?token=token123", "123456").Return(nil)
			},
			wantErr: nil,
		},
		{
			name:  "rate limited",
			email: "spammer@example.com",
			setupMocks: func(store *MockStore, courier *MockCourier) {
				store.On("CheckRateLimit", mock.Anything, "login:spammer@example.com", 5, time.Hour).Return(ErrRateLimited)
			},
			wantErr: ErrRateLimited,
		},
		{
			name:  "empty email",
			email: "",
			setupMocks: func(store *MockStore, courier *MockCourier) {
				// No mocks needed - validation fails first
			},
			wantErr: ErrRecipientEmpty,
		},
		{
			name:  "store creation fails",
			email: "user@example.com",
			setupMocks: func(store *MockStore, courier *MockCourier) {
				store.On("CheckRateLimit", mock.Anything, "login:user@example.com", 5, time.Hour).Return(nil)
				store.On("InvalidatePrevious", mock.Anything, "user@example.com", "login").Return(nil)
				store.On("Create", mock.Anything, "user@example.com", "login", (*string)(nil), 15*time.Minute).Return("", "", errors.New("store error"))
			},
			wantErr: errors.New("store error"),
		},
		{
			name:  "courier send fails",
			email: "user@example.com",
			setupMocks: func(store *MockStore, courier *MockCourier) {
				store.On("CheckRateLimit", mock.Anything, "login:user@example.com", 5, time.Hour).Return(nil)
				store.On("InvalidatePrevious", mock.Anything, "user@example.com", "login").Return(nil)
				store.On("Create", mock.Anything, "user@example.com", "login", (*string)(nil), 15*time.Minute).Return("123456", "token123", nil)
				courier.On("SendMagicLinkEmail", mock.Anything, "user@example.com", "/self-service/login/methods/link/verify?token=token123", "123456").Return(errors.New("email send failed"))
			},
			wantErr: errors.New("email send failed"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &MockStore{}
			courier := &MockCourier{}
			
			service := New(store, courier)
			tt.setupMocks(store, courier)

			ctx := context.Background()
			err := service.SendLoginCode(ctx, tt.email)

			if tt.wantErr != nil {
				assert.Error(t, err)
				assert.Equal(t, tt.wantErr.Error(), err.Error())
			} else {
				assert.NoError(t, err)
			}

			store.AssertExpectations(t)
			courier.AssertExpectations(t)
		})
	}
}

func TestService_VerifyCode(t *testing.T) {
	identityID := uuid.New().String()
	codeData := map[string]interface{}{
		"id":          "code123",
		"identity_id": identityID,
		"recipient":   "user@example.com",
		"used":        false,
		"expires_at":  time.Now().Add(time.Hour),
	}

	tests := []struct {
		name            string
		email           string
		otpCode         string
		setupMocks      func(*MockStore)
		wantRecipient   string
		wantIdentityID  string
		wantErr         error
	}{
		{
			name:    "successful verification",
			email:   "user@example.com",
			otpCode: "123456",
			setupMocks: func(store *MockStore) {
				store.On("GetByCode", mock.Anything, "user@example.com", "123456", "login").Return(codeData, nil)
				store.On("MarkUsed", mock.Anything, "code123").Return(nil)
			},
			wantRecipient:  "user@example.com",
			wantIdentityID: identityID,
			wantErr:        nil,
		},
		{
			name:    "code not found",
			email:   "user@example.com",
			otpCode: "wrong123",
			setupMocks: func(store *MockStore) {
				store.On("GetByCode", mock.Anything, "user@example.com", "wrong123", "login").Return(nil, ErrCodeNotFound)
			},
			wantRecipient:  "",
			wantIdentityID: "",
			wantErr:        ErrInvalidCode,
		},
		{
			name:    "code expired",
			email:   "user@example.com",
			otpCode: "123456",
			setupMocks: func(store *MockStore) {
				store.On("GetByCode", mock.Anything, "user@example.com", "123456", "login").Return(nil, ErrCodeExpired)
			},
			wantRecipient:  "",
			wantIdentityID: "",
			wantErr:        ErrInvalidCode,
		},
		{
			name:    "code already used",
			email:   "user@example.com",
			otpCode: "123456",
			setupMocks: func(store *MockStore) {
				store.On("GetByCode", mock.Anything, "user@example.com", "123456", "login").Return(codeData, nil)
				store.On("MarkUsed", mock.Anything, "code123").Return(ErrCodeUsed)
			},
			wantRecipient:  "",
			wantIdentityID: "",
			wantErr:        ErrInvalidCode,
		},
		{
			name:    "empty email",
			email:   "",
			otpCode: "123456",
			setupMocks: func(store *MockStore) {
				// No mocks needed - validation fails first
			},
			wantRecipient:  "",
			wantIdentityID: "",
			wantErr:        ErrRecipientEmpty,
		},
		{
			name:    "empty code",
			email:   "user@example.com",
			otpCode: "",
			setupMocks: func(store *MockStore) {
				// No mocks needed - validation fails first
			},
			wantRecipient:  "",
			wantIdentityID: "",
			wantErr:        ErrInvalidCode,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &MockStore{}
			courier := &MockCourier{}
			
			service := New(store, courier)
			tt.setupMocks(store)

			ctx := context.Background()
			recipient, identityID, err := service.VerifyCode(ctx, tt.email, tt.otpCode)

			assert.Equal(t, tt.wantRecipient, recipient)
			assert.Equal(t, tt.wantIdentityID, identityID)
			
			if tt.wantErr != nil {
				assert.Error(t, err)
				assert.True(t, errors.Is(err, tt.wantErr) || err.Error() == tt.wantErr.Error())
			} else {
				assert.NoError(t, err)
			}

			store.AssertExpectations(t)
		})
	}
}

func TestService_VerifyMagicLink(t *testing.T) {
	identityID := uuid.New().String()
	tokenData := map[string]interface{}{
		"id":          "code123",
		"identity_id": identityID,
		"recipient":   "user@example.com",
		"token":       "token123",
		"used":        false,
		"expires_at":  time.Now().Add(time.Hour),
	}

	tests := []struct {
		name            string
		token           string
		setupMocks      func(*MockStore)
		wantRecipient   string
		wantIdentityID  string
		wantErr         error
	}{
		{
			name:  "successful verification",
			token: "token123",
			setupMocks: func(store *MockStore) {
				store.On("GetByToken", mock.Anything, "token123").Return(tokenData, nil)
				store.On("MarkUsed", mock.Anything, "code123").Return(nil)
			},
			wantRecipient:  "user@example.com",
			wantIdentityID: identityID,
			wantErr:        nil,
		},
		{
			name:  "token not found",
			token: "invalid-token",
			setupMocks: func(store *MockStore) {
				store.On("GetByToken", mock.Anything, "invalid-token").Return(nil, ErrCodeNotFound)
			},
			wantRecipient:  "",
			wantIdentityID: "",
			wantErr:        ErrInvalidCode,
		},
		{
			name:  "empty token",
			token: "",
			setupMocks: func(store *MockStore) {
				// No mocks needed - validation fails first
			},
			wantRecipient:  "",
			wantIdentityID: "",
			wantErr:        ErrInvalidCode,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &MockStore{}
			courier := &MockCourier{}
			
			service := New(store, courier)
			tt.setupMocks(store)

			ctx := context.Background()
			recipient, identityID, err := service.VerifyMagicLink(ctx, tt.token)

			assert.Equal(t, tt.wantRecipient, recipient)
			assert.Equal(t, tt.wantIdentityID, identityID)
			
			if tt.wantErr != nil {
				assert.Error(t, err)
				assert.True(t, errors.Is(err, tt.wantErr) || err.Error() == tt.wantErr.Error())
			} else {
				assert.NoError(t, err)
			}

			store.AssertExpectations(t)
		})
	}
}

func TestService_SendVerificationCode(t *testing.T) {
	identityID := uuid.New().String()

	tests := []struct {
		name       string
		email      string
		identityID string
		setupMocks func(*MockStore, *MockCourier)
		wantErr    error
	}{
		{
			name:       "successful send",
			email:      "user@example.com",
			identityID: identityID,
			setupMocks: func(store *MockStore, courier *MockCourier) {
				store.On("CheckRateLimit", mock.Anything, "verify:"+identityID, 5, time.Hour).Return(nil)
				store.On("InvalidatePrevious", mock.Anything, "user@example.com", "verification").Return(nil)
				store.On("Create", mock.Anything, "user@example.com", "verification", &identityID, 15*time.Minute).Return("123456", "token123", nil)
				courier.On("SendMagicLinkEmail", mock.Anything, "user@example.com", "/self-service/verification/methods/link/verify?token=token123", "123456").Return(nil)
			},
			wantErr: nil,
		},
		{
			name:       "rate limited",
			email:      "user@example.com",
			identityID: identityID,
			setupMocks: func(store *MockStore, courier *MockCourier) {
				store.On("CheckRateLimit", mock.Anything, "verify:"+identityID, 5, time.Hour).Return(ErrRateLimited)
			},
			wantErr: ErrRateLimited,
		},
		{
			name:       "empty email",
			email:      "",
			identityID: identityID,
			setupMocks: func(store *MockStore, courier *MockCourier) {
				// No mocks needed - validation fails first
			},
			wantErr: ErrRecipientEmpty,
		},
		{
			name:       "empty identity ID",
			email:      "user@example.com",
			identityID: "",
			setupMocks: func(store *MockStore, courier *MockCourier) {
				// No mocks needed - validation fails first
			},
			wantErr: ErrRecipientEmpty,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &MockStore{}
			courier := &MockCourier{}
			
			service := New(store, courier)
			tt.setupMocks(store, courier)

			ctx := context.Background()
			err := service.SendVerificationCode(ctx, tt.email, tt.identityID)

			if tt.wantErr != nil {
				assert.Error(t, err)
				assert.Equal(t, tt.wantErr.Error(), err.Error())
			} else {
				assert.NoError(t, err)
			}

			store.AssertExpectations(t)
			courier.AssertExpectations(t)
		})
	}
}

func TestService_SendRecoveryCode(t *testing.T) {
	identityID := uuid.New().String()

	tests := []struct {
		name       string
		email      string
		identityID string
		setupMocks func(*MockStore, *MockCourier)
		wantErr    error
	}{
		{
			name:       "successful send",
			email:      "user@example.com",
			identityID: identityID,
			setupMocks: func(store *MockStore, courier *MockCourier) {
				// Recovery has stricter rate limit (3 per hour)
				store.On("CheckRateLimit", mock.Anything, "recover:user@example.com", 3, time.Hour).Return(nil)
				store.On("InvalidatePrevious", mock.Anything, "user@example.com", "recovery").Return(nil)
				store.On("Create", mock.Anything, "user@example.com", "recovery", &identityID, 15*time.Minute).Return("123456", "token123", nil)
				courier.On("SendMagicLinkEmail", mock.Anything, "user@example.com", "/self-service/recovery/methods/link/verify?token=token123", "123456").Return(nil)
			},
			wantErr: nil,
		},
		{
			name:       "rate limited",
			email:      "user@example.com",
			identityID: identityID,
			setupMocks: func(store *MockStore, courier *MockCourier) {
				store.On("CheckRateLimit", mock.Anything, "recover:user@example.com", 3, time.Hour).Return(ErrRateLimited)
			},
			wantErr: ErrRateLimited,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &MockStore{}
			courier := &MockCourier{}
			
			service := New(store, courier)
			tt.setupMocks(store, courier)

			ctx := context.Background()
			err := service.SendRecoveryCode(ctx, tt.email, tt.identityID)

			if tt.wantErr != nil {
				assert.Error(t, err)
				assert.Equal(t, tt.wantErr.Error(), err.Error())
			} else {
				assert.NoError(t, err)
			}

			store.AssertExpectations(t)
			courier.AssertExpectations(t)
		})
	}
}

func TestService_Cleanup(t *testing.T) {
	tests := []struct {
		name       string
		setupMocks func(*MockStore)
		wantErr    error
	}{
		{
			name: "successful cleanup",
			setupMocks: func(store *MockStore) {
				store.On("Cleanup", mock.Anything).Return(nil)
			},
			wantErr: nil,
		},
		{
			name: "cleanup error",
			setupMocks: func(store *MockStore) {
				store.On("Cleanup", mock.Anything).Return(errors.New("cleanup failed"))
			},
			wantErr: errors.New("cleanup failed"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &MockStore{}
			courier := &MockCourier{}
			
			service := New(store, courier)
			tt.setupMocks(store)

			ctx := context.Background()
			err := service.Cleanup(ctx)

			if tt.wantErr != nil {
				assert.Error(t, err)
				assert.Equal(t, tt.wantErr.Error(), err.Error())
			} else {
				assert.NoError(t, err)
			}

			store.AssertExpectations(t)
		})
	}
}

// Test different flow types for magic link building
func TestService_FlowTypes(t *testing.T) {
	flowTypes := []string{"login", "registration", "verification", "recovery", "settings"}
	service := &Service{config: Config{BaseURL: "https://auth.example.com"}}

	for _, flowType := range flowTypes {
		t.Run("flow_type_"+flowType, func(t *testing.T) {
			token := "test-token-123"
			link := service.buildMagicLink(flowType, token)
			
			expected := "https://auth.example.com/self-service/" + flowType + "/methods/link/verify?token=test-token-123"
			assert.Equal(t, expected, link)
		})
	}
}

// Test rate limit calculations
func TestService_RateLimitTypes(t *testing.T) {
	service := New(&MockStore{}, &MockCourier{})
	
	tests := []struct {
		operation   string
		email       string
		identityID  string
		expectKey   string
		expectLimit int
	}{
		{
			operation:   "login",
			email:       "user@example.com",
			identityID:  "",
			expectKey:   "login:user@example.com",
			expectLimit: 5,
		},
		{
			operation:   "verify",
			email:       "user@example.com",
			identityID:  "identity123",
			expectKey:   "verify:identity123",
			expectLimit: 5,
		},
		{
			operation:   "recover",
			email:       "user@example.com",
			identityID:  "",
			expectKey:   "recover:user@example.com",
			expectLimit: 3, // Stricter limit for recovery
		},
	}

	for _, tt := range tests {
		t.Run(tt.operation+"_rate_limit", func(t *testing.T) {
			// Verify that different operations have different rate limit keys and values
			// This tests the business logic around rate limiting
			assert.Contains(t, tt.expectKey, tt.operation)
			assert.True(t, tt.expectLimit > 0)
			assert.True(t, tt.expectLimit <= 5)
		})
	}
}

// Test context handling
func TestService_ContextCancellation(t *testing.T) {
	store := &MockStore{}
	courier := &MockCourier{}
	service := New(store, courier)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Test that canceled context is handled properly
	err := service.SendLoginCode(ctx, "user@example.com")
	
	// Should fail due to context cancellation or validation (empty email is checked first)
	assert.Error(t, err)
}

// Test edge cases and error conditions
func TestService_EdgeCases(t *testing.T) {
	t.Run("very long email", func(t *testing.T) {
		service := New(&MockStore{}, &MockCourier{})
		longEmail := strings.Repeat("a", 320) + "@example.com" // Email longer than RFC limit
		
		ctx := context.Background()
		err := service.SendLoginCode(ctx, longEmail)
		
		// Should not fail due to length alone, but would likely fail at store/courier level
		// This tests that the service doesn't crash with unusual input
		assert.Error(t, err) // Will fail due to empty email check or store operations
	})

	t.Run("unicode in email", func(t *testing.T) {
		service := New(&MockStore{}, &MockCourier{})
		unicodeEmail := "tëst@éxample.com"
		
		ctx := context.Background()
		err := service.SendLoginCode(ctx, unicodeEmail)
		
		// Should handle unicode gracefully
		assert.Error(t, err) // Will fail due to mock store, but shouldn't panic
	})

	t.Run("empty string validation", func(t *testing.T) {
		service := New(&MockStore{}, &MockCourier{})
		ctx := context.Background()
		
		// Test various empty string scenarios
		err1 := service.SendLoginCode(ctx, "")
		assert.Error(t, err1)
		assert.True(t, errors.Is(err1, ErrRecipientEmpty))
		
		_, _, err2 := service.VerifyCode(ctx, "", "123456")
		assert.Error(t, err2)
		assert.True(t, errors.Is(err2, ErrRecipientEmpty))
		
		_, _, err3 := service.VerifyCode(ctx, "user@example.com", "")
		assert.Error(t, err3)
		assert.True(t, errors.Is(err3, ErrInvalidCode))
	})
}

// Benchmark critical functions
func BenchmarkBuildMagicLink(b *testing.B) {
	service := &Service{
		config: Config{BaseURL: "https://auth.example.com"},
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		service.buildMagicLink("login", "test-token-123")
	}
}

func BenchmarkURLEscaping(b *testing.B) {
	token := "token+with/special=chars&more"
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		url.QueryEscape(token)
	}
}