// Package revocation provides OAuth2 token revocation.
package revocation

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strings"

	"github.com/aegion/aegion/modules/oauth2/store"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidClient = errors.New("invalid_client")
	ErrInvalidToken  = errors.New("invalid_token")
)

// RevocationStore interface for revocation operations.
type RevocationStore interface {
	GetClient(ctx context.Context, id string) (*store.Client, error)
	RevokeAccessToken(ctx context.Context, jti string) error
	GetRefreshToken(ctx context.Context, id string) (*store.RefreshToken, error)
	InvalidateRefreshTokenFamily(ctx context.Context, familyID string) (int64, error)
}

// RevocationService handles OAuth2 token revocation.
type RevocationService struct {
	store RevocationStore
}

// NewRevocationService creates a new revocation service.
func NewRevocationService(store RevocationStore) *RevocationService {
	return &RevocationService{store: store}
}

// RevocationRequest represents a token revocation request (RFC 7009).
type RevocationRequest struct {
	Token         string
	TokenTypeHint string
	ClientID      string
	ClientSecret  string
}

// RevokeToken revokes an access or refresh token.
// Per RFC 7009: Invalid tokens do not cause an error, the service responds with 200 OK.
func (s *RevocationService) RevokeToken(ctx context.Context, req *RevocationRequest) error {
	// Authenticate client
	if req.ClientID == "" {
		return ErrInvalidClient
	}

	client, err := s.store.GetClient(ctx, req.ClientID)
	if err != nil {
		return ErrInvalidClient
	}

	// Verify client can authenticate
	if client.TokenEndpointAuthMethod != "none" {
		if req.ClientSecret == "" {
			return ErrInvalidClient
		}
		// For basic/post auth methods, verify secret
		if client.SecretHash != nil && !authenticateClientSecret(*client.SecretHash, req.ClientSecret) {
			return ErrInvalidClient
		}
	}

	// Determine token type and revoke
	if req.TokenTypeHint == "" || req.TokenTypeHint == "access_token" {
		// Try as access token (JTI format)
		if err := s.store.RevokeAccessToken(ctx, req.Token); err == nil {
			return nil
		}
	}

	if req.TokenTypeHint == "" || req.TokenTypeHint == "refresh_token" {
		// Try as refresh token
		refreshToken, err := s.store.GetRefreshToken(ctx, req.Token)
		if err == nil {
			// Verify token belongs to client
			if refreshToken.ClientID != client.ID {
				// Per RFC 7009: respond with 200 OK even if token doesn't belong to client
				return nil
			}
			// Invalidate entire family
			_, _ = s.store.InvalidateRefreshTokenFamily(ctx, refreshToken.FamilyID)
			return nil
		}
	}

	// Token not found or invalid - return success per RFC 7009 Section 2.2
	return nil
}

// authenticateClientSecret verifies a client secret using constant-time comparison.
func authenticateClientSecret(hashedSecret, plainSecret string) bool {
	if strings.HasPrefix(hashedSecret, "$2") {
		return bcrypt.CompareHashAndPassword([]byte(hashedSecret), []byte(plainSecret)) == nil
	}

	// Fallback for legacy plaintext secrets.
	return subtle.ConstantTimeCompare([]byte(hashedSecret), []byte(plainSecret)) == 1
}

// ExtractTokenFromHeader extracts Bearer token from Authorization header.
func ExtractTokenFromHeader(authHeader string) (string, error) {
	if authHeader == "" {
		return "", ErrInvalidToken
	}
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		return "", ErrInvalidToken
	}
	token := strings.TrimSpace(parts[1])
	if token == "" {
		return "", ErrInvalidToken
	}
	return token, nil
}

// ExtractClientCredentials extracts client credentials from Authorization header (Basic auth).
func ExtractClientCredentials(authHeader string) (clientID, clientSecret string, err error) {
	if authHeader == "" {
		return "", "", ErrInvalidClient
	}
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || strings.ToLower(parts[0]) != "basic" {
		return "", "", ErrInvalidClient
	}

	decoded, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return "", "", ErrInvalidClient
	}

	credentials := strings.SplitN(string(decoded), ":", 2)
	if len(credentials) != 2 {
		return "", "", ErrInvalidClient
	}

	return credentials[0], credentials[1], nil
}
