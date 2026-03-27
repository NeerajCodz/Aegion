package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfigDefaults(t *testing.T) {
	config := Config{}
	
	// Test that zero values are as expected
	assert.Equal(t, 0, config.MinLength)
	assert.False(t, config.RequireUppercase)
	assert.False(t, config.RequireLowercase)
	assert.False(t, config.RequireNumber)
	assert.False(t, config.RequireSpecial)
	assert.False(t, config.HIBPEnabled)
	assert.Equal(t, 0, config.HistoryCount)
}

func TestConfigWithValues(t *testing.T) {
	config := Config{
		MinLength:        12,
		RequireUppercase: true,
		RequireLowercase: true,
		RequireNumber:    true,
		RequireSpecial:   true,
		HIBPEnabled:      true,
		HistoryCount:     5,
	}
	
	assert.Equal(t, 12, config.MinLength)
	assert.True(t, config.RequireUppercase)
	assert.True(t, config.RequireLowercase)
	assert.True(t, config.RequireNumber)
	assert.True(t, config.RequireSpecial)
	assert.True(t, config.HIBPEnabled)
	assert.Equal(t, 5, config.HistoryCount)
}

// MockHasher implements Hasher interface for testing
type mockHasher struct {
	hashFunc   func(password string) (string, error)
	verifyFunc func(password, hash string) (bool, error)
}

func (m *mockHasher) Hash(password string) (string, error) {
	if m.hashFunc != nil {
		return m.hashFunc(password)
	}
	return "hashed_" + password, nil
}

func (m *mockHasher) Verify(password, hash string) (bool, error) {
	if m.verifyFunc != nil {
		return m.verifyFunc(password, hash)
	}
	return hash == "hashed_"+password, nil
}

func TestCheckComplexity(t *testing.T) {
	tests := []struct {
		name     string
		password string
		config   Config
		wantErr  bool
	}{
		{
			name:     "all requirements met",
			password: "SecurePass123!",
			config: Config{
				RequireUppercase: true,
				RequireLowercase: true,
				RequireNumber:    true,
				RequireSpecial:   true,
			},
			wantErr: false,
		},
		{
			name:     "missing uppercase",
			password: "lowercase123!",
			config: Config{
				RequireUppercase: true,
			},
			wantErr: true,
		},
		{
			name:     "missing lowercase",
			password: "UPPERCASE123!",
			config: Config{
				RequireLowercase: true,
			},
			wantErr: true,
		},
		{
			name:     "missing number",
			password: "SecurePass!",
			config: Config{
				RequireNumber: true,
			},
			wantErr: true,
		},
		{
			name:     "missing special character",
			password: "SecurePass123",
			config: Config{
				RequireSpecial: true,
			},
			wantErr: true,
		},
		{
			name:     "no requirements - anything passes",
			password: "simple",
			config:   Config{},
			wantErr:  false,
		},
		{
			name:     "unicode characters",
			password: "Pässwörd123!",
			config: Config{
				RequireUppercase: true,
				RequireLowercase: true,
				RequireNumber:    true,
				RequireSpecial:   true,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &Service{config: tt.config}
			err := svc.checkComplexity(tt.password)
			
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCheckSimilarity(t *testing.T) {
	tests := []struct {
		name       string
		password   string
		identifier string
		wantErr    bool
	}{
		{
			name:       "different password and identifier",
			password:   "SecurePass123!",
			identifier: "user@example.com",
			wantErr:    false,
		},
		{
			name:       "password contains username",
			password:   "testuser123!",
			identifier: "testuser@example.com",
			wantErr:    true,
		},
		{
			name:       "password is too similar",
			password:   "john123!",
			identifier: "john@example.com",
			wantErr:    true,
		},
		{
			name:       "empty identifier",
			password:   "SecurePass123!",
			identifier: "",
			wantErr:    false,
		},
		{
			name:       "password same as email",
			password:   "test@example.com",
			identifier: "test@example.com",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &Service{}
			err := svc.checkSimilarity(tt.password, tt.identifier)
			
			if tt.wantErr {
				assert.Error(t, err)
				assert.True(t, errors.Is(err, ErrPasswordSimilar))
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name       string
		password   string
		identifier string
		config     Config
		wantErr    error
	}{
		{
			name:       "valid password",
			password:   "SecurePass123!",
			identifier: "user@example.com",
			config: Config{
				MinLength:        8,
				RequireUppercase: true,
				RequireLowercase: true,
				RequireNumber:    true,
				RequireSpecial:   true,
			},
			wantErr: nil,
		},
		{
			name:       "too short",
			password:   "Short1!",
			identifier: "user@example.com",
			config:     Config{MinLength: 8},
			wantErr:    ErrPasswordTooShort,
		},
		{
			name:       "too weak - missing uppercase",
			password:   "lowercase123!",
			identifier: "user@example.com",
			config: Config{
				MinLength:        8,
				RequireUppercase: true,
			},
			wantErr: ErrPasswordTooWeak,
		},
		{
			name:       "similar to identifier",
			password:   "testuser123!",
			identifier: "testuser@example.com",
			config: Config{
				MinLength: 8,
			},
			wantErr: ErrPasswordSimilar,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &Service{
				hasher: &mockHasher{},
				config: tt.config,
			}

			ctx := context.Background()
			err := svc.ValidatePassword(ctx, tt.password, tt.identifier)

			if tt.wantErr != nil {
				assert.Error(t, err)
				assert.True(t, errors.Is(err, tt.wantErr))
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestErrors(t *testing.T) {
	// Test that all errors are properly defined
	assert.NotNil(t, ErrPasswordTooShort)
	assert.NotNil(t, ErrPasswordTooWeak)
	assert.NotNil(t, ErrPasswordBreached)
	assert.NotNil(t, ErrPasswordReused)
	assert.NotNil(t, ErrPasswordSimilar)
	assert.NotNil(t, ErrInvalidCredentials)
	assert.NotNil(t, ErrIdentityNotFound)
	
	// Test error messages
	assert.Contains(t, ErrPasswordTooShort.Error(), "short")
	assert.Contains(t, ErrPasswordTooWeak.Error(), "complexity")
	assert.Contains(t, ErrPasswordBreached.Error(), "breach")
}
