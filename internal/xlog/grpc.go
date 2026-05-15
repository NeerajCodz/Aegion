package xlog

import (
	"context"

	"github.com/aegion/aegion/internal/platform/observability"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// UnaryServerInterceptor emits one xlog event per unary RPC.
func (l *Logger) UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		ctx = contextWithMetadata(ctx)
		event := l.Start(ctx, "grpc.request", WithKind(KindRequest)).
			Set("rpc.system", "grpc").
			Set("rpc.method", info.FullMethod)
		ctx = WithEvent(ctx, event)
		resp, err := handler(ctx, req)
		event.Set("rpc.grpc.status_code", status.Code(err).String())
		setRPCOutcome(event, err)
		_ = event.Emit()
		return resp, err
	}
}

// StreamServerInterceptor emits one xlog event per stream RPC.
func (l *Logger) StreamServerInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx := contextWithMetadata(ss.Context())
		event := l.Start(ctx, "grpc.stream", WithKind(KindRequest)).
			Set("rpc.system", "grpc").
			Set("rpc.method", info.FullMethod).
			Set("rpc.grpc.client_stream", info.IsClientStream).
			Set("rpc.grpc.server_stream", info.IsServerStream)
		wrapped := &xlogServerStream{ServerStream: ss, ctx: WithEvent(ctx, event)}
		err := handler(srv, wrapped)
		event.Set("rpc.grpc.status_code", status.Code(err).String())
		setRPCOutcome(event, err)
		_ = event.Emit()
		return err
	}
}

func setRPCOutcome(event *Event, err error) {
	if err == nil {
		event.Success()
		return
	}
	switch status.Code(err) {
	case codes.Canceled:
		event.Cancelled(err)
	case codes.DeadlineExceeded:
		event.Timeout(err)
	case codes.PermissionDenied, codes.Unauthenticated, codes.InvalidArgument:
		event.Rejected(err)
	default:
		event.Error(err)
	}
}

func contextWithMetadata(ctx context.Context) context.Context {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if vals := md.Get("x-request-id"); len(vals) > 0 {
			ctx = contextWithRequestID(ctx, vals[0])
		}
	}
	return ctx
}

func contextWithRequestID(ctx context.Context, requestID string) context.Context {
	return observability.WithRequestIDForLogger(ctx, requestID)
}

type xlogServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *xlogServerStream) Context() context.Context {
	if s.ctx == nil {
		return context.Background()
	}
	return s.ctx
}
