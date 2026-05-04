// Package token provides OAuth2 token issuance and management.
package token

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"
	"github.com/aegion/aegion/modules/oauth2/service/authorization"
	"github.com/aegion/aegion/modules/oauth2/store"
)

var (
	ErrInvalidGrant         = errors.New("invalid_grant")
	ErrInvalidClient        = errors.New("invalid_client")
	ErrInvalidRequest       = errors.New("invalid_request")
	ErrUnauthorizedClient   = errors.New("unauthorized_client")
	ErrUnsupportedGrantType = errors.New("unsupported_grant_type")
	ErrInvalidScope         = errors.New("invalid_scope")
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

// DeviceCodeTokenRequest represents a device code token exchange request.
type DeviceCodeTokenRequest struct {
	ClientID     string    `json:"client_id"`
	ClientSecret string    `json:"client_secret,omitempty"`
	IdentityID   string    `json:"identity_id"`
	SessionID    string    `json:"session_id,omitempty"`
	Scopes       []string  `json:"scopes,omitempty"`
	Audience     []string  `json:"audience,omitempty"`
	AuthTime     time.Time `json:"auth_time,omitempty"`
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

// IntrospectionRequest represents an RFC 7662 token introspection request.
type IntrospectionRequest struct {
	Token        string
	ClientID     string
	ClientSecret string
}

// IntrospectionResponse represents an RFC 7662 token introspection response.
type IntrospectionResponse struct {
	Active    bool     `json:"active"`
	ClientID  string   `json:"client_id,omitempty"`
	Subject   string   `json:"sub,omitempty"`
	Scope     string   `json:"scope,omitempty"`
	ExpiresAt int64    `json:"exp,omitempty"`
	Issuer    string   `json:"iss,omitempty"`
	Audience  []string `json:"aud,omitempty"`
	TokenType string   `json:"token_type,omitempty"`
}

// ExchangeAuthorizationCode exchanges an authorization code for tokens.
func (s *TokenService) ExchangeAuthorizationCode(ctx context.Context, req *TokenRequest) (*TokenResponse, error) {
	if req == nil {
		return nil, ErrInvalidRequest
	}
	if strings.TrimSpace(req.ClientID) == "" || strings.TrimSpace(req.Code) == "" || strings.TrimSpace(req.RedirectURI) == "" {
		return nil, ErrInvalidRequest
	}

	// Validate client
	client, err := s.store.GetClient(ctx, req.ClientID)
	if err != nil {
		return nil, ErrInvalidClient
	}
	if err := authenticateClient(client, req.ClientSecret); err != nil {
		return nil, err
	}
	if len(client.GrantTypes) > 0 && !client.HasGrantType("authorization_code") {
		return nil, ErrUnauthorizedClient
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
	if req == nil {
		return nil, ErrInvalidRequest
	}
	if strings.TrimSpace(req.ClientID) == "" || strings.TrimSpace(req.RefreshToken) == "" {
		return nil, ErrInvalidRequest
	}

	// Validate client
	client, err := s.store.GetClient(ctx, req.ClientID)
	if err != nil {
		return nil, ErrInvalidClient
	}
	if err := authenticateClient(client, req.ClientSecret); err != nil {
		return nil, err
	}
	if len(client.GrantTypes) > 0 && !client.HasGrantType("refresh_token") {
		return nil, ErrUnauthorizedClient
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
	if resp.RefreshToken == nil || strings.TrimSpace(*resp.RefreshToken) == "" {
		return nil, fmt.Errorf("%w: refreshed token set did not include a refresh token", ErrInvalidScope)
	}

	// Mark old refresh token as used and link to new one
	gracePeriod, err := refreshTokenGracePeriod(client)
	if err != nil {
		return nil, err
	}
	if err := s.store.MarkRefreshTokenUsed(ctx, req.RefreshToken, *resp.RefreshToken, gracePeriod); err != nil {
		return nil, err
	}

	return resp, nil
}

// ExchangeDeviceCode exchanges an approved device code context for tokens.
func (s *TokenService) ExchangeDeviceCode(ctx context.Context, req *DeviceCodeTokenRequest) (*TokenResponse, error) {
	if req == nil {
		return nil, ErrInvalidRequest
	}
	if strings.TrimSpace(req.ClientID) == "" || strings.TrimSpace(req.IdentityID) == "" {
		return nil, ErrInvalidRequest
	}

	client, err := s.store.GetClient(ctx, req.ClientID)
	if err != nil {
		return nil, ErrInvalidClient
	}
	if err := authenticateClient(client, req.ClientSecret); err != nil {
		return nil, err
	}
	if len(client.GrantTypes) > 0 &&
		!client.HasGrantType("urn:ietf:params:oauth:grant-type:device_code") &&
		!client.HasGrantType("device_code") {
		return nil, ErrUnauthorizedClient
	}
	for _, requestedScope := range req.Scopes {
		if !client.HasScope(requestedScope) {
			return nil, ErrInvalidScope
		}
	}

	authTime := req.AuthTime
	if authTime.IsZero() {
		authTime = time.Now().UTC()
	}

	return s.issueTokens(
		ctx,
		client,
		req.IdentityID,
		req.SessionID,
		req.Scopes,
		req.Audience,
		nil,
		"aal1",
		[]string{"pwd"},
		authTime,
	)
}

// IntrospectToken returns token metadata for active access tokens.
func (s *TokenService) IntrospectToken(ctx context.Context, req *IntrospectionRequest) (*IntrospectionResponse, error) {
	if req == nil || strings.TrimSpace(req.Token) == "" || strings.TrimSpace(req.ClientID) == "" {
		return nil, ErrInvalidRequest
	}

	client, err := s.store.GetClient(ctx, req.ClientID)
	if err != nil {
		return nil, ErrInvalidClient
	}
	if err := authenticateClient(client, req.ClientSecret); err != nil {
		return nil, err
	}
	if client.TokenEndpointAuthMethod == "" || client.TokenEndpointAuthMethod == "none" {
		return nil, ErrUnauthorizedClient
	}

	accessToken, err := s.resolveAccessTokenForIntrospection(ctx, req.Token)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return &IntrospectionResponse{Active: false}, nil
		}
		return nil, err
	}

	if accessToken.Revoked || time.Now().UTC().After(accessToken.ExpiresAt) {
		return &IntrospectionResponse{Active: false}, nil
	}
	if accessToken.ClientID != client.ID {
		return nil, ErrUnauthorizedClient
	}

	return &IntrospectionResponse{
		Active:    true,
		ClientID:  accessToken.ClientID,
		Subject:   accessToken.Subject,
		Scope:     strings.Join(accessToken.Scopes, " "),
		ExpiresAt: accessToken.ExpiresAt.Unix(),
		Issuer:    accessToken.Issuer,
		Audience:  append([]string(nil), accessToken.Audience...),
		TokenType: "Bearer",
	}, nil
}

type accessTokenReader interface {
	GetAccessToken(ctx context.Context, jti string) (*store.AccessToken, error)
}

type accessTokenSignatureReader interface {
	GetAccessTokenBySignature(ctx context.Context, signature string) (*store.AccessToken, error)
}

func (s *TokenService) resolveAccessTokenForIntrospection(ctx context.Context, token string) (*store.AccessToken, error) {
	reader, ok := s.store.(accessTokenReader)
	if ok {
		accessToken, err := reader.GetAccessToken(ctx, token)
		if err == nil {
			return accessToken, nil
		}
		if !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
	}

	sigReader, ok := s.store.(accessTokenSignatureReader)
	if !ok {
		return nil, store.ErrNotFound
	}
	return sigReader.GetAccessTokenBySignature(ctx, accessTokenSignature(token))
}

// issueTokens issues a complete token set (access, refresh, ID).
func (s *TokenService) issueTokens(ctx context.Context, client *store.Client, identityID, sessionID string, scopes, audience []string, nonce *string, acr string, amr []string, authTime time.Time) (*TokenResponse, error) {
	now := time.Now().UTC()

	subject, err := resolveTokenSubject(client, identityID, s.issuer)
	if err != nil {
		return nil, err
	}

	// Issue access token
	accessJTI, err := store.GenerateAccessTokenJTI()
	if err != nil {
		return nil, fmt.Errorf("failed to generate access token JTI: %w", err)
	}
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
		Signature:  stringPointer(accessTokenSignature(accessTokenJWT)),
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
		refreshTokenID, err := store.GenerateRefreshToken()
		if err != nil {
			return nil, fmt.Errorf("failed to generate refresh token: %w", err)
		}
		familyID, err := store.GenerateRefreshTokenFamily()
		if err != nil {
			return nil, fmt.Errorf("failed to generate refresh token family: %w", err)
		}
		refreshExpiresAt := now.Add(time.Duration(client.RefreshTokenTTL) * time.Second)

		refreshToken := &store.RefreshToken{
			ID:             refreshTokenID,
			FamilyID:       familyID,
			ClientID:       client.ID,
			IdentityID:     identityID,
			SessionID:      sessionID,
			Scopes:         scopes,
			Audience:       audience,
			Active:         true,
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
		idJTI, err := store.GenerateIDTokenJTI()
		if err != nil {
			return nil, fmt.Errorf("failed to generate ID token JTI: %w", err)
		}
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

func resolveTokenSubject(client *store.Client, identityID, issuer string) (string, error) {
	identityID = strings.TrimSpace(identityID)
	if identityID == "" {
		return "", ErrInvalidRequest
	}
	if client == nil {
		return "", ErrInvalidClient
	}

	switch strings.ToLower(strings.TrimSpace(client.SubjectType)) {
	case "", "public":
		return identityID, nil
	case "pairwise":
		sector := resolvePairwiseSector(client)
		if sector == "" {
			return "", ErrInvalidClient
		}
		return computePairwiseSubject(identityID, strings.TrimSpace(issuer), sector), nil
	default:
		return "", ErrInvalidClient
	}
}

func resolvePairwiseSector(client *store.Client) string {
	if client == nil {
		return ""
	}
	if client.SectorIdentifierURI != nil {
		if sector := normalizeSectorIdentifier(*client.SectorIdentifierURI); sector != "" {
			return sector
		}
	}
	for _, redirectURI := range client.RedirectURIs {
		if sector := normalizeSectorIdentifier(redirectURI); sector != "" {
			return sector
		}
	}
	return ""
}

func normalizeSectorIdentifier(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return strings.ToLower(trimmed)
	}
	if host := strings.TrimSpace(parsed.Hostname()); host != "" {
		return strings.ToLower(host)
	}
	return strings.ToLower(trimmed)
}

func computePairwiseSubject(identityID, issuer, sector string) string {
	base := strings.Join([]string{issuer, sector, identityID}, "|")
	sum := sha256.Sum256([]byte(base))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func accessTokenSignature(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func stringPointer(v string) *string {
	return &v
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

func refreshTokenGracePeriod(client *store.Client) (time.Duration, error) {
	if client == nil {
		return 0, ErrInvalidClient
	}
	if client.Metadata == nil {
		return 0, nil
	}
	raw := strings.TrimSpace(client.Metadata["refresh_token_grace_seconds"])
	if raw == "" {
		raw = strings.TrimSpace(client.Metadata["refresh_token_grace_period_seconds"])
	}
	if raw == "" {
		return 0, nil
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds < 0 {
		return 0, ErrInvalidClient
	}
	return time.Duration(seconds) * time.Second, nil
}

func authenticateClient(client *store.Client, clientSecret string) error {
	if client == nil {
		return ErrInvalidClient
	}

	switch client.TokenEndpointAuthMethod {
	case "", "none":
		return nil
	case "client_secret_basic", "client_secret_post":
		// continue
	default:
		return ErrInvalidClient
	}

	if client.SecretHash == nil || strings.TrimSpace(*client.SecretHash) == "" {
		return ErrInvalidClient
	}
	if strings.TrimSpace(clientSecret) == "" {
		return ErrInvalidClient
	}
	matches, verifyErr := platformcrypto.VerifyPassword(clientSecret, *client.SecretHash)
	if verifyErr != nil || !matches {
		return ErrInvalidClient
	}
	return nil
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
