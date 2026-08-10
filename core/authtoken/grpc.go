package authtoken

import (
	"context"
	"errors"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const grpcInternalTokenMetadataKey = "x-aegion-internal-token"

// UnaryServerInterceptor authenticates an internal module token before a unary
// RPC handler is allowed to run. The authenticated module ID is attached to the
// request context for authorization at the service boundary.
func UnaryServerInterceptor(generator *Generator) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		authenticated, err := authenticateGRPCContext(ctx, generator)
		if err != nil {
			return nil, err
		}
		return handler(authenticated, req)
	}
}

// StreamServerInterceptor authenticates an internal module token before a
// streaming RPC handler is allowed to run.
func StreamServerInterceptor(generator *Generator) grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		authenticated, err := authenticateGRPCContext(stream.Context(), generator)
		if err != nil {
			return err
		}
		return handler(srv, &authenticatedServerStream{ServerStream: stream, ctx: authenticated})
	}
}

func authenticateGRPCContext(ctx context.Context, generator *Generator) (context.Context, error) {
	if generator == nil {
		return nil, status.Error(codes.Unavailable, "internal authentication unavailable")
	}
	values := metadata.ValueFromIncomingContext(ctx, grpcInternalTokenMetadataKey)
	if len(values) != 1 || values[0] == "" {
		return nil, status.Error(codes.Unauthenticated, "internal authentication token is required")
	}
	moduleID, err := generator.ValidateString(values[0])
	if err != nil {
		if errors.Is(err, ErrExpiredToken) {
			return nil, status.Error(codes.Unauthenticated, "internal authentication token expired")
		}
		return nil, status.Error(codes.Unauthenticated, "invalid internal authentication token")
	}
	return context.WithValue(ctx, ContextKeyModuleID, moduleID), nil
}

type authenticatedServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *authenticatedServerStream) Context() context.Context { return s.ctx }
