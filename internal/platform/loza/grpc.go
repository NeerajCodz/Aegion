package loza

import (
	"context"
	"runtime/debug"
	"time"

	lozasdk "github.com/astraive/loza/sdks/go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// UnaryServerInterceptor emits exactly one event for each unary RPC.
func UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		started := time.Now()
		eventCtx := Start(ctx, lozasdk.Default(), lozasdk.Params{
			Event:     "aegion.rpc",
			Kind:      "request",
			StartedAt: started,
			Custom: []lozasdk.Attr{
				lozasdk.String(FieldRPCSystem, "grpc"),
				lozasdk.String(FieldRPCMethod, info.FullMethod),
			},
		})
		defer func() {
			if recovered := recover(); recovered != nil {
				err = status.Errorf(codes.Internal, "panic recovered: %v", recovered)
				_ = lozasdk.Default().Set(eventCtx, lozasdk.String(FieldErrorStack, string(debug.Stack())))
			}
			finishRPC(eventCtx, err, time.Since(started))
		}()
		return handler(ctx, req)
	}
}

// StreamServerInterceptor emits exactly one event for each stream RPC.
func StreamServerInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
		started := time.Now()
		eventCtx := Start(stream.Context(), lozasdk.Default(), lozasdk.Params{
			Event:     "aegion.rpc",
			Kind:      "request",
			StartedAt: started,
			Custom: []lozasdk.Attr{
				lozasdk.String(FieldRPCSystem, "grpc"),
				lozasdk.String(FieldRPCMethod, info.FullMethod),
				lozasdk.Bool("rpc.is_stream", true),
			},
		})
		defer func() {
			if recovered := recover(); recovered != nil {
				err = status.Errorf(codes.Internal, "panic recovered: %v", recovered)
				_ = lozasdk.Default().Set(eventCtx, lozasdk.String(FieldErrorStack, string(debug.Stack())))
			}
			finishRPC(eventCtx, err, time.Since(started))
		}()
		return handler(srv, stream)
	}
}

func finishRPC(ctx context.Context, err error, elapsed time.Duration) {
	logger := lozasdk.Default()
	_ = logger.Set(ctx,
		lozasdk.String(FieldRPCStatusCode, status.Code(err).String()),
		lozasdk.Int64(FieldDurationMS, elapsed.Milliseconds()),
	)
	if err != nil {
		_ = logger.FinishError(ctx, err)
	} else {
		_ = logger.Finish(ctx, rpcOutcome(err))
	}
	_ = logger.Emit(ctx)
}

func rpcOutcome(err error) string {
	switch status.Code(err) {
	case codes.OK:
		return "success"
	case codes.Canceled:
		return "cancelled"
	case codes.DeadlineExceeded:
		return "timeout"
	case codes.InvalidArgument, codes.Unauthenticated, codes.PermissionDenied,
		codes.NotFound, codes.AlreadyExists, codes.FailedPrecondition,
		codes.Aborted, codes.OutOfRange, codes.ResourceExhausted:
		return "rejected"
	default:
		return "error"
	}
}
