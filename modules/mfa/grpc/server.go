package grpc

import (
	"context"

	mfapb "github.com/aegion/aegion/internal/proto/mfa/v1"
)

// StatusProvider defines MFA status lookup operations.
type StatusProvider interface {
	GetStatus(ctx context.Context, identityID string) (*mfapb.MFAStatusResponse, error)
	GetEnrolledFactors(ctx context.Context, identityID string) ([]*mfapb.Factor, error)
}

// Server exposes MFA gRPC surfaces.
type Server struct {
	mfapb.UnimplementedMFAEngineServer
	provider StatusProvider
}

var _ mfapb.MFAEngineServer = (*Server)(nil)

// NewServer creates a new MFA gRPC server adapter.
func NewServer(provider ...StatusProvider) *Server {
	s := &Server{}
	if len(provider) > 0 {
		s.provider = provider[0]
	}
	return s
}

// GetStatus returns MFA status for an identity.
func (s *Server) GetStatus(ctx context.Context, req *mfapb.MFAStatusRequest) (*mfapb.MFAStatusResponse, error) {
	if s.provider != nil {
		return s.provider.GetStatus(ctx, req.GetIdentityId())
	}

	return &mfapb.MFAStatusResponse{
		MfaEnrolled:     false,
		HighestAal:      "aal1",
		EnrolledMethods: []string{},
	}, nil
}

// GetEnrolledFactors returns the enrolled factors for an identity.
func (s *Server) GetEnrolledFactors(ctx context.Context, req *mfapb.FactorListRequest) (*mfapb.FactorListResponse, error) {
	if s.provider != nil {
		factors, err := s.provider.GetEnrolledFactors(ctx, req.GetIdentityId())
		if err != nil {
			return nil, err
		}
		return &mfapb.FactorListResponse{Factors: factors}, nil
	}

	return &mfapb.FactorListResponse{Factors: []*mfapb.Factor{}}, nil
}
