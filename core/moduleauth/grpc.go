package moduleauth

import (
	"context"
	"errors"
	"strings"

	"github.com/aegion/aegion/core/authtoken"
	corepb "github.com/aegion/aegion/internal/proto/core"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

const internalTokenMetadataKey = "x-aegion-internal-token"

// Requirement maps a gRPC method to the audience and permission required for
// the operation. Methods omitted from the map are rejected unless explicitly
// configured as bootstrap methods.
type Requirement struct {
	Audience   string
	Permission string
	Bootstrap  bool
}

// UnaryServerInterceptor validates module-scoped credentials and attaches the
// authenticated module ID for service-level ownership checks.
func (m *Manager) UnaryServerInterceptor(requirements map[string]Requirement) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		requirement, ok := requirements[info.FullMethod]
		if !ok {
			return nil, status.Error(codes.PermissionDenied, "gRPC method is not authorized for modules")
		}
		if requirement.Bootstrap {
			return handler(ctx, req)
		}
		values := metadata.ValueFromIncomingContext(ctx, internalTokenMetadataKey)
		if len(values) != 1 || values[0] == "" {
			return nil, status.Error(codes.Unauthenticated, "module token is required")
		}
		claims, err := m.Validate(ctx, values[0], requirement.Audience, requirement.Permission)
		if err != nil {
			return nil, grpcAuthError(err)
		}
		mtlsIdentity, err := moduleTLSIdentity(ctx)
		if err != nil {
			return nil, err
		}
		if mtlsIdentity != claims.ModuleID {
			return nil, status.Error(codes.PermissionDenied, "module token does not match the mTLS client identity")
		}
		return handler(authtoken.ContextWithModuleID(ctx, claims.ModuleID), req)
	}
}

func grpcAuthError(err error) error {
	switch {
	case errors.Is(err, ErrTokenDenied), errors.Is(err, ErrCredentialRevoked):
		return status.Error(codes.PermissionDenied, "module token is not authorized")
	case errors.Is(err, ErrCredentialInvalid), errors.Is(err, ErrTokenInvalid), errors.Is(err, ErrTokenExpired):
		return status.Error(codes.Unauthenticated, "module token is invalid")
	default:
		return status.Error(codes.Unavailable, "module authentication is unavailable")
	}
}

// GRPCService exposes bootstrap exchange over the mTLS-only control plane.
// Bootstrap secrets are never logged, persisted raw, or returned.
type GRPCService struct {
	corepb.UnimplementedInternalTokenServiceServer
	manager *Manager
}

// NewGRPCService creates the durable module credential exchange service.
func NewGRPCService(manager *Manager) *GRPCService {
	return &GRPCService{manager: manager}
}
func (s *GRPCService) GetCurrent(ctx context.Context, request *corepb.GetCurrentRequest) (*corepb.GetCurrentResponse, error) {
	if s.manager == nil || request == nil || request.GetModule() == "" || request.GetBootstrapSecret() == "" {
		return nil, status.Error(codes.Unauthenticated, "module bootstrap credential is required")
	}
	mtlsIdentity, err := moduleTLSIdentity(ctx)
	if err != nil {
		return nil, err
	}
	if mtlsIdentity != request.GetModule() {
		return nil, status.Error(codes.PermissionDenied, "module bootstrap credential does not match the mTLS client identity")
	}
	token, claims, err := s.manager.Exchange(ctx, request.GetModule(), request.GetBootstrapSecret(), "core.registry", nil)
	if err != nil {
		return nil, grpcAuthError(err)
	}
	return &corepb.GetCurrentResponse{
		Token: token,
		Metadata: &corepb.TokenMetadata{
			IssuedAt:  claims.IssuedAt,
			ExpiresAt: claims.ExpiresAt,
		},
	}, nil
}

func (s *GRPCService) Validate(ctx context.Context, request *corepb.ValidateRequest) (*corepb.ValidateResponse, error) {
	if s.manager == nil || request == nil {
		return &corepb.ValidateResponse{Valid: false, ErrorCode: "unavailable"}, nil
	}
	claims, err := s.manager.Validate(ctx, request.GetToken(), "core.registry", "registry:register")
	if err != nil || claims.ModuleID != request.GetModule() {
		return &corepb.ValidateResponse{Valid: false, ErrorCode: "invalid_token"}, nil
	}
	return &corepb.ValidateResponse{
		Valid: true,
		Metadata: &corepb.TokenMetadata{
			IssuedAt:  claims.IssuedAt,
			ExpiresAt: claims.ExpiresAt,
		},
	}, nil
}

func moduleTLSIdentity(ctx context.Context) (string, error) {
	peerInfo, ok := peer.FromContext(ctx)
	if !ok {
		return "", status.Error(codes.Unauthenticated, "mTLS client identity is required")
	}
	tlsInfo, ok := peerInfo.AuthInfo.(credentials.TLSInfo)
	if !ok || len(tlsInfo.State.PeerCertificates) == 0 {
		return "", status.Error(codes.Unauthenticated, "mTLS client identity is required")
	}
	identity := strings.TrimSpace(tlsInfo.State.PeerCertificates[0].Subject.CommonName)
	if identity == "" {
		return "", status.Error(codes.Unauthenticated, "mTLS client certificate common name is required")
	}
	return identity, nil
}
