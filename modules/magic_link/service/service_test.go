package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/aegion/aegion/modules/magic_link/store"
)

// ============================================================================
// Mock Courier Implementation
// ============================================================================

// MockCourier mocks the Courier interface
type MockCourier struct {
	mock.Mock
}

func (m *MockCourier) SendMagicLinkEmail(ctx context.Context, to string, link string, code string) error {
	args := m.Called(ctx, to, link, code)
	return args.Error(0)
}

// ============================================================================
// Configuration Tests
// ============================================================================

func TestConfigDefaults(t *testing.T) {
	config := Config{}

	assert.Equal(t, "", config.BaseURL)
	assert.Equal(t, 0, config.CodeLength)
	assert.Equal(t, "", config.CodeCharset)
	assert.Equal(t, time.Duration(0), config.LinkLifespan)
	assert.Equal(t, time.Duration(0), config.CodeLifespan)
	assert.Equal(t, 0, config.RateLimit)
	assert.Equal(t, time.Duration(0), config.RateWindow)
}

func TestConfigWithValues(t *testing.T) {
	config := Config{
		BaseURL:      "https://example.com",
		CodeLength:   8,
		CodeCharset:  "ABCDEFGHIJKLMNOPQRSTUVWXYZ",
		LinkLifespan: 30 * time.Minute,
		CodeLifespan: 10 * time.Minute,
		RateLimit:    10,
		RateWindow:   30 * time.Minute,
	}

	assert.Equal(t, "https://example.com", config.BaseURL)
	assert.Equal(t, 8, config.CodeLength)
	assert.Equal(t, "ABCDEFGHIJKLMNOPQRSTUVWXYZ", config.CodeCharset)
	assert.Equal(t, 30*time.Minute, config.LinkLifespan)
	assert.Equal(t, 10*time.Minute, config.CodeLifespan)
	assert.Equal(t, 10, config.RateLimit)
	assert.Equal(t, 30*time.Minute, config.RateWindow)
}

func TestErrorDefinitions(t *testing.T) {
	assert.NotNil(t, ErrInvalidCode)
	assert.NotNil(t, ErrRateLimited)
	assert.NotNil(t, ErrRecipientEmpty)

	assert.Contains(t, ErrInvalidCode.Error(), "invalid")
	assert.Contains(t, ErrRateLimited.Error(), "too many")
	assert.Contains(t, ErrRecipientEmpty.Error(), "required")
}

// ============================================================================
// BuildMagicLink Tests
// ============================================================================

func TestBuildMagicLink(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		token    string
		flowType string
		want     string
	}{
		{
			name:     "login flow",
			baseURL:  "https://example.com",
			token:    "abc123",
			flowType: "login",
			want:     "https://example.com/self-service/login/methods/link/verify?token=abc123",
		},
		{
			name:     "verification flow",
			baseURL:  "https://example.com",
			token:    "verify456",
			flowType: "verification",
			want:     "https://example.com/self-service/verification/methods/link/verify?token=verify456",
		},
		{
			name:     "recovery flow",
			baseURL:  "https://example.com",
			token:    "recover789",
			flowType: "recovery",
			want:     "https://example.com/self-service/recovery/methods/link/verify?token=recover789",
		},
		{
			name:     "empty base URL defaults to localhost",
			baseURL:  "",
			token:    "test123",
			flowType: "login",
			want:     "http://localhost:8080/self-service/login/methods/link/verify?token=test123",
		},
		{
			name:     "URL with trailing slash",
			baseURL:  "https://example.com/",
			token:    "token123",
			flowType: "login",
			want:     "https://example.com/self-service/login/methods/link/verify?token=token123",
		},
		{
			name:     "URL with port",
			baseURL:  "https://example.com:8443",
			token:    "secure123",
			flowType: "login",
			want:     "https://example.com:8443/self-service/login/methods/link/verify?token=secure123",
		},
		{
			name:     "token with special characters",
			baseURL:  "https://example.com",
			token:    "abc-123_xyz",
			flowType: "login",
			want:     "https://example.com/self-service/login/methods/link/verify?token=abc-123_xyz",
		},
		{
			name:     "http scheme",
			baseURL:  "http://localhost:3000",
			token:    "token123",
			flowType: "login",
			want:     "http://localhost:3000/self-service/login/methods/link/verify?token=token123",
		},
		{
			name:     "token with encoded characters",
			baseURL:  "https://example.com",
			token:    "token=test&value=1",
			flowType: "login",
			want:     "https://example.com/self-service/login/methods/link/verify?token=token%3Dtest%26value%3D1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &Service{
				config: Config{BaseURL: tt.baseURL},
			}
			result := svc.buildMagicLink(tt.token, tt.flowType)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestBuildMagicLinkInvalidURL(t *testing.T) {
	svc := &Service{
		config: Config{BaseURL: "ht!tp://invalid"},
	}
	result := svc.buildMagicLink("token", "login")
	assert.Equal(t, "", result)
}

// ============================================================================
// New Service Tests
// ============================================================================

func TestNewServiceWithDefaults(t *testing.T) {
	mockStore := newTestStore()
	mockCourier := new(MockCourier)

	config := Config{}
	svc := New(mockStore, mockCourier, config)

	assert.NotNil(t, svc)
	assert.Equal(t, 6, svc.config.CodeLength)
	assert.Equal(t, "0123456789", svc.config.CodeCharset)
	assert.Equal(t, 15*time.Minute, svc.config.LinkLifespan)
	assert.Equal(t, 15*time.Minute, svc.config.CodeLifespan)
	assert.Equal(t, 5, svc.config.RateLimit)
	assert.Equal(t, time.Hour, svc.config.RateWindow)
}

func TestNewServiceWithCustomConfig(t *testing.T) {
	mockStore := newTestStore()
	mockCourier := new(MockCourier)

	config := Config{
		BaseURL:      "https://example.com",
		CodeLength:   8,
		CodeCharset:  "ABCDEFGHIJKLMNOPQRSTUVWXYZ",
		LinkLifespan: 30 * time.Minute,
		CodeLifespan: 10 * time.Minute,
		RateLimit:    10,
		RateWindow:   2 * time.Hour,
	}
	svc := New(mockStore, mockCourier, config)

	assert.NotNil(t, svc)
	assert.Equal(t, "https://example.com", svc.config.BaseURL)
	assert.Equal(t, 8, svc.config.CodeLength)
	assert.Equal(t, "ABCDEFGHIJKLMNOPQRSTUVWXYZ", svc.config.CodeCharset)
	assert.Equal(t, 30*time.Minute, svc.config.LinkLifespan)
	assert.Equal(t, 10*time.Minute, svc.config.CodeLifespan)
	assert.Equal(t, 10, svc.config.RateLimit)
	assert.Equal(t, 2*time.Hour, svc.config.RateWindow)
}

func TestNewServiceWithNilCourier(t *testing.T) {
	mockStore := newTestStore()

	config := Config{}
	svc := New(mockStore, nil, config)

	assert.NotNil(t, svc)
	assert.Nil(t, svc.courier)
}

// ============================================================================
// Error Handling Tests
// ============================================================================

func TestSendLoginCodeEmptyEmail(t *testing.T) {
	mockStore := newTestStore()
	mockCourier := new(MockCourier)

	svc := New(mockStore, mockCourier, Config{})
	err := svc.SendLoginCode(context.Background(), "")

	assert.Error(t, err)
	assert.Equal(t, ErrRecipientEmpty, err)
}

func TestSendVerificationCodeEmptyEmail(t *testing.T) {
	mockStore := newTestStore()
	mockCourier := new(MockCourier)
	identityID := uuid.New()

	svc := New(mockStore, mockCourier, Config{})
	err := svc.SendVerificationCode(context.Background(), "", identityID)

	assert.Error(t, err)
	assert.Equal(t, ErrRecipientEmpty, err)
}

func TestSendRecoveryCodeEmptyEmail(t *testing.T) {
	mockStore := newTestStore()
	mockCourier := new(MockCourier)
	identityID := uuid.New()

	svc := New(mockStore, mockCourier, Config{})
	err := svc.SendRecoveryCode(context.Background(), "", identityID)

	assert.Error(t, err)
	assert.Equal(t, ErrRecipientEmpty, err)
}

// ============================================================================
// Courier Interface Tests
// ============================================================================

func TestCourierInterface(t *testing.T) {
	// Verify that MockCourier implements the Courier interface
	var _ Courier = (*MockCourier)(nil)
}

func TestCourierImplementation(t *testing.T) {
	mockCourier := new(MockCourier)
	ctx := context.Background()

	mockCourier.On("SendMagicLinkEmail", ctx, "test@example.com", "https://example.com/verify?token=abc123", "123456").Return(nil)

	err := mockCourier.SendMagicLinkEmail(ctx, "test@example.com", "https://example.com/verify?token=abc123", "123456")
	assert.NoError(t, err)
	mockCourier.AssertExpectations(t)
}

// ============================================================================
// Config Defaults Tests
// ============================================================================

func TestConfigDefaultsCodeLength(t *testing.T) {
	config := Config{}
	mockStore := newTestStore()
	mockCourier := new(MockCourier)

	svc := New(mockStore, mockCourier, config)

	assert.Equal(t, 6, svc.config.CodeLength)
}

func TestConfigDefaultsCodeCharset(t *testing.T) {
	config := Config{}
	mockStore := newTestStore()
	mockCourier := new(MockCourier)

	svc := New(mockStore, mockCourier, config)

	assert.Equal(t, "0123456789", svc.config.CodeCharset)
}

func TestConfigDefaultsLinkLifespan(t *testing.T) {
	config := Config{}
	mockStore := newTestStore()
	mockCourier := new(MockCourier)

	svc := New(mockStore, mockCourier, config)

	assert.Equal(t, 15*time.Minute, svc.config.LinkLifespan)
}

func TestConfigDefaultsCodeLifespan(t *testing.T) {
	config := Config{}
	mockStore := newTestStore()
	mockCourier := new(MockCourier)

	svc := New(mockStore, mockCourier, config)

	assert.Equal(t, 15*time.Minute, svc.config.CodeLifespan)
}

func TestConfigDefaultsRateLimit(t *testing.T) {
	config := Config{}
	mockStore := newTestStore()
	mockCourier := new(MockCourier)

	svc := New(mockStore, mockCourier, config)

	assert.Equal(t, 5, svc.config.RateLimit)
}

func TestConfigDefaultsRateWindow(t *testing.T) {
	config := Config{}
	mockStore := newTestStore()
	mockCourier := new(MockCourier)

	svc := New(mockStore, mockCourier, config)

	assert.Equal(t, time.Hour, svc.config.RateWindow)
}

// ============================================================================
// Config Override Tests
// ============================================================================

func TestConfigOverrideCodeLength(t *testing.T) {
	config := Config{CodeLength: 8}
	mockStore := newTestStore()
	mockCourier := new(MockCourier)

	New(mockStore, mockCourier, config)

	assert.Equal(t, 8, config.CodeLength)
}

func TestConfigOverrideCodeCharset(t *testing.T) {
	config := Config{CodeCharset: "ABCDEF"}
	mockStore := newTestStore()
	mockCourier := new(MockCourier)

	New(mockStore, mockCourier, config)

	assert.Equal(t, "ABCDEF", config.CodeCharset)
}

func TestConfigOverrideLinkLifespan(t *testing.T) {
	config := Config{LinkLifespan: 30 * time.Minute}
	mockStore := newTestStore()
	mockCourier := new(MockCourier)

	New(mockStore, mockCourier, config)

	assert.Equal(t, 30*time.Minute, config.LinkLifespan)
}

func TestConfigOverrideCodeLifespan(t *testing.T) {
	config := Config{CodeLifespan: 10 * time.Minute}
	mockStore := newTestStore()
	mockCourier := new(MockCourier)

	New(mockStore, mockCourier, config)

	assert.Equal(t, 10*time.Minute, config.CodeLifespan)
}

func TestConfigOverrideRateLimit(t *testing.T) {
	config := Config{RateLimit: 10}
	mockStore := newTestStore()
	mockCourier := new(MockCourier)

	New(mockStore, mockCourier, config)

	assert.Equal(t, 10, config.RateLimit)
}

func TestConfigOverrideRateWindow(t *testing.T) {
	config := Config{RateWindow: 2 * time.Hour}
	mockStore := newTestStore()
	mockCourier := new(MockCourier)

	New(mockStore, mockCourier, config)

	assert.Equal(t, 2*time.Hour, config.RateWindow)
}

// ============================================================================
// BuildMagicLink Edge Cases
// ============================================================================

func TestBuildMagicLinkEmptyToken(t *testing.T) {
	svc := &Service{
		config: Config{BaseURL: "https://example.com"},
	}
	result := svc.buildMagicLink("", "login")
	assert.Contains(t, result, "token=")
}

func TestBuildMagicLinkEmptyFlowType(t *testing.T) {
	svc := &Service{
		config: Config{BaseURL: "https://example.com"},
	}
	result := svc.buildMagicLink("token123", "")
	assert.Contains(t, result, "token=token123")
}

func TestBuildMagicLinkURLPath(t *testing.T) {
	svc := &Service{
		config: Config{BaseURL: "https://example.com"},
	}
	result := svc.buildMagicLink("token", "login")
	assert.Contains(t, result, "/self-service/login/methods/link/verify")
}

func TestBuildMagicLinkQueryParameter(t *testing.T) {
	svc := &Service{
		config: Config{BaseURL: "https://example.com"},
	}
	result := svc.buildMagicLink("test-token", "login")
	assert.Contains(t, result, "?token=test-token")
}

// ============================================================================
// Helper Functions
// ============================================================================

// newTestStore creates a test store with nil database
func newTestStore() *store.Store {
	return &store.Store{}
}
