package grpc

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/aegion/aegion/modules/oauth2/store"
)

// TokenStore defines storage operations required by the gRPC adapter layer.
type TokenStore interface {
	GetAccessToken(ctx context.Context, jti string) (*store.AccessToken, error)
	RevokeAccessToken(ctx context.Context, jti string) error
	InvalidateRefreshTokenFamily(ctx context.Context, familyID string) (int64, error)
}

// Server provides OAuth2 token operations used by downstream services.
// It is designed to be wrapped by generated gRPC transport handlers.
type Server struct {
	store TokenStore
}

// NewServer creates a new OAuth2 gRPC server adapter.
func NewServer(store TokenStore) *Server {
	return &Server{store: store}
}

// IntrospectTokenRequest contains a token identifier (JTI).
type IntrospectTokenRequest struct {
	Token string `json:"token"`
}

// IntrospectTokenResponse contains active state and token metadata.
type IntrospectTokenResponse struct {
	Active     bool      `json:"active"`
	ClientID   string    `json:"client_id,omitempty"`
	IdentityID string    `json:"identity_id,omitempty"`
	Scopes     []string  `json:"scopes,omitempty"`
	Audience   []string  `json:"audience,omitempty"`
	ExpiresAt  time.Time `json:"expires_at,omitempty"`
}

// RevokeTokenRequest revokes a token by JTI.
type RevokeTokenRequest struct {
	Token string `json:"token"`
}

// RevokeTokenResponse indicates whether the token existed and was revoked.
type RevokeTokenResponse struct {
	Revoked bool `json:"revoked"`
}

// InvalidateFamilyRequest invalidates all refresh tokens in a family.
type InvalidateFamilyRequest struct {
	FamilyID string `json:"family_id"`
}

// InvalidateFamilyResponse includes the number of tokens invalidated.
type InvalidateFamilyResponse struct {
	Invalidated int64 `json:"invalidated"`
}

// Introspect checks whether a token is active and returns metadata for active tokens.
func (s *Server) Introspect(ctx context.Context, req *IntrospectTokenRequest) (*IntrospectTokenResponse, error) {
	if req == nil || strings.TrimSpace(req.Token) == "" {
		return nil, errors.New("token is required")
	}

	token, err := s.store.GetAccessToken(ctx, req.Token)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return &IntrospectTokenResponse{Active: false}, nil
		}
		return nil, err
	}

	if token.Revoked || time.Now().UTC().After(token.ExpiresAt) {
		return &IntrospectTokenResponse{Active: false}, nil
	}

	return &IntrospectTokenResponse{
		Active:     true,
		ClientID:   token.ClientID,
		IdentityID: token.IdentityID,
		Scopes:     token.Scopes,
		Audience:   token.Audience,
		ExpiresAt:  token.ExpiresAt,
	}, nil
}

// Revoke revokes a token. Per RFC behavior, unknown tokens are not treated as errors.
func (s *Server) Revoke(ctx context.Context, req *RevokeTokenRequest) (*RevokeTokenResponse, error) {
	if req == nil || strings.TrimSpace(req.Token) == "" {
		return nil, errors.New("token is required")
	}

	err := s.store.RevokeAccessToken(ctx, req.Token)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return &RevokeTokenResponse{Revoked: false}, nil
		}
		return nil, err
	}

	return &RevokeTokenResponse{Revoked: true}, nil
}

// InvalidateFamily invalidates all active refresh tokens in a token family.
func (s *Server) InvalidateFamily(ctx context.Context, req *InvalidateFamilyRequest) (*InvalidateFamilyResponse, error) {
	if req == nil || strings.TrimSpace(req.FamilyID) == "" {
		return nil, errors.New("family_id is required")
	}

	count, err := s.store.InvalidateRefreshTokenFamily(ctx, req.FamilyID)
	if err != nil {
		return nil, err
	}

	return &InvalidateFamilyResponse{Invalidated: count}, nil
}
