// Package grants implements additional OAuth2 grant types.
package grants

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/aegion/aegion/modules/oauth2/store"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidClient      = errors.New("invalid_client")
	ErrInvalidScope       = errors.New("invalid_scope")
	ErrUnauthorizedClient = errors.New("unauthorized_client")
)

// GrantStore interface for grant operations.
type GrantStore interface {
	GetClient(ctx context.Context, id string) (*store.Client, error)
	CreateAccessToken(ctx context.Context, token *store.AccessToken) error
}

// JWTSigner interface for signing JWTs.
type JWTSigner interface {
	SignAccessToken(claims map[string]interface{}) (string, error)
}

// ClientCredentialsService handles client credentials grant.
type ClientCredentialsService struct {
	store  GrantStore
	signer JWTSigner
	issuer string
}

// NewClientCredentialsService creates a new client credentials service.
func NewClientCredentialsService(store GrantStore, signer JWTSigner, issuer string) *ClientCredentialsService {
	return &ClientCredentialsService{
		store:  store,
		signer: signer,
		issuer: issuer,
	}
}

// ClientCredentialsRequest represents a client credentials grant request.
type ClientCredentialsRequest struct {
	ClientID     string
	ClientSecret string
	Scope        string
}

// ClientCredentialsResponse represents a token response.
type ClientCredentialsResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	Scope       string `json:"scope,omitempty"`
}

// IssueClientCredentials issues an access token for client credentials grant.
func (s *ClientCredentialsService) IssueClientCredentials(ctx context.Context, req *ClientCredentialsRequest) (*ClientCredentialsResponse, error) {
	if req == nil || strings.TrimSpace(req.ClientID) == "" {
		return nil, ErrInvalidClient
	}

	// Validate client
	client, err := s.store.GetClient(ctx, req.ClientID)
	if err != nil {
		return nil, ErrInvalidClient
	}
	if err := authenticateClient(client, req.ClientSecret); err != nil {
		return nil, err
	}

	// Verify grant is allowed
	if !hasGrantType(client.GrantTypes, "client_credentials") {
		return nil, ErrUnauthorizedClient
	}

	// Parse requested scopes
	scopes := parseScopes(req.Scope)

	// Validate scopes against client allowed scopes
	for _, requestedScope := range scopes {
		allowed := false
		for _, allowedScope := range client.Scopes {
			if requestedScope == allowedScope {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil, ErrInvalidScope
		}
	}

	// Issue access token (no subject, client is the subject)
	now := time.Now().UTC()
	jti := store.GenerateAccessTokenJTI()
	expiresAt := now.Add(time.Duration(client.AccessTokenTTL) * time.Second)

	claims := map[string]interface{}{
		"iss":       s.issuer,
		"sub":       client.ID,
		"aud":       client.ID,
		"iat":       now.Unix(),
		"exp":       expiresAt.Unix(),
		"jti":       jti,
		"client_id": client.ID,
		"scope":     strings.Join(scopes, " "),
	}

	accessTokenJWT, err := s.signer.SignAccessToken(claims)
	if err != nil {
		return nil, err
	}

	// Store access token metadata
	accessToken := &store.AccessToken{
		JTI:        jti,
		ClientID:   client.ID,
		IdentityID: "", // No identity for client credentials
		Scopes:     scopes,
		Audience:   []string{client.ID},
		Issuer:     s.issuer,
		Subject:    client.ID,
		ExpiresAt:  expiresAt,
	}
	if err := s.store.CreateAccessToken(ctx, accessToken); err != nil {
		return nil, err
	}

	return &ClientCredentialsResponse{
		AccessToken: accessTokenJWT,
		TokenType:   "Bearer",
		ExpiresIn:   client.AccessTokenTTL,
		Scope:       strings.Join(scopes, " "),
	}, nil
}

// JWTBearerService handles JWT bearer grant (RFC 7523).
type JWTBearerService struct {
	store     GrantStore
	signer    JWTSigner
	issuer    string
	validator JWTValidator
}

// JWTValidator interface for validating JWT assertions.
type JWTValidator interface {
	ValidateJWTAssertion(ctx context.Context, assertion string, clientID string) (*JWTAssertionClaims, error)
}

// JWTAssertionClaims represents validated JWT assertion claims.
type JWTAssertionClaims struct {
	Issuer    string
	Subject   string
	Audience  []string
	ExpiresAt time.Time
	IssuedAt  time.Time
	Scopes    []string
}

// NewJWTBearerService creates a new JWT bearer service.
func NewJWTBearerService(store GrantStore, signer JWTSigner, issuer string, validator JWTValidator) *JWTBearerService {
	return &JWTBearerService{
		store:     store,
		signer:    signer,
		issuer:    issuer,
		validator: validator,
	}
}

// JWTBearerRequest represents a JWT bearer grant request.
type JWTBearerRequest struct {
	GrantType    string
	Assertion    string
	Scope        string
	ClientID     string
	ClientSecret string
}

// IssueJWTBearer issues an access token for JWT bearer grant.
func (s *JWTBearerService) IssueJWTBearer(ctx context.Context, req *JWTBearerRequest) (*ClientCredentialsResponse, error) {
	if req == nil || strings.TrimSpace(req.ClientID) == "" || strings.TrimSpace(req.Assertion) == "" {
		return nil, ErrInvalidClient
	}

	// Validate client
	client, err := s.store.GetClient(ctx, req.ClientID)
	if err != nil {
		return nil, ErrInvalidClient
	}
	if err := authenticateClient(client, req.ClientSecret); err != nil {
		return nil, err
	}

	// Verify grant is allowed
	if !hasGrantType(client.GrantTypes, "urn:ietf:params:oauth:grant-type:jwt-bearer") {
		return nil, ErrUnauthorizedClient
	}

	// Validate JWT assertion
	claims, err := s.validator.ValidateJWTAssertion(ctx, req.Assertion, req.ClientID)
	if err != nil {
		return nil, err
	}

	// Parse requested scopes (use assertion scopes if none requested)
	scopes := claims.Scopes
	if req.Scope != "" {
		scopes = parseScopes(req.Scope)
	}
	if !scopesAllowed(scopes, client.Scopes) {
		return nil, ErrInvalidScope
	}

	// Issue access token
	now := time.Now().UTC()
	jti := store.GenerateAccessTokenJTI()
	expiresAt := now.Add(time.Duration(client.AccessTokenTTL) * time.Second)

	tokenClaims := map[string]interface{}{
		"iss":       s.issuer,
		"sub":       claims.Subject,
		"aud":       claims.Audience,
		"iat":       now.Unix(),
		"exp":       expiresAt.Unix(),
		"jti":       jti,
		"client_id": client.ID,
		"scope":     strings.Join(scopes, " "),
	}

	accessTokenJWT, err := s.signer.SignAccessToken(tokenClaims)
	if err != nil {
		return nil, err
	}

	// Store access token metadata
	accessToken := &store.AccessToken{
		JTI:        jti,
		ClientID:   client.ID,
		IdentityID: claims.Subject,
		Scopes:     scopes,
		Audience:   claims.Audience,
		Issuer:     s.issuer,
		Subject:    claims.Subject,
		ExpiresAt:  expiresAt,
	}
	if err := s.store.CreateAccessToken(ctx, accessToken); err != nil {
		return nil, err
	}

	return &ClientCredentialsResponse{
		AccessToken: accessTokenJWT,
		TokenType:   "Bearer",
		ExpiresIn:   client.AccessTokenTTL,
		Scope:       strings.Join(scopes, " "),
	}, nil
}

// hasGrantType checks if a grant type is in the list.
func hasGrantType(grantTypes []string, grantType string) bool {
	for _, gt := range grantTypes {
		if gt == grantType {
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

func authenticateClient(client *store.Client, clientSecret string) error {
	if client == nil {
		return ErrInvalidClient
	}
	if client.TokenEndpointAuthMethod != "client_secret_basic" && client.TokenEndpointAuthMethod != "client_secret_post" {
		return nil
	}
	if client.SecretHash == nil || strings.TrimSpace(*client.SecretHash) == "" {
		return ErrInvalidClient
	}
	if strings.TrimSpace(clientSecret) == "" {
		return ErrInvalidClient
	}
	if err := bcrypt.CompareHashAndPassword([]byte(*client.SecretHash), []byte(clientSecret)); err != nil {
		return ErrInvalidClient
	}
	return nil
}

func scopesAllowed(scopes []string, allowed []string) bool {
	if len(scopes) == 0 {
		return true
	}
	if len(allowed) == 0 {
		return false
	}
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, scope := range allowed {
		allowedSet[scope] = struct{}{}
	}
	for _, scope := range scopes {
		if _, ok := allowedSet[scope]; !ok {
			return false
		}
	}
	return true
}

// MockJWTValidator is a mock JWT validator for testing.
type MockJWTValidator struct {
	Claims *JWTAssertionClaims
	Err    error
}

func (m *MockJWTValidator) ValidateJWTAssertion(ctx context.Context, assertion string, clientID string) (*JWTAssertionClaims, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	if m.Claims != nil {
		return m.Claims, nil
	}
	return &JWTAssertionClaims{
		Issuer:    "https://trusted-issuer.example.com",
		Subject:   "service-account-123",
		Audience:  []string{"https://aegion.example.com"},
		ExpiresAt: time.Now().Add(5 * time.Minute),
		IssuedAt:  time.Now(),
		Scopes:    []string{"read", "write"},
	}, nil
}
