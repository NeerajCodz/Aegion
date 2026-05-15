package grpc

import (
	"context"
	"errors"
	"testing"

	"github.com/aegion/aegion/internal/xlog"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type mockGRPCServerStream struct {
	ctx context.Context
}

func (m *mockGRPCServerStream) SetHeader(md metadata.MD) error  { return nil }
func (m *mockGRPCServerStream) SendHeader(md metadata.MD) error { return nil }
func (m *mockGRPCServerStream) SetTrailer(md metadata.MD)       {}
func (m *mockGRPCServerStream) Context() context.Context        { return m.ctx }
func (m *mockGRPCServerStream) SendMsg(interface{}) error       { return nil }
func (m *mockGRPCServerStream) RecvMsg(interface{}) error       { return nil }

func TestAuthInterceptor_RequiresVerifier(t *testing.T) {
	interceptor := AuthInterceptor(nil)

	ctxWithMD := metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-aegion-internal-token", "token"))
	_, err := interceptor(ctxWithMD, struct{}{}, &grpc.UnaryServerInfo{FullMethod: "/m"}, func(context.Context, interface{}) (interface{}, error) {
		return "ok", nil
	})
	require.Error(t, err)
	require.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestAuthInterceptor_DelegatesToAuthFunc(t *testing.T) {
	authErr := errors.New("nope")
	interceptor := AuthInterceptor(func(context.Context) error { return authErr })

	ctxWithMD := metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-aegion-internal-token", "token"))
	_, err := interceptor(ctxWithMD, struct{}{}, &grpc.UnaryServerInfo{FullMethod: "/m"}, func(context.Context, interface{}) (interface{}, error) {
		return "ok", nil
	})
	require.Error(t, err)
	require.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestStreamAuthInterceptor_RequiresVerifier(t *testing.T) {
	interceptor := StreamAuthInterceptor(nil)

	ctxWithMD := metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-aegion-internal-token", "token"))
	err := interceptor(struct{}{}, &mockGRPCServerStream{ctx: ctxWithMD}, &grpc.StreamServerInfo{FullMethod: "/m", IsServerStream: true}, func(interface{}, grpc.ServerStream) error {
		return nil
	})
	require.Error(t, err)
	require.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestLoggingInterceptor_EmitsLine(t *testing.T) {
	var events []xlog.Record
	sink := xlog.SinkFunc(func(ctx context.Context, r xlog.Record) error {
		events = append(events, r)
		return nil
	})
	logger := xlog.New(xlog.Config{Sinks: []xlog.Sink{sink}})

	interceptor := logger.UnaryServerInterceptor()

	resp, err := interceptor(context.Background(), struct{}{}, &grpc.UnaryServerInfo{FullMethod: "/m"}, func(context.Context, interface{}) (interface{}, error) {
		return "ok", nil
	})
	require.NoError(t, err)
	require.Equal(t, "ok", resp)

	require.Len(t, events, 1)
	logData := events[0].Fields
	require.Equal(t, "grpc.request", logData["event.name"])
	require.Equal(t, "/m", logData["rpc.method"])
	require.Equal(t, "OK", logData["rpc.grpc.status_code"])
	require.Equal(t, "success", logData["event.outcome"])
}

func TestStreamLoggingInterceptor_EmitsLine(t *testing.T) {
	var events []xlog.Record
	sink := xlog.SinkFunc(func(ctx context.Context, r xlog.Record) error {
		events = append(events, r)
		return nil
	})
	logger := xlog.New(xlog.Config{Sinks: []xlog.Sink{sink}})

	interceptor := logger.StreamServerInterceptor()

	stream := &mockGRPCServerStream{ctx: context.Background()}
	err := interceptor(struct{}{}, stream, &grpc.StreamServerInfo{FullMethod: "/m", IsServerStream: true}, func(interface{}, grpc.ServerStream) error {
		return nil
	})
	require.NoError(t, err)

	require.Len(t, events, 1)
	logData := events[0].Fields
	require.Equal(t, "grpc.stream", logData["event.name"])
	require.Equal(t, "/m", logData["rpc.method"])
	require.Equal(t, "OK", logData["rpc.grpc.status_code"])
	require.Equal(t, "success", logData["event.outcome"])
}

func TestTracingInterceptor_RecordsErrorAttributes(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	tracer := tp.Tracer("test")

	interceptor := TracingInterceptor(tracer)

	grpcErr := status.Error(codes.Internal, "boom")
	_, err := interceptor(context.Background(), struct{}{}, &grpc.UnaryServerInfo{FullMethod: "/m"}, func(ctx context.Context, req interface{}) (interface{}, error) {
		span := oteltrace.SpanFromContext(ctx)
		require.True(t, span.SpanContext().IsValid())
		return nil, grpcErr
	})
	require.Error(t, err)

	ended := sr.Ended()
	require.Len(t, ended, 1)
	require.Equal(t, "/m", ended[0].Name())

	attrs := ended[0].Attributes()
	require.Contains(t, attrs, attribute.Bool("error", true))
	require.Contains(t, attrs, attribute.String("code", codes.Internal.String()))
}

func TestStreamTracingInterceptor_WrapsContext(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	tracer := tp.Tracer("test")

	interceptor := StreamTracingInterceptor(tracer)

	stream := &mockGRPCServerStream{ctx: context.Background()}
	err := interceptor(struct{}{}, stream, &grpc.StreamServerInfo{FullMethod: "/m", IsServerStream: true}, func(_ interface{}, ss grpc.ServerStream) error {
		span := oteltrace.SpanFromContext(ss.Context())
		require.True(t, span.SpanContext().IsValid())
		return status.Error(codes.Internal, "boom")
	})
	require.Error(t, err)

	ended := sr.Ended()
	require.Len(t, ended, 1)
	require.Equal(t, "/m", ended[0].Name())
}
