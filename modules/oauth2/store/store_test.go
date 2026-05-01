// Package store tests for OAuth2 store layer.
package store

import (
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
)

// Test ID generators
func TestIDGenerators(t *testing.T) {
	t.Run("GenerateAccessTokenJTI", func(t *testing.T) {
		jti1 := GenerateAccessTokenJTI()
		jti2 := GenerateAccessTokenJTI()
		assert.NotEqual(t, jti1, jti2)
		assert.Contains(t, jti1, "at_")
		assert.Greater(t, len(jti1), 40) // at_ + base64 chars
	})

	t.Run("GenerateRefreshToken", func(t *testing.T) {
		rt1 := GenerateRefreshToken()
		rt2 := GenerateRefreshToken()
		assert.NotEqual(t, rt1, rt2)
		assert.Contains(t, rt1, "rt_")
	})

	t.Run("GenerateRefreshTokenFamily", func(t *testing.T) {
		rtf1 := GenerateRefreshTokenFamily()
		rtf2 := GenerateRefreshTokenFamily()
		assert.NotEqual(t, rtf1, rtf2)
		assert.Contains(t, rtf1, "rtf_")
	})

	t.Run("GenerateIDTokenJTI", func(t *testing.T) {
		idt1 := GenerateIDTokenJTI()
		idt2 := GenerateIDTokenJTI()
		assert.NotEqual(t, idt1, idt2)
		assert.Contains(t, idt1, "idt_")
	})

	t.Run("GenerateDeviceCode", func(t *testing.T) {
		dc1 := GenerateDeviceCode()
		dc2 := GenerateDeviceCode()
		assert.NotEqual(t, dc1, dc2)
		assert.Contains(t, dc1, "dc_")
	})

	t.Run("GenerateLoginChallenge", func(t *testing.T) {
		lc1 := GenerateLoginChallenge()
		lc2 := GenerateLoginChallenge()
		assert.NotEqual(t, lc1, lc2)
		assert.Contains(t, lc1, "lc_")
	})

	t.Run("GenerateConsentChallenge", func(t *testing.T) {
		cc1 := GenerateConsentChallenge()
		cc2 := GenerateConsentChallenge()
		assert.NotEqual(t, cc1, cc2)
		assert.Contains(t, cc1, "cc_")
	})
}

// Test AuthCode validation
func TestAuthCode_IsValid(t *testing.T) {
	t.Run("ExpiredCode", func(t *testing.T) {
		authCode := &AuthCode{
			ExpiresAt: time.Now().UTC().Add(-1 * time.Hour),
			Used:      false,
		}
		err := authCode.IsValid()
		assert.ErrorIs(t, err, ErrCodeExpired)
	})

	t.Run("UsedCode", func(t *testing.T) {
		authCode := &AuthCode{
			ExpiresAt: time.Now().UTC().Add(1 * time.Hour),
			Used:      true,
		}
		err := authCode.IsValid()
		assert.ErrorIs(t, err, ErrCodeUsed)
	})

	t.Run("ValidCode", func(t *testing.T) {
		authCode := &AuthCode{
			ExpiresAt: time.Now().UTC().Add(1 * time.Hour),
			Used:      false,
		}
		assert.NoError(t, authCode.IsValid())
	})
}

// Test RefreshToken validation
func TestRefreshToken_IsValid(t *testing.T) {
	t.Run("ExpiredToken", func(t *testing.T) {
		rt := &RefreshToken{
			ExpiresAt: time.Now().UTC().Add(-1 * time.Hour),
			Active:    true,
		}
		err := rt.IsValid()
		assert.ErrorIs(t, err, ErrTokenExpired)
	})

	t.Run("InactiveToken", func(t *testing.T) {
		rt := &RefreshToken{
			ExpiresAt: time.Now().UTC().Add(1 * time.Hour),
			Active:    false,
		}
		err := rt.IsValid()
		assert.ErrorIs(t, err, ErrTokenInactive)
	})

	t.Run("ValidToken", func(t *testing.T) {
		rt := &RefreshToken{
			ExpiresAt: time.Now().UTC().Add(1 * time.Hour),
			Active:    true,
		}
		assert.NoError(t, rt.IsValid())
	})
}

// Test Client struct
func TestClient_Struct(t *testing.T) {
	t.Run("ValidClient", func(t *testing.T) {
		secretHash := "hashed-secret"
		client := &Client{
			ID:                      "client-123",
			Name:                    "Test Client",
			SecretHash:              &secretHash,
			RedirectURIs:            []string{"https://app.example.com/callback"},
			GrantTypes:              []string{"authorization_code", "refresh_token"},
			ResponseTypes:           []string{"code"},
			Scopes:                  []string{"openid", "profile", "email"},
			TokenEndpointAuthMethod: "client_secret_post",
			AccessTokenTTL:          900,
			RefreshTokenTTL:         2592000,
			IDTokenTTL:              3600,
			AllowOfflineAccess:      true,
		}

		assert.Equal(t, "client-123", client.ID)
		assert.Equal(t, "Test Client", client.Name)
		assert.Equal(t, 900, client.AccessTokenTTL)
		assert.True(t, client.AllowOfflineAccess)
	})
}

// Test DeviceCode struct
func TestDeviceCode_Struct(t *testing.T) {
	t.Run("ValidDeviceCode", func(t *testing.T) {
		dc := &DeviceCode{
			DeviceCode: "dc_test123",
			UserCode:   "ABCD-EFGH",
			ClientID:   "client-456",
			Scopes:     []string{"openid"},
			ExpiresAt:  time.Now().UTC().Add(15 * time.Minute),
			Interval:   5,
		}

		assert.Equal(t, "dc_test123", dc.DeviceCode)
		assert.Equal(t, "ABCD-EFGH", dc.UserCode)
		assert.Equal(t, 5, dc.Interval)
	})
}

// Test error types
func TestErrors(t *testing.T) {
	assert.Error(t, ErrNotFound)
	assert.Error(t, ErrCodeExpired)
	assert.Error(t, ErrCodeUsed)
	assert.Error(t, ErrTokenRevoked)
	assert.Error(t, ErrTokenExpired)
	assert.Error(t, ErrTokenInactive)
	assert.Error(t, ErrFamilyInvalidated)
	assert.Error(t, ErrPKCERequired)
	assert.Error(t, ErrAlreadyExists)
}

func TestAdditionalIDGeneratorsAndHelpers(t *testing.T) {
	t.Run("GenerateClientID has expected prefix and entropy", func(t *testing.T) {
		id1 := GenerateClientID()
		id2 := GenerateClientID()
		assert.NotEqual(t, id1, id2)
		assert.Regexp(t, regexp.MustCompile(`^oa2_[A-Za-z0-9_-]+$`), id1)
	})

	t.Run("GenerateAuthCode produces non-empty unique value", func(t *testing.T) {
		code1 := GenerateAuthCode()
		code2 := GenerateAuthCode()
		assert.NotEmpty(t, code1)
		assert.NotEqual(t, code1, code2)
		assert.NotContains(t, code1, " ")
	})

	t.Run("GenerateUserCode format and charset", func(t *testing.T) {
		code := GenerateUserCode()
		assert.Regexp(t, regexp.MustCompile(`^[BCDFGHJKLMNPQRSTVWXZ]{4}-[BCDFGHJKLMNPQRSTVWXZ]{4}$`), code)
	})

	t.Run("isDuplicateKeyError branch coverage", func(t *testing.T) {
		assert.False(t, isDuplicateKeyError(nil))
		assert.False(t, isDuplicateKeyError(errors.New("other")))
		assert.True(t, isDuplicateKeyError(&pgconn.PgError{Code: "23505"}))
		assert.False(t, isDuplicateKeyError(&pgconn.PgError{Code: "22001"}))
	})

	t.Run("nowUTC returns UTC location", func(t *testing.T) {
		now := nowUTC()
		assert.Equal(t, time.UTC, now.Location())
	})
}
