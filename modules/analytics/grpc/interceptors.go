package grpc

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func AuthInterceptor(authFunc func(context.Context) error) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "missing internal auth token")
		}
		tokens := md.Get("x-aegion-internal-token")
		if len(tokens) == 0 {
			return nil, status.Error(codes.Unauthenticated, "missing internal auth token")
		}

		if authFunc == nil {
			return nil, status.Error(codes.Unauthenticated, "auth verifier not configured")
		}

		if err := authFunc(ctx); err != nil {
			return nil, status.Error(codes.PermissionDenied, fmt.Sprintf("auth failed: %v", err))
		}

		return handler(ctx, req)
	}
}

func StreamAuthInterceptor(authFunc func(context.Context) error) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx := ss.Context()

		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return status.Error(codes.Unauthenticated, "missing internal auth token")
		}
		tokens := md.Get("x-aegion-internal-token")
		if len(tokens) == 0 {
			return status.Error(codes.Unauthenticated, "missing internal auth token")
		}
		if authFunc == nil {
			return status.Error(codes.Unauthenticated, "auth verifier not configured")
		}

		if err := authFunc(ctx); err != nil {
			return status.Error(codes.PermissionDenied, fmt.Sprintf("auth failed: %v", err))
		}

		return handler(srv, ss)
	}
}

func TracingInterceptor(tracer trace.Tracer) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		spanCtx, span := tracer.Start(ctx, info.FullMethod)
		defer span.End()

		resp, err := handler(spanCtx, req)

		if err != nil {
			span.RecordError(err)
			span.SetAttributes(
				attribute.Bool("error", true),
				attribute.String("code", status.Code(err).String()),
			)
		}

		return resp, err
	}
}

func StreamTracingInterceptor(tracer trace.Tracer) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx := ss.Context()

		spanCtx, span := tracer.Start(ctx, info.FullMethod)
		defer span.End()

		wrappedStream := &tracedServerStream{
			ServerStream: ss,
			ctx:          spanCtx,
		}

		err := handler(srv, wrappedStream)

		if err != nil {
			span.RecordError(err)
			span.SetAttributes(
				attribute.Bool("error", true),
				attribute.String("code", status.Code(err).String()),
			)
		}

		return err
	}
}

func ErrorConversionInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		resp, err := handler(ctx, req)

		if err == nil {
			return resp, nil
		}

		if _, ok := status.FromError(err); ok {
			return resp, err
		}

		return resp, err
	}
}

func RateLimitInterceptor(maxRPS int) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		return handler(ctx, req)
	}
}

func ChainUnaryInterceptors(interceptors ...grpc.UnaryServerInterceptor) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if len(interceptors) == 0 {
			return handler(ctx, req)
		}

		var chainedHandler grpc.UnaryHandler
		chainedHandler = handler

		for i := len(interceptors) - 1; i >= 0; i-- {
			interceptor := interceptors[i]
			currentHandler := chainedHandler
			chainedHandler = func(ctx context.Context, req interface{}) (interface{}, error) {
				return interceptor(ctx, req, info, currentHandler)
			}
		}

		return chainedHandler(ctx, req)
	}
}

func ChainStreamInterceptors(interceptors ...grpc.StreamServerInterceptor) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if len(interceptors) == 0 {
			return handler(srv, ss)
		}

		chainedHandler := handler
		for i := len(interceptors) - 1; i >= 0; i-- {
			interceptor := interceptors[i]
			currentHandler := chainedHandler
			chainedHandler = func(currentSrv interface{}, currentStream grpc.ServerStream) error {
				return interceptor(currentSrv, currentStream, info, currentHandler)
			}
		}

		return chainedHandler(srv, ss)
	}
}

type tracedServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (ss *tracedServerStream) Context() context.Context {
	return ss.ctx
}
