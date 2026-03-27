package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestConfigDefaults(t *testing.T) {
	config := Config{}
	
	// Test that zero values are as expected
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

func TestErrors(t *testing.T) {
	// Test that all errors are properly defined
	assert.NotNil(t, ErrInvalidCode)
	assert.NotNil(t, ErrRateLimited)
	assert.NotNil(t, ErrRecipientEmpty)
	
	// Test error messages
	assert.Contains(t, ErrInvalidCode.Error(), "invalid")
	assert.Contains(t, ErrRateLimited.Error(), "too many")
	assert.Contains(t, ErrRecipientEmpty.Error(), "required")
}

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
			name:     "registration flow",
			baseURL:  "https://example.com",
			token:    "xyz789",
			flowType: "registration",
			want:     "https://example.com/self-service/registration/methods/link/verify?token=xyz789",
		},
		{
			name:     "recovery flow",
			baseURL:  "https://example.com",
			token:    "recover123",
			flowType: "recovery",
			want:     "https://example.com/self-service/recovery/methods/link/verify?token=recover123",
		},
		{
			name:     "empty base URL uses localhost",
			baseURL:  "",
			token:    "test",
			flowType: "login",
			want:     "http://localhost:8080/self-service/login/methods/link/verify?token=test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &Service{
				config: Config{
					BaseURL: tt.baseURL,
				},
			}
			
			result := svc.buildMagicLink(tt.token, tt.flowType)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestCourierInterface(t *testing.T) {
	// Test that Courier interface is properly defined
	var _ Courier = (*mockCourier)(nil)
}

type mockCourier struct{}

func (m *mockCourier) SendMagicLinkEmail(ctx context.Context, to string, link string, code string) error {
	return nil
}
