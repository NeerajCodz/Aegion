// Package token provides OAuth2 token issuance and management.
package token

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aegion/aegion/modules/oauth2/service/authorization"
	"github.com/aegion/aegion/modules/oauth2/store"
)

var (
	ErrInvalidGrant        = errors.New("invalid_grant")
	ErrInvalidClient       = errors.New("invalid_client")
	ErrUnsupportedGrantType = errors.New("unsupported_grant_type")
	ErrInvalidScope        = errors.New("invalid_scope")
)

// TokenStore interface for token operations.
type TokenStore interface {
	GetClient(ctx context.Context, id string) (*store.Client, error)
	GetAuthCode(ctx context.Context, code string) (*store.AuthCode, error)
	MarkAuthCodeUsed(ctx context.Context, code string) error
	CreateAccessToken(ctx context.Context, token *store.AccessToken) error
	CreateRefreshToken(ctx context.Context, token *store.RefreshToken) error
	CreateIDToken(ctx context.Context, token *store.IDToken) error
	GetRefreshToken(ctx context.Context, id string) (*store.RefreshToken, error)
	MarkRefreshTokenUsed(ctx context.Context, id, successorID string, gracePeriod time.Duration) error
	InvalidateRefreshTokenFamily(ctx context.Context, familyID string) (int64, error)
	RevokeAccessToken(ctx context.Context, jti string) error
	RevokeRefreshTokensBySession(ctx context.Context, sessionID string) (int64, error)
	RevokeAccessTokensBySession(ctx context.Context, sessionID string) (int64, error)
}

// JWTSigner interface for signing JWTs.
type JWTSigner interface {
	SignAccessToken(claims map[string]interface{}) (string, error)
	SignIDToken(claims map[string]interface{}) (string, error)
}

// TokenService handles OAuth2 token operations.
type TokenService struct {
	store  TokenStore
	signer JWTSigner
	issuer string
}

// NewTokenService creates a new token service.
func NewTokenService(store TokenStore, signer JWTSigner, issuer string) *TokenService {
	return &TokenService{
		store:  store,
		signer: signer,
		issuer: issuer,
	}
}

// TokenRequest represents a token exchange request.
type TokenRequest struct {
	GrantType    string `json:"grant_type"`
	Code         string `json:"code,omitempty"`
	RedirectURI  string `json:"redirect_uri,omitempty"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret,omitempty"`
	CodeVerifier string `json:"code_verifier,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// TokenResponse represents a successful token response.
type TokenResponse struct {
	AccessToken  string  `json:"access_token"`
	TokenType    string  `json:"token_type"`
	ExpiresIn    int     `json:"expires_in"`
	RefreshToken *string `json:"refresh_token,omitempty"`
	IDToken      *string `json:"id_token,omitempty"`
	Scope        string  `json:"scope,omitempty"`
}

// ExchangeAuthorizationCode exchanges an authorization code for tokens.
func (s *TokenService) ExchangeAuthorizationCode(ctx context.Context, req *TokenRequest) (*TokenResponse, error) {
	// Validate client
	client, err := s.store.GetClient(ctx, req.ClientID)
	if err != nil {
		return nil, ErrInvalidClient
	}

	// Retrieve authorization code
	authCode, err := s.store.GetAuthCode(ctx, req.Code)
	if err != nil {
		return nil, ErrInvalidGrant
	}

	// Validate code
	if err := authCode.IsValid(); err != nil {
		return nil, ErrInvalidGrant
	}

	// Verify code belongs to client
	if authCode.ClientID != client.ID {
		return nil, ErrInvalidGrant
	}

	// Verify redirect URI matches
	if authCode.RedirectURI != req.RedirectURI {
		return nil, ErrInvalidGrant
	}

	// Verify PKCE if present
	if authCode.CodeChallenge != nil && *authCode.CodeChallenge != "" {
		if req.CodeVerifier == "" {
			return nil, fmt.Errorf("%w: code_verifier required", store.ErrPKCERequired)
		}
		method := "S256"
		if authCode.CodeChallengeMethod != nil {
			method = *authCode.CodeChallengeMethod
		}
		if err := authorization.VerifyPKCE(req.CodeVerifier, *authCode.CodeChallenge, method); err != nil {
			return nil, err
		}
	}

	// Mark code as used
	if err := s.store.MarkAuthCodeUsed(ctx, req.Code); err != nil {
		return nil, ErrInvalidGrant
	}

	// Issue tokens
	return s.issueTokens(ctx, client, authCode.IdentityID, authCode.SessionID, authCode.Scopes, authCode.Audience, authCode.Nonce, authCode.ACR, authCode.AMR, authCode.AuthTime)
}

// RefreshAccessToken refreshes an access token using a refresh token.
func (s *TokenService) RefreshAccessToken(ctx context.Context, req *TokenRequest) (*TokenResponse, error) {
	// Validate client
	client, err := s.store.GetClient(ctx, req.ClientID)
	if err != nil {
		return nil, ErrInvalidClient
	}

	// Retrieve refresh token
	refreshToken, err := s.store.GetRefreshToken(ctx, req.RefreshToken)
	if err != nil {
		return nil, ErrInvalidGrant
	}

	// Validate token
	if err := refreshToken.IsValid(); err != nil {
		return nil, ErrInvalidGrant
	}

	// Check if token belongs to client
	if refreshToken.ClientID != client.ID {
		return nil, ErrInvalidGrant
	}

	// Detect replay attack
	if refreshToken.Used {
		// Check if within grace period
		inGracePeriod := false
		if refreshToken.GracePeriodExpiresAt != nil && time.Now().UTC().Before(*refreshToken.GracePeriodExpiresAt) {
			inGracePeriod = true
		}

		if !inGracePeriod {
			// Invalidate entire family
			_, _ = s.store.InvalidateRefreshTokenFamily(ctx, refreshToken.FamilyID)
			return nil, fmt.Errorf("%w: token replay detected", store.ErrFamilyInvalidated)
		}
		// Within grace period - return existing successor if available
		if refreshToken.SuccessorID != nil {
			return s.RefreshAccessToken(ctx, &TokenRequest{
				GrantType:    req.GrantType,
				ClientID:     req.ClientID,
				ClientSecret: req.ClientSecret,
				RefreshToken: *refreshToken.SuccessorID,
			})
		}
	}

	// Parse requested scopes
	scopes := refreshToken.Scopes
	if req.Scope != "" {
		requestedScopes := parseScopes(req.Scope)
		// Verify requested scopes are subset of original
		for _, rs := range requestedScopes {
			found := false
			for _, os := range refreshToken.Scopes {
				if rs == os {
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("%w: scope '%s' not in original grant", ErrInvalidScope, rs)
			}
		}
		scopes = requestedScopes
	}

	// Issue new tokens with rotation
	resp, err := s.issueTokens(ctx, client, refreshToken.IdentityID, refreshToken.SessionID, scopes, refreshToken.Audience, nil, "aal1", []string{"pwd"}, time.Now().UTC())
	if err != nil {
		return nil, err
	}

	// Mark old refresh token as used and link to new one
	gracePeriod := 0 * time.Second // TODO: make configurable
	if err := s.store.MarkRefreshTokenUsed(ctx, req.RefreshToken, *resp.RefreshToken, gracePeriod); err != nil {
		return nil, err
	}

	return resp, nil
}

// issueTokens issues a complete token set (access, refresh, ID).
func (s *TokenService) issueTokens(ctx context.Context, client *store.Client, identityID, sessionID string, scopes, audience []string, nonce *string, acr string, amr []string, authTime time.Time) (*TokenResponse, error) {
	now := time.Now().UTC()

	// Determine subject (TODO: support pairwise)
	subject := identityID

	// Issue access token
	accessJTI := store.GenerateAccessTokenJTI()
	expiresAt := now.Add(time.Duration(client.AccessTokenTTL) * time.Second)

	accessClaims := map[string]interface{}{
		"iss":       s.issuer,
		"sub":       subject,
		"aud":       audience,
		"iat":       now.Unix(),
		"exp":       expiresAt.Unix(),
		"jti":       accessJTI,
		"client_id": client.ID,
		"scope":     strings.Join(scopes, " "),
	}

	accessTokenJWT, err := s.signer.SignAccessToken(accessClaims)
	if err != nil {
		return nil, fmt.Errorf("failed to sign access token: %w", err)
	}

	// Store access token metadata
	accessToken := &store.AccessToken{
		JTI:        accessJTI,
		ClientID:   client.ID,
		IdentityID: identityID,
		SessionID:  sessionID,
		Scopes:     scopes,
		Audience:   audience,
		Issuer:     s.issuer,
		Subject:    subject,
		ExpiresAt:  expiresAt,
	}
	if err := s.store.CreateAccessToken(ctx, accessToken); err != nil {
		return nil, err
	}

	resp := &TokenResponse{
		AccessToken: accessTokenJWT,
		TokenType:   "Bearer",
		ExpiresIn:   client.AccessTokenTTL,
		Scope:       strings.Join(scopes, " "),
	}

	// Issue refresh token if offline_access scope is present
	if client.AllowOfflineAccess && hasScope(scopes, "offline_access") {
		refreshTokenID := store.GenerateRefreshToken()
		familyID := store.GenerateRefreshTokenFamily()
		refreshExpiresAt := now.Add(time.Duration(client.RefreshTokenTTL) * time.Second)

		refreshToken := &store.RefreshToken{
			ID:             refreshTokenID,
			FamilyID:       familyID,
			ClientID:       client.ID,
			IdentityID:     identityID,
			SessionID:      sessionID,
			Scopes:         scopes,
			Audience:       audience,
			AccessTokenJTI: &accessJTI,
			ExpiresAt:      refreshExpiresAt,
		}
		if err := s.store.CreateRefreshToken(ctx, refreshToken); err != nil {
			return nil, err
		}

		resp.RefreshToken = &refreshTokenID
	}

	// Issue ID token if openid scope is present
	if hasScope(scopes, "openid") {
		idJTI := store.GenerateIDTokenJTI()
		idExpiresAt := now.Add(time.Duration(client.IDTokenTTL) * time.Second)

		// Compute at_hash (access token hash)
		atHash := computeATHash(accessTokenJWT)

		idClaims := map[string]interface{}{
			"iss":       s.issuer,
			"sub":       subject,
			"aud":       client.ID,
			"iat":       now.Unix(),
			"exp":       idExpiresAt.Unix(),
			"auth_time": authTime.Unix(),
			"jti":       idJTI,
			"acr":       acr,
			"amr":       amr,
			"at_hash":   atHash,
		}

		if nonce != nil && *nonce != "" {
			idClaims["nonce"] = *nonce
		}

		idTokenJWT, err := s.signer.SignIDToken(idClaims)
		if err != nil {
			return nil, fmt.Errorf("failed to sign ID token: %w", err)
		}

		// Store ID token metadata
		idToken := &store.IDToken{
			JTI:        idJTI,
			ClientID:   client.ID,
			IdentityID: identityID,
			SessionID:  sessionID,
			Nonce:      nonce,
			ATHash:     &atHash,
			ACR:        acr,
			AMR:        amr,
			AuthTime:   authTime,
			ExpiresAt:  idExpiresAt,
		}
		if err := s.store.CreateIDToken(ctx, idToken); err != nil {
			return nil, err
		}

		resp.IDToken = &idTokenJWT
	}

	return resp, nil
}

// RevokeToken revokes an access or refresh token.
func (s *TokenService) RevokeToken(ctx context.Context, token, tokenTypeHint string) error {
	// Try as access token first
	if tokenTypeHint == "" || tokenTypeHint == "access_token" {
		if err := s.store.RevokeAccessToken(ctx, token); err == nil {
			return nil
		}
	}

	// Try as refresh token
	if tokenTypeHint == "" || tokenTypeHint == "refresh_token" {
		refreshToken, err := s.store.GetRefreshToken(ctx, token)
		if err == nil {
			_, _ = s.store.InvalidateRefreshTokenFamily(ctx, refreshToken.FamilyID)
			return nil
		}
	}

	// Token not found is success per RFC 7009
	return nil
}

// computeATHash computes the at_hash claim for ID tokens.
func computeATHash(accessToken string) string {
	h := sha256.Sum256([]byte(accessToken))
	leftHalf := h[:len(h)/2]
	return base64.RawURLEncoding.EncodeToString(leftHalf)
}

// hasScope checks if a scope is in the list.
func hasScope(scopes []string, scope string) bool {
	for _, s := range scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// parseScopes parses a space-separated scope string.
func parseScopes(scope string) []string {
	if scope == "" {
		return []string{}
	}
	parts := strings.Split(scope, " ")
	var scopes []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			scopes = append(scopes, p)
		}
	}
	return scopes
}

// MockJWTSigner is a mock JWT signer for testing.
type MockJWTSigner struct{}

func (m *MockJWTSigner) SignAccessToken(claims map[string]interface{}) (string, error) {
	data, _ := json.Marshal(claims)
	return "mock_access_" + base64.RawURLEncoding.EncodeToString(data), nil
}

func (m *MockJWTSigner) SignIDToken(claims map[string]interface{}) (string, error) {
	data, _ := json.Marshal(claims)
	return "mock_id_" + base64.RawURLEncoding.EncodeToString(data), nil
}
