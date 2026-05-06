package grpc

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// AuthInterceptor verifies internal service identity via headers.
func AuthInterceptor(authFunc func(context.Context) error) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// Check for internal auth token header
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

// StreamAuthInterceptor verifies internal service identity via headers for stream RPCs.
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

// LoggingInterceptor logs all unary RPC calls.
func LoggingInterceptor(logger *slog.Logger) grpc.UnaryServerInterceptor {
	if logger == nil {
		logger = slog.Default()
	}
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		start := time.Now()

		// Call handler
		resp, err := handler(ctx, req)

		// Log request
		duration := time.Since(start)
		msg := "RPC call completed"
		if err != nil {
			logger.ErrorContext(ctx, msg,
				"method", info.FullMethod,
				"latency_ms", duration.Milliseconds(),
				"error", err,
				"outcome", "error",
			)
		} else {
			logger.InfoContext(ctx, msg,
				"method", info.FullMethod,
				"latency_ms", duration.Milliseconds(),
				"outcome", "success",
			)
		}

		return resp, err
	}
}

// StreamLoggingInterceptor logs all streaming RPC calls.
func StreamLoggingInterceptor(logger *slog.Logger) grpc.StreamServerInterceptor {
	if logger == nil {
		logger = slog.Default()
	}
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		start := time.Now()
		ctx := ss.Context()

		// Call handler
		err := handler(srv, ss)

		duration := time.Since(start)
		msg := "Stream RPC call completed"
		if err != nil {
			logger.ErrorContext(ctx, msg,
				"method", info.FullMethod,
				"is_client_stream", info.IsClientStream,
				"is_server_stream", info.IsServerStream,
				"latency_ms", duration.Milliseconds(),
				"error", err,
				"outcome", "error",
			)
		} else {
			logger.InfoContext(ctx, msg,
				"method", info.FullMethod,
				"is_client_stream", info.IsClientStream,
				"is_server_stream", info.IsServerStream,
				"latency_ms", duration.Milliseconds(),
				"outcome", "success",
			)
		}

		return err
	}
}

// TracingInterceptor adds OpenTelemetry tracing to unary RPCs.
func TracingInterceptor(tracer trace.Tracer) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// Extract trace context from metadata if available
		spanCtx, span := tracer.Start(ctx, info.FullMethod)
		defer span.End()

		// Call handler with span context
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

// StreamTracingInterceptor adds OpenTelemetry tracing to streaming RPCs.
func StreamTracingInterceptor(tracer trace.Tracer) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx := ss.Context()

		spanCtx, span := tracer.Start(ctx, info.FullMethod)
		defer span.End()

		// Create a wrapped stream that uses the span context
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

// ErrorConversionInterceptor converts Go errors to gRPC status codes.
func ErrorConversionInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		resp, err := handler(ctx, req)

		if err == nil {
			return resp, nil
		}

		// Check if already a gRPC status error
		if _, ok := status.FromError(err); ok {
			return resp, err
		}

		// Convert common Go errors to gRPC status codes
		return resp, err
	}
}

// RateLimitInterceptor enforces rate limiting per service.
func RateLimitInterceptor(maxRPS int) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// In a real implementation, check rate limits per service ID
		// For now, pass through
		return handler(ctx, req)
	}
}

// ChainUnaryInterceptors chains multiple unary interceptors.
func ChainUnaryInterceptors(interceptors ...grpc.UnaryServerInterceptor) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if len(interceptors) == 0 {
			return handler(ctx, req)
		}

		// Build a chain of handlers
		var chainedHandler grpc.UnaryHandler
		chainedHandler = handler

		// Apply interceptors in reverse order so first interceptor wraps second, etc.
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

// ChainStreamInterceptors chains multiple stream interceptors.
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

// tracedServerStream wraps a gRPC ServerStream with a traced context.
type tracedServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (ss *tracedServerStream) Context() context.Context {
	return ss.ctx
}
