package xlog

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
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

func TestUnaryServerInterceptor_EmitsEvent(t *testing.T) {
	var events []Record
	sink := SinkFunc(func(ctx context.Context, r Record) error {
		events = append(events, r)
		return nil
	})
	logger := New(Config{Sinks: []Sink{sink}})

	interceptor := logger.UnaryServerInterceptor()

	resp, err := interceptor(context.Background(), "req", &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}, func(ctx context.Context, req any) (any, error) {
		return "resp", nil
	})

	require.NoError(t, err)
	require.Equal(t, "resp", resp)
	require.Len(t, events, 1)

	fields := events[0].Fields
	require.Equal(t, "grpc.request", fields["event.name"])
	require.Equal(t, "request", fields["event.kind"])
	require.Equal(t, "success", fields["event.outcome"])
	require.Equal(t, "grpc", fields["rpc.system"])
	require.Equal(t, "/test.Service/Method", fields["rpc.method"])
	require.Equal(t, "OK", fields["rpc.grpc.status_code"])
}

func TestUnaryServerInterceptor_CapturesError(t *testing.T) {
	var events []Record
	sink := SinkFunc(func(ctx context.Context, r Record) error {
		events = append(events, r)
		return nil
	})
	logger := New(Config{Sinks: []Sink{sink}})

	interceptor := logger.UnaryServerInterceptor()

	wantErr := status.Error(codes.InvalidArgument, "bad request")
	_, err := interceptor(context.Background(), "req", &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}, func(ctx context.Context, req any) (any, error) {
		return nil, wantErr
	})

	require.Error(t, err)
	require.Len(t, events, 1)

	fields := events[0].Fields
	require.Equal(t, "rejected", fields["event.outcome"])
	require.Equal(t, "InvalidArgument", fields["rpc.grpc.status_code"])
	require.Contains(t, fields["error.message"], "bad request")
}

func TestStreamServerInterceptor_EmitsEvent(t *testing.T) {
	var events []Record
	sink := SinkFunc(func(ctx context.Context, r Record) error {
		events = append(events, r)
		return nil
	})
	logger := New(Config{Sinks: []Sink{sink}})

	interceptor := logger.StreamServerInterceptor()

	stream := &mockGRPCServerStream{ctx: context.Background()}
	err := interceptor(nil, stream, &grpc.StreamServerInfo{FullMethod: "/test.Service/Stream", IsServerStream: true}, func(srv any, ss grpc.ServerStream) error {
		return nil
	})

	require.NoError(t, err)
	require.Len(t, events, 1)

	fields := events[0].Fields
	require.Equal(t, "grpc.stream", fields["event.name"])
	require.Equal(t, "success", fields["event.outcome"])
	require.Equal(t, "/test.Service/Stream", fields["rpc.method"])
	require.Equal(t, true, fields["rpc.grpc.server_stream"])
}
