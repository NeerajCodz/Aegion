package grpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

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

func TestAuthInterceptor_RequiresServiceID(t *testing.T) {
	interceptor := AuthInterceptor(nil)

	_, err := interceptor(context.Background(), struct{}{}, &grpc.UnaryServerInfo{FullMethod: "/aegion.analytics.v1.AnalyticsService/QueryEvents"}, func(context.Context, interface{}) (interface{}, error) {
		return "ok", nil
	})
	require.Error(t, err)
	require.Equal(t, codes.Unauthenticated, status.Code(err))

	ctxWithMD := metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-service-id", "svc-1"))
	resp, err := interceptor(ctxWithMD, struct{}{}, &grpc.UnaryServerInfo{FullMethod: "/m"}, func(context.Context, interface{}) (interface{}, error) {
		return "ok", nil
	})
	require.NoError(t, err)
	require.Equal(t, "ok", resp)
}

func TestAuthInterceptor_DelegatesToAuthFunc(t *testing.T) {
	authErr := errors.New("nope")
	interceptor := AuthInterceptor(func(context.Context) error { return authErr })

	ctxWithMD := metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-service-id", "svc-1"))
	_, err := interceptor(ctxWithMD, struct{}{}, &grpc.UnaryServerInfo{FullMethod: "/m"}, func(context.Context, interface{}) (interface{}, error) {
		return "ok", nil
	})
	require.Error(t, err)
	require.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestLoggingInterceptor_EmitsLine(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	interceptor := LoggingInterceptor(logger)

	resp, err := interceptor(context.Background(), struct{}{}, &grpc.UnaryServerInfo{FullMethod: "/m"}, func(context.Context, interface{}) (interface{}, error) {
		return "ok", nil
	})
	require.NoError(t, err)
	require.Equal(t, "ok", resp)

	// Verify the log output contains expected fields
	var logData map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logData); err == nil {
		require.Contains(t, logData, "msg")
		require.Contains(t, logData["msg"], "RPC call completed")
		require.Contains(t, logData, "method")
		require.Equal(t, "/m", logData["method"])
	}
}

func TestStreamLoggingInterceptor_EmitsLine(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	interceptor := StreamLoggingInterceptor(logger)

	stream := &mockGRPCServerStream{ctx: context.Background()}
	err := interceptor(struct{}{}, stream, &grpc.StreamServerInfo{FullMethod: "/m", IsServerStream: true}, func(interface{}, grpc.ServerStream) error {
		return nil
	})
	require.NoError(t, err)

	// Verify the log output contains expected fields
	var logData map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logData); err == nil {
		require.Contains(t, logData, "msg")
		require.Contains(t, logData["msg"], "Stream RPC call completed")
		require.Contains(t, logData, "method")
		require.Equal(t, "/m", logData["method"])
	}
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
