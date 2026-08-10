package authtoken

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestUnaryServerInterceptorAuthenticatesModule(t *testing.T) {
	generator, err := NewGenerator(GeneratorConfig{Secret: []byte("01234567890123456789012345678901")})
	require.NoError(t, err)
	token, err := generator.Generate("analytics")
	require.NoError(t, err)

	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(grpcInternalTokenMetadataKey, token))
	response, err := UnaryServerInterceptor(generator)(ctx, "request", &grpc.UnaryServerInfo{FullMethod: "/core.Registry/Register"}, func(ctx context.Context, request any) (any, error) {
		require.Equal(t, "analytics", ModuleIDFromContext(ctx))
		require.Equal(t, "request", request)
		return "response", nil
	})
	require.NoError(t, err)
	require.Equal(t, "response", response)
}

func TestUnaryServerInterceptorRejectsMissingOrInvalidToken(t *testing.T) {
	generator, err := NewGenerator(GeneratorConfig{Secret: []byte("01234567890123456789012345678901")})
	require.NoError(t, err)
	interceptor := UnaryServerInterceptor(generator)

	_, err = interceptor(context.Background(), nil, &grpc.UnaryServerInfo{}, func(context.Context, any) (any, error) {
		t.Fatal("handler must not be called")
		return nil, nil
	})
	require.Equal(t, codes.Unauthenticated, status.Code(err))

	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(grpcInternalTokenMetadataKey, "not-a-token"))
	_, err = interceptor(ctx, nil, &grpc.UnaryServerInfo{}, func(context.Context, any) (any, error) {
		t.Fatal("handler must not be called")
		return nil, nil
	})
	require.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestUnaryServerInterceptorFailsClosedWhenGeneratorUnavailable(t *testing.T) {
	_, err := UnaryServerInterceptor(nil)(context.Background(), nil, &grpc.UnaryServerInfo{}, func(context.Context, any) (any, error) {
		t.Fatal("handler must not be called")
		return nil, nil
	})
	require.Equal(t, codes.Unavailable, status.Code(err))
}
