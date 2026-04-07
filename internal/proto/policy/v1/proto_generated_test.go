package policypb

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

type policyTestClientConn struct {
	invokeCount int
}

func (c *policyTestClientConn) Invoke(ctx context.Context, method string, args interface{}, reply interface{}, opts ...grpc.CallOption) error {
	c.invokeCount++
	return nil
}

func (c *policyTestClientConn) NewStream(ctx context.Context, desc *grpc.StreamDesc, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, errors.New("stream not expected")
}

type policyTestRegistrar struct {
	desc *grpc.ServiceDesc
	impl interface{}
}

func (r *policyTestRegistrar) RegisterService(desc *grpc.ServiceDesc, impl interface{}) {
	r.desc = desc
	r.impl = impl
}

type policyProtoServer struct {
	UnimplementedPolicyEngineServer
}

func (policyProtoServer) Check(context.Context, *CheckRequest) (*CheckResponse, error) {
	return &CheckResponse{Allowed: true}, nil
}

func (policyProtoServer) BatchCheck(context.Context, *BatchCheckRequest) (*BatchCheckResponse, error) {
	return &BatchCheckResponse{}, nil
}

func exercisePolicyProtoMessage(t *testing.T, msg proto.Message) {
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

func TestPolicyGeneratedMessages(t *testing.T) {
	messages := []proto.Message{
		&CheckRequest{},
		&CheckResponse{},
		&BatchCheckRequest{},
		&BatchCheckResponse{},
		&Context{},
	}
	for _, msg := range messages {
		exercisePolicyProtoMessage(t, msg)
	}
}

func TestPolicyGeneratedGRPCPaths(t *testing.T) {
	conn := &policyTestClientConn{}
	client := NewPolicyEngineClient(conn)

	if _, err := client.Check(context.Background(), &CheckRequest{}); err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if _, err := client.BatchCheck(context.Background(), &BatchCheckRequest{}); err != nil {
		t.Fatalf("BatchCheck failed: %v", err)
	}
	if conn.invokeCount != 2 {
		t.Fatalf("expected 2 client invokes, got %d", conn.invokeCount)
	}

	unimplemented := UnimplementedPolicyEngineServer{}
	if _, err := unimplemented.Check(context.Background(), &CheckRequest{}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("expected unimplemented status, got %v", err)
	}
	if _, err := unimplemented.BatchCheck(context.Background(), &BatchCheckRequest{}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("expected unimplemented status, got %v", err)
	}

	registrar := &policyTestRegistrar{}
	RegisterPolicyEngineServer(registrar, policyProtoServer{})
	if registrar.desc == nil || registrar.desc.ServiceName != PolicyEngine_ServiceDesc.ServiceName {
		t.Fatalf("service registration mismatch: %#v", registrar.desc)
	}

	srv := policyProtoServer{}
	interceptor := func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		return handler(ctx, req)
	}

	for i, method := range PolicyEngine_ServiceDesc.Methods {
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
