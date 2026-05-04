// Package service provides business logic for OAuth2 client management.
package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"
	"github.com/aegion/aegion/modules/oauth2/store"
)

var (
	ErrInvalidClient         = errors.New("invalid client")
	ErrInvalidSecret         = errors.New("invalid client secret")
	ErrInvalidRedirectURI    = errors.New("invalid redirect URI")
	ErrInvalidGrantType      = errors.New("invalid grant type")
	ErrInvalidScope          = errors.New("invalid scope")
	ErrUnsupportedAuthMethod = errors.New("unsupported authentication method")
)

// ClientStore interface for client operations.
type ClientStore interface {
	CreateClient(ctx context.Context, client *store.Client) error
	GetClient(ctx context.Context, id string) (*store.Client, error)
	UpdateClient(ctx context.Context, client *store.Client) error
	UpdateClientSecret(ctx context.Context, clientID string, secretHash string) error
	DeleteClient(ctx context.Context, id string) error
	ListClients(ctx context.Context, ownerID *string, limit, offset int) ([]*store.Client, error)
}

// ClientService handles OAuth2 client management.
type ClientService struct {
	store ClientStore
}

// NewClientService creates a new client service.
func NewClientService(store ClientStore) *ClientService {
	return &ClientService{store: store}
}

// CreateClientRequest contains parameters for creating a client.
type CreateClientRequest struct {
	Name                    string            `json:"name"`
	Description             *string           `json:"description,omitempty"`
	RedirectURIs            []string          `json:"redirect_uris"`
	GrantTypes              []string          `json:"grant_types,omitempty"`
	ResponseTypes           []string          `json:"response_types,omitempty"`
	Scopes                  []string          `json:"scopes,omitempty"`
	TokenEndpointAuthMethod string            `json:"token_endpoint_auth_method,omitempty"`
	RequirePKCE             *bool             `json:"require_pkce,omitempty"`
	RequireConsent          *bool             `json:"require_consent,omitempty"`
	AllowOfflineAccess      *bool             `json:"allow_offline_access,omitempty"`
	Metadata                map[string]string `json:"metadata,omitempty"`
	OwnerID                 *string           `json:"owner_id,omitempty"`
}

// CreateClientResponse contains the created client and secret.
type CreateClientResponse struct {
	Client       *store.Client `json:"client"`
	ClientSecret *string       `json:"client_secret,omitempty"` // only returned on creation
}

// CreateClient creates a new OAuth2 client.
func (s *ClientService) CreateClient(ctx context.Context, req *CreateClientRequest) (*CreateClientResponse, error) {
	// Validate required fields
	if req.Name == "" {
		return nil, errors.New("client name is required")
	}
	if len(req.RedirectURIs) == 0 {
		return nil, errors.New("at least one redirect URI is required")
	}

	// Validate redirect URIs
	for _, uri := range req.RedirectURIs {
		if err := validateRedirectURI(uri); err != nil {
			return nil, fmt.Errorf("invalid redirect URI %q: %w", uri, err)
		}
	}

	// Set defaults
	grantTypes := req.GrantTypes
	if len(grantTypes) == 0 {
		grantTypes = []string{"authorization_code"}
	}

	responseTypes := req.ResponseTypes
	if len(responseTypes) == 0 {
		responseTypes = []string{"code"}
	}

	scopes := req.Scopes
	if len(scopes) == 0 {
		scopes = []string{"openid"}
	}

	authMethod := req.TokenEndpointAuthMethod
	if authMethod == "" {
		authMethod = "client_secret_basic"
	}

	// Validate auth method
	if !isValidAuthMethod(authMethod) {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedAuthMethod, authMethod)
	}

	requirePKCE := true
	if req.RequirePKCE != nil {
		requirePKCE = *req.RequirePKCE
	}

	requireConsent := true
	if req.RequireConsent != nil {
		requireConsent = *req.RequireConsent
	}

	allowOfflineAccess := true
	if req.AllowOfflineAccess != nil {
		allowOfflineAccess = *req.AllowOfflineAccess
	}

	// Generate client ID
	clientID, err := store.GenerateClientID()
	if err != nil {
		return nil, fmt.Errorf("failed to generate client ID: %w", err)
	}

	// Generate client secret if needed
	var secretHash *string
	var plainSecret *string

	if authMethod != "none" {
		secret, hash, err := generateClientSecret()
		if err != nil {
			return nil, fmt.Errorf("failed to generate client secret: %w", err)
		}
		secretHash = &hash
		plainSecret = &secret
	}

	client := &store.Client{
		ID:                       clientID,
		SecretHash:               secretHash,
		Name:                     req.Name,
		Description:              req.Description,
		RedirectURIs:             req.RedirectURIs,
		GrantTypes:               grantTypes,
		ResponseTypes:            responseTypes,
		Scopes:                   scopes,
		TokenEndpointAuthMethod:  authMethod,
		SubjectType:              "public",
		IDTokenSignedResponseAlg: "RS256",
		AccessTokenStrategy:      "jwt",
		AccessTokenTTL:           900,     // 15 minutes
		RefreshTokenTTL:          2592000, // 30 days
		IDTokenTTL:               3600,    // 1 hour
		AuthCodeTTL:              600,     // 10 minutes
		RequirePKCE:              requirePKCE,
		RequireConsent:           requireConsent,
		AllowOfflineAccess:       allowOfflineAccess,
		Metadata:                 req.Metadata,
		OwnerID:                  req.OwnerID,
	}

	if err := s.store.CreateClient(ctx, client); err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	return &CreateClientResponse{
		Client:       client,
		ClientSecret: plainSecret,
	}, nil
}

// GetClient retrieves a client by ID.
func (s *ClientService) GetClient(ctx context.Context, id string) (*store.Client, error) {
	client, err := s.store.GetClient(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrInvalidClient
		}
		return nil, err
	}
	return client, nil
}

// UpdateClient updates a client.
func (s *ClientService) UpdateClient(ctx context.Context, id string, req *CreateClientRequest) (*store.Client, error) {
	client, err := s.store.GetClient(ctx, id)
	if err != nil {
		return nil, err
	}

	// Update fields
	if req.Name != "" {
		client.Name = req.Name
	}
	if req.Description != nil {
		client.Description = req.Description
	}
	if len(req.RedirectURIs) > 0 {
		for _, uri := range req.RedirectURIs {
			if err := validateRedirectURI(uri); err != nil {
				return nil, err
			}
		}
		client.RedirectURIs = req.RedirectURIs
	}
	if len(req.GrantTypes) > 0 {
		client.GrantTypes = req.GrantTypes
	}
	if len(req.Scopes) > 0 {
		client.Scopes = req.Scopes
	}
	if req.RequirePKCE != nil {
		client.RequirePKCE = *req.RequirePKCE
	}
	if req.RequireConsent != nil {
		client.RequireConsent = *req.RequireConsent
	}
	if req.AllowOfflineAccess != nil {
		client.AllowOfflineAccess = *req.AllowOfflineAccess
	}
	if req.Metadata != nil {
		client.Metadata = req.Metadata
	}

	if err := s.store.UpdateClient(ctx, client); err != nil {
		return nil, err
	}

	return client, nil
}

// RotateClientSecret generates a new client secret.
func (s *ClientService) RotateClientSecret(ctx context.Context, id string) (string, error) {
	client, err := s.store.GetClient(ctx, id)
	if err != nil {
		return "", err
	}

	if client.IsPublic() {
		return "", errors.New("cannot rotate secret for public client")
	}

	secret, hash, err := generateClientSecret()
	if err != nil {
		return "", err
	}

	if err := s.store.UpdateClientSecret(ctx, id, hash); err != nil {
		return "", err
	}

	return secret, nil
}

// DeleteClient deletes a client.
func (s *ClientService) DeleteClient(ctx context.Context, id string) error {
	return s.store.DeleteClient(ctx, id)
}

// ListClients lists clients, optionally filtered by owner.
func (s *ClientService) ListClients(ctx context.Context, ownerID *string, limit, offset int) ([]*store.Client, error) {
	return s.store.ListClients(ctx, ownerID, limit, offset)
}

// AuthenticateClient verifies client credentials.
func (s *ClientService) AuthenticateClient(ctx context.Context, clientID, clientSecret, authMethod string) (*store.Client, error) {
	client, err := s.store.GetClient(ctx, clientID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrInvalidClient
		}
		return nil, err
	}

	// Public clients (auth method "none") don't require secret verification
	if client.TokenEndpointAuthMethod == "none" {
		if authMethod != "none" && authMethod != "" {
			return nil, ErrInvalidClient
		}
		return client, nil
	}

	// Verify auth method matches
	if authMethod != "" && client.TokenEndpointAuthMethod != authMethod {
		return nil, errors.New("client authentication method mismatch")
	}

	// Verify secret for confidential clients
	if client.SecretHash == nil {
		return nil, errors.New("client secret not set")
	}

	matches, verifyErr := platformcrypto.VerifyPassword(clientSecret, *client.SecretHash)
	if verifyErr != nil || !matches {
		return nil, ErrInvalidSecret
	}

	return client, nil
}

// generateClientSecret generates a cryptographically secure client secret.
func generateClientSecret() (string, string, error) {
	b, err := platformcrypto.RandomBytes(32)
	if err != nil {
		return "", "", err
	}
	secret := base64.RawURLEncoding.EncodeToString(b)

	hash, err := platformcrypto.HashPassword(secret)
	if err != nil {
		return "", "", err
	}

	return secret, hash, nil
}

// validateRedirectURI validates a redirect URI.
func validateRedirectURI(uri string) error {
	if uri == "" {
		return errors.New("redirect URI cannot be empty")
	}

	// Must be absolute URI
	if !strings.HasPrefix(uri, "http://") && !strings.HasPrefix(uri, "https://") && !strings.Contains(uri, "://") {
		return errors.New("redirect URI must be absolute")
	}

	// No wildcards allowed
	if strings.Contains(uri, "*") {
		return errors.New("wildcards not allowed in redirect URIs")
	}

	// No fragments
	if strings.Contains(uri, "#") {
		return errors.New("fragments not allowed in redirect URIs")
	}

	return nil
}

// isValidAuthMethod checks if an authentication method is supported.
func isValidAuthMethod(method string) bool {
	valid := []string{
		"none",
		"client_secret_basic",
		"client_secret_post",
		"client_secret_jwt",
		"private_key_jwt",
	}
	for _, v := range valid {
		if v == method {
			return true
		}
	}
	return false
}

// VerifyClientSecret verifies a client secret using constant-time comparison.
func VerifyClientSecret(storedHash, providedSecret string) bool {
	matches, err := platformcrypto.VerifyPassword(providedSecret, storedHash)
	return err == nil && matches
}
