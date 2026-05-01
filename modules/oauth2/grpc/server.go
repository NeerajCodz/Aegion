package grpc

import (
	"context"
	"errors"
	"strings"
	"time"

	oauth2pb "github.com/aegion/aegion/internal/proto/oauth2/v1"
	"github.com/aegion/aegion/modules/oauth2/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
	oauth2pb.UnimplementedTokenStoreServer
	store TokenStore
}

var _ oauth2pb.TokenStoreServer = (*Server)(nil)

// NewServer creates a new OAuth2 gRPC server adapter.
func NewServer(store TokenStore) *Server {
	return &Server{store: store}
}

// Introspect checks whether a token is active and returns metadata for active tokens.
func (s *Server) Introspect(ctx context.Context, req *oauth2pb.IntrospectRequest) (*oauth2pb.IntrospectResponse, error) {
	if req == nil || strings.TrimSpace(req.GetToken()) == "" {
		return nil, status.Error(codes.InvalidArgument, "token is required")
	}

	token, err := s.store.GetAccessToken(ctx, req.GetToken())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return &oauth2pb.IntrospectResponse{Active: false}, nil
		}
		return nil, status.Error(codes.Internal, "failed to introspect token")
	}

	if token.Revoked || time.Now().UTC().After(token.ExpiresAt) {
		return &oauth2pb.IntrospectResponse{Active: false}, nil
	}

	return &oauth2pb.IntrospectResponse{
		Active:     true,
		ClientId:   token.ClientID,
		IdentityId: token.IdentityID,
		Scopes:     token.Scopes,
		Audience:   token.Audience,
		ExpiresAt:  token.ExpiresAt.Unix(),
	}, nil
}

// Revoke revokes a token. Per RFC behavior, unknown tokens are not treated as errors.
func (s *Server) Revoke(ctx context.Context, req *oauth2pb.RevokeRequest) (*oauth2pb.RevokeResponse, error) {
	if req == nil || strings.TrimSpace(req.GetToken()) == "" {
		return nil, status.Error(codes.InvalidArgument, "token is required")
	}

	err := s.store.RevokeAccessToken(ctx, req.GetToken())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return &oauth2pb.RevokeResponse{Revoked: false}, nil
		}
		return nil, status.Error(codes.Internal, "failed to revoke token")
	}

	return &oauth2pb.RevokeResponse{Revoked: true}, nil
}

// InvalidateFamily invalidates all active refresh tokens in a token family.
func (s *Server) InvalidateFamily(ctx context.Context, req *oauth2pb.InvalidateFamilyRequest) (*oauth2pb.InvalidateFamilyResponse, error) {
	if req == nil || strings.TrimSpace(req.GetFamilyId()) == "" {
		return nil, status.Error(codes.InvalidArgument, "family_id is required")
	}

	count, err := s.store.InvalidateRefreshTokenFamily(ctx, req.GetFamilyId())
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to invalidate token family")
	}

	return &oauth2pb.InvalidateFamilyResponse{Invalidated: count}, nil
}
