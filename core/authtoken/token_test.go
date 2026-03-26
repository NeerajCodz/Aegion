package authtoken

import (
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewGenerator(t *testing.T) {
	tests := []struct {
		name    string
		config  GeneratorConfig
		wantErr error
	}{
		{
			name: "valid config with defaults",
			config: GeneratorConfig{
				Secret: []byte("test-secret-32-bytes-long!!!"),
			},
			wantErr: nil,
		},
		{
			name: "valid config with custom TTL",
			config: GeneratorConfig{
				Secret: []byte("test-secret"),
				TTL:    10 * time.Minute,
			},
			wantErr: nil,
		},
		{
			name: "valid config with previous secrets",
			config: GeneratorConfig{
				Secret: []byte("new-secret"),
				PreviousSecrets: [][]byte{
					[]byte("old-secret-1"),
					[]byte("old-secret-2"),
				},
			},
			wantErr: nil,
		},
		{
			name: "empty secret",
			config: GeneratorConfig{
				Secret: nil,
			},
			wantErr: ErrInvalidSecret,
		},
		{
			name: "empty secret slice",
			config: GeneratorConfig{
				Secret: []byte{},
			},
			wantErr: ErrInvalidSecret,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gen, err := NewGenerator(tt.config)

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, gen)
				return
			}

			require.NoError(t, err)
			assert.NotNil(t, gen)

			// Verify TTL is set correctly
			expectedTTL := tt.config.TTL
			if expectedTTL == 0 {
				expectedTTL = DefaultTTL
			}
			assert.Equal(t, expectedTTL, gen.GetTTL())

			// Verify secrets are properly ordered
			expectedSecretCount := 1 + len(tt.config.PreviousSecrets)
			assert.Len(t, gen.secrets, expectedSecretCount)
			assert.Equal(t, tt.config.Secret, gen.secrets[0])
			for i, prevSecret := range tt.config.PreviousSecrets {
				assert.Equal(t, prevSecret, gen.secrets[i+1])
			}
		})
	}
}

func TestGenerator_Generate(t *testing.T) {
	secret := make([]byte, 32)
	_, err := rand.Read(secret)
	require.NoError(t, err)

	gen, err := NewGenerator(GeneratorConfig{
		Secret: secret,
		TTL:    5 * time.Minute,
	})
	require.NoError(t, err)

	tests := []struct {
		name     string
		moduleID string
		wantErr  error
	}{
		{
			name:     "valid module ID",
			moduleID: "password",
			wantErr:  nil,
		},
		{
			name:     "another valid module ID",
			moduleID: "magic_link",
			wantErr:  nil,
		},
		{
			name:     "module ID with special characters",
			moduleID: "test-module_123",
			wantErr:  nil,
		},
		{
			name:     "empty module ID",
			moduleID: "",
			wantErr:  ErrModuleIDEmpty,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := gen.Generate(tt.moduleID)

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Empty(t, token)
				return
			}

			require.NoError(t, err)
			assert.NotEmpty(t, token)

			// Verify token format: 3 parts separated by dots
			parts := strings.Split(token, TokenSeparator)
			assert.Len(t, parts, 3)

			// Verify each part is valid base64
			for i, part := range parts {
				_, err := base64.RawURLEncoding.DecodeString(part)
				assert.NoError(t, err, "part %d should be valid base64", i)
			}

			// Verify token can be validated
			validatedToken, err := gen.Validate(token)
			require.NoError(t, err)
			assert.Equal(t, tt.moduleID, validatedToken.ModuleID)
			assert.WithinDuration(t, time.Now().UTC(), validatedToken.Timestamp, time.Second)
		})
	}
}

func TestGenerator_Validate(t *testing.T) {
	secret := []byte("test-secret-32-bytes-for-validation")
	gen, err := NewGenerator(GeneratorConfig{
		Secret: secret,
		TTL:    5 * time.Minute,
	})
	require.NoError(t, err)

	// Generate a valid token for testing
	validToken, err := gen.Generate("test-module")
	require.NoError(t, err)

	tests := []struct {
		name      string
		token     string
		wantErr   error
		wantValid bool
	}{
		{
			name:      "valid token",
			token:     validToken,
			wantErr:   nil,
			wantValid: true,
		},
		{
			name:      "invalid format - too few parts",
			token:     "invalid.token",
			wantErr:   ErrInvalidToken,
			wantValid: false,
		},
		{
			name:      "invalid format - too many parts",
			token:     "invalid.token.with.too.many.parts",
			wantErr:   ErrInvalidToken,
			wantValid: false,
		},
		{
			name:      "invalid base64 in module ID",
			token:     "invalid_base64!.dGVzdA.dGVzdA",
			wantErr:   ErrInvalidToken,
			wantValid: false,
		},
		{
			name:      "invalid base64 in timestamp",
			token:     "dGVzdA.invalid_base64!.dGVzdA",
			wantErr:   ErrInvalidToken,
			wantValid: false,
		},
		{
			name:      "invalid base64 in signature",
			token:     "dGVzdA.dGVzdA.invalid_base64!",
			wantErr:   ErrInvalidToken,
			wantValid: false,
		},
		{
			name:      "invalid timestamp format",
			token:     base64.RawURLEncoding.EncodeToString([]byte("test")) + "." + base64.RawURLEncoding.EncodeToString([]byte("invalid-timestamp")) + "." + base64.RawURLEncoding.EncodeToString([]byte("signature")),
			wantErr:   ErrInvalidToken,
			wantValid: false,
		},
		{
			name:      "empty token",
			token:     "",
			wantErr:   ErrInvalidToken,
			wantValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := gen.Validate(tt.token)

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, token)
				return
			}

			if tt.wantValid {
				require.NoError(t, err)
				assert.NotNil(t, token)
				assert.Equal(t, "test-module", token.ModuleID)
				assert.NotEmpty(t, token.Signature)
				assert.WithinDuration(t, time.Now().UTC(), token.Timestamp, time.Minute)
			} else {
				assert.Error(t, err)
				assert.Nil(t, token)
			}
		})
	}
}

func TestGenerator_ValidateExpiredToken(t *testing.T) {
	secret := []byte("test-secret-for-expiration-test")
	gen, err := NewGenerator(GeneratorConfig{
		Secret: secret,
		TTL:    100 * time.Millisecond, // Very short TTL for testing
	})
	require.NoError(t, err)

	// Generate token
	token, err := gen.Generate("test-module")
	require.NoError(t, err)

	// Wait for token to expire
	time.Sleep(200 * time.Millisecond)

	// Validate expired token
	result, err := gen.Validate(token)
	assert.ErrorIs(t, err, ErrExpiredToken)
	assert.Nil(t, result)
}

func TestGenerator_ValidateString(t *testing.T) {
	secret := []byte("test-secret-for-validate-string")
	gen, err := NewGenerator(GeneratorConfig{
		Secret: secret,
		TTL:    5 * time.Minute,
	})
	require.NoError(t, err)

	tests := []struct {
		name       string
		setupToken func() string
		wantModule string
		wantErr    error
	}{
		{
			name: "valid token returns module ID",
			setupToken: func() string {
				token, _ := gen.Generate("password")
				return token
			},
			wantModule: "password",
			wantErr:    nil,
		},
		{
			name: "invalid token returns error",
			setupToken: func() string {
				return "invalid.token.format"
			},
			wantModule: "",
			wantErr:    ErrInvalidToken,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := tt.setupToken()
			moduleID, err := gen.ValidateString(token)

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Equal(t, tt.wantModule, moduleID)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantModule, moduleID)
			}
		})
	}
}

func TestGenerator_SetSecrets(t *testing.T) {
	secret1 := []byte("secret-1")
	secret2 := []byte("secret-2")
	secret3 := []byte("secret-3")

	gen, err := NewGenerator(GeneratorConfig{
		Secret: secret1,
	})
	require.NoError(t, err)

	tests := []struct {
		name     string
		primary  []byte
		previous [][]byte
		wantErr  error
	}{
		{
			name:     "valid single secret",
			primary:  secret2,
			previous: nil,
			wantErr:  nil,
		},
		{
			name:     "valid with previous secrets",
			primary:  secret2,
			previous: [][]byte{secret1, secret3},
			wantErr:  nil,
		},
		{
			name:     "empty primary secret",
			primary:  []byte{},
			previous: [][]byte{secret1},
			wantErr:  ErrInvalidSecret,
		},
		{
			name:     "nil primary secret",
			primary:  nil,
			previous: [][]byte{secret1},
			wantErr:  ErrInvalidSecret,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := gen.SetSecrets(tt.primary, tt.previous...)

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)

			// Verify secrets were set correctly
			expectedCount := 1 + len(tt.previous)
			assert.Len(t, gen.secrets, expectedCount)
			assert.Equal(t, tt.primary, gen.secrets[0])
			for i, prev := range tt.previous {
				assert.Equal(t, prev, gen.secrets[i+1])
			}
		})
	}
}

func TestGenerator_SecretRotation(t *testing.T) {
	oldSecret := []byte("old-secret-32-bytes-for-testing")
	newSecret := []byte("new-secret-32-bytes-for-testing")

	gen, err := NewGenerator(GeneratorConfig{
		Secret: oldSecret,
		TTL:    5 * time.Minute,
	})
	require.NoError(t, err)

	// Generate token with old secret
	oldToken, err := gen.Generate("test-module")
	require.NoError(t, err)

	// Rotate to new secret while keeping old one
	err = gen.SetSecrets(newSecret, oldSecret)
	require.NoError(t, err)

	// Generate token with new secret
	newToken, err := gen.Generate("test-module")
	require.NoError(t, err)

	// Both tokens should be valid
	oldResult, err := gen.Validate(oldToken)
	require.NoError(t, err)
	assert.Equal(t, "test-module", oldResult.ModuleID)

	newResult, err := gen.Validate(newToken)
	require.NoError(t, err)
	assert.Equal(t, "test-module", newResult.ModuleID)

	// Remove old secret
	err = gen.SetSecrets(newSecret)
	require.NoError(t, err)

	// New token should still be valid
	_, err = gen.Validate(newToken)
	assert.NoError(t, err)

	// Old token should now be invalid
	_, err = gen.Validate(oldToken)
	assert.ErrorIs(t, err, ErrInvalidToken)
}

func TestGenerator_ConcurrentAccess(t *testing.T) {
	secret := []byte("concurrent-test-secret-32-bytes")
	gen, err := NewGenerator(GeneratorConfig{
		Secret: secret,
		TTL:    5 * time.Minute,
	})
	require.NoError(t, err)

	// Test concurrent generation and validation
	done := make(chan bool, 100)
	errors := make(chan error, 100)

	// Start multiple goroutines
	for i := 0; i < 50; i++ {
		go func(id int) {
			defer func() { done <- true }()

			// Generate token
			token, err := gen.Generate("concurrent-test")
			if err != nil {
				errors <- err
				return
			}

			// Validate token
			_, err = gen.Validate(token)
			if err != nil {
				errors <- err
				return
			}

			// Rotate secrets
			newSecret := make([]byte, 32)
			_, err = rand.Read(newSecret)
			if err != nil {
				errors <- err
				return
			}

			err = gen.SetSecrets(newSecret, secret)
			if err != nil {
				errors <- err
			}
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 50; i++ {
		<-done
	}

	// Check for errors
	close(errors)
	for err := range errors {
		assert.NoError(t, err, "concurrent access should not cause errors")
	}
}

func TestBuildPayload(t *testing.T) {
	timestamp, err := time.Parse(time.RFC3339Nano, "2023-01-01T12:00:00.123456789Z")
	require.NoError(t, err)

	tests := []struct {
		name      string
		moduleID  string
		timestamp time.Time
		expected  string
	}{
		{
			name:      "simple module ID",
			moduleID:  "password",
			timestamp: timestamp,
			expected:  "password:2023-01-01T12:00:00.123456789Z",
		},
		{
			name:      "complex module ID",
			moduleID:  "magic_link-test",
			timestamp: timestamp,
			expected:  "magic_link-test:2023-01-01T12:00:00.123456789Z",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildPayload(tt.moduleID, tt.timestamp)
			assert.Equal(t, []byte(tt.expected), result)
		})
	}
}

func TestSign(t *testing.T) {
	secret := []byte("test-signing-secret")
	payload1 := []byte("test payload 1")
	payload2 := []byte("test payload 2")

	// Same payload and secret should produce same signature
	sig1a := sign(payload1, secret)
	sig1b := sign(payload1, secret)
	assert.Equal(t, sig1a, sig1b)

	// Different payloads should produce different signatures
	sig2 := sign(payload2, secret)
	assert.NotEqual(t, sig1a, sig2)

	// Signature should be 32 bytes (SHA256)
	assert.Len(t, sig1a, SignatureLength)
	assert.Len(t, sig2, SignatureLength)
}

func TestTokenUniqueness(t *testing.T) {
	secret := []byte("uniqueness-test-secret")
	gen, err := NewGenerator(GeneratorConfig{
		Secret: secret,
		TTL:    5 * time.Minute,
	})
	require.NoError(t, err)

	// Generate multiple tokens for the same module
	tokens := make(map[string]bool)
	for i := 0; i < 100; i++ {
		token, err := gen.Generate("test")
		require.NoError(t, err)
		
		// Each token should be unique (due to timestamp precision)
		assert.False(t, tokens[token], "token should be unique: %s", token)
		tokens[token] = true
	}
}