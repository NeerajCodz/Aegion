package oauth2pb

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type oauth2TestClientConn struct {
	invokeCount int
}

func (c *oauth2TestClientConn) Invoke(ctx context.Context, method string, args interface{}, reply interface{}, opts ...grpc.CallOption) error {
	c.invokeCount++
	return nil
}

func (c *oauth2TestClientConn) NewStream(ctx context.Context, desc *grpc.StreamDesc, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, errors.New("stream not expected")
}

type oauth2TestRegistrar struct {
	desc *grpc.ServiceDesc
	impl interface{}
}

func (r *oauth2TestRegistrar) RegisterService(desc *grpc.ServiceDesc, impl interface{}) {
	r.desc = desc
	r.impl = impl
}

type oauth2ProtoServer struct {
	UnimplementedTokenStoreServer
}

func (oauth2ProtoServer) Introspect(context.Context, *IntrospectRequest) (*IntrospectResponse, error) {
	return &IntrospectResponse{}, nil
}

func (oauth2ProtoServer) Revoke(context.Context, *RevokeRequest) (*RevokeResponse, error) {
	return &RevokeResponse{}, nil
}

func (oauth2ProtoServer) InvalidateFamily(context.Context, *InvalidateFamilyRequest) (*InvalidateFamilyResponse, error) {
	return &InvalidateFamilyResponse{}, nil
}

func exerciseOAuth2ProtoMessage(t *testing.T, msg proto.Message) {
	t.Helper()

	if x, ok := any(msg).(interface{ Reset() }); ok {
		x.Reset()
	}
	if x, ok := any(msg).(interface{ String() string }); ok {
		_ = x.String()
	}
	_ = msg.ProtoReflect().Descriptor().FullName()
	if x, ok := any(msg).(interface{ Descriptor() ([]byte, []int) }); ok {
		_, _ = x.Descriptor()
	}

	b, err := proto.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	cloned := reflect.New(reflect.TypeOf(msg).Elem()).Interface().(proto.Message)
	if err := proto.Unmarshal(b, cloned); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	msgType := reflect.TypeOf(msg)
	nilReceiver := reflect.Zero(msgType)
	receiver := reflect.ValueOf(msg)

	for i := 0; i < msgType.NumMethod(); i++ {
		method := msgType.Method(i)
		if !strings.HasPrefix(method.Name, "Get") || method.Type.NumIn() != 1 {
			continue
		}
		method.Func.Call([]reflect.Value{receiver})
		func() {
			defer func() {
				_ = recover()
			}()
			method.Func.Call([]reflect.Value{nilReceiver})
		}()
	}
}

func TestOAuth2GeneratedMessages(t *testing.T) {
	messages := []proto.Message{
		&IntrospectRequest{},
		&IntrospectResponse{},
		&RevokeRequest{},
		&RevokeResponse{},
		&InvalidateFamilyRequest{},
		&InvalidateFamilyResponse{},
	}
	for _, msg := range messages {
		exerciseOAuth2ProtoMessage(t, msg)
	}
}

func TestOAuth2GeneratedGRPCPaths(t *testing.T) {
	conn := &oauth2TestClientConn{}
	client := NewTokenStoreClient(conn)

	if _, err := client.Introspect(context.Background(), &IntrospectRequest{}); err != nil {
		t.Fatalf("Introspect failed: %v", err)
	}
	if _, err := client.Revoke(context.Background(), &RevokeRequest{}); err != nil {
		t.Fatalf("Revoke failed: %v", err)
	}
	if _, err := client.InvalidateFamily(context.Background(), &InvalidateFamilyRequest{}); err != nil {
		t.Fatalf("InvalidateFamily failed: %v", err)
	}
	if conn.invokeCount != 3 {
		t.Fatalf("expected 3 client invokes, got %d", conn.invokeCount)
	}

	unimplemented := UnimplementedTokenStoreServer{}
	if _, err := unimplemented.Introspect(context.Background(), &IntrospectRequest{}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("expected unimplemented status, got %v", err)
	}
	if _, err := unimplemented.Revoke(context.Background(), &RevokeRequest{}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("expected unimplemented status, got %v", err)
	}
	if _, err := unimplemented.InvalidateFamily(context.Background(), &InvalidateFamilyRequest{}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("expected unimplemented status, got %v", err)
	}

	registrar := &oauth2TestRegistrar{}
	RegisterTokenStoreServer(registrar, oauth2ProtoServer{})
	if registrar.desc == nil || registrar.desc.ServiceName != TokenStore_ServiceDesc.ServiceName {
		t.Fatalf("service registration mismatch: %#v", registrar.desc)
	}

	srv := oauth2ProtoServer{}
	interceptor := func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		return handler(ctx, req)
	}

	for i, method := range TokenStore_ServiceDesc.Methods {
		if _, err := method.Handler(srv, context.Background(), func(interface{}) error { return nil }, nil); err != nil {
			t.Fatalf("method %s without interceptor failed: %v", method.MethodName, err)
		}
		if _, err := method.Handler(srv, context.Background(), func(interface{}) error { return nil }, interceptor); err != nil {
			t.Fatalf("method %s with interceptor failed: %v", method.MethodName, err)
		}
		if i == 0 {
			if _, err := method.Handler(srv, context.Background(), func(interface{}) error { return errors.New("decode failed") }, nil); err == nil {
				t.Fatalf("expected decode error for method %s", method.MethodName)
			}
		}
	}
}
