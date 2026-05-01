package mfapb

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

type mfaTestClientConn struct {
	invokeCount int
	invokeErr   error
}

func (c *mfaTestClientConn) Invoke(ctx context.Context, method string, args interface{}, reply interface{}, opts ...grpc.CallOption) error {
	c.invokeCount++
	return c.invokeErr
}

func (c *mfaTestClientConn) NewStream(ctx context.Context, desc *grpc.StreamDesc, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, errors.New("stream not expected")
}

type mfaTestRegistrar struct {
	desc *grpc.ServiceDesc
	impl interface{}
}

func (r *mfaTestRegistrar) RegisterService(desc *grpc.ServiceDesc, impl interface{}) {
	r.desc = desc
	r.impl = impl
}

type mfaProtoServer struct {
	UnimplementedMFAEngineServer
}

func (mfaProtoServer) GetStatus(context.Context, *MFAStatusRequest) (*MFAStatusResponse, error) {
	return &MFAStatusResponse{}, nil
}

func (mfaProtoServer) GetEnrolledFactors(context.Context, *FactorListRequest) (*FactorListResponse, error) {
	return &FactorListResponse{}, nil
}

func exerciseMFAProtoMessage(t *testing.T, msg proto.Message) {
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

func TestMFAGeneratedMessages(t *testing.T) {
	messages := []proto.Message{
		&MFAStatusRequest{},
		&MFAStatusResponse{},
		&FactorListRequest{},
		&FactorListResponse{},
		&Factor{},
	}
	for _, msg := range messages {
		exerciseMFAProtoMessage(t, msg)
	}
}

func TestMFAGeneratedGRPCPaths(t *testing.T) {
	conn := &mfaTestClientConn{}
	client := NewMFAEngineClient(conn)

	if _, err := client.GetStatus(context.Background(), &MFAStatusRequest{}); err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}
	if _, err := client.GetEnrolledFactors(context.Background(), &FactorListRequest{}); err != nil {
		t.Fatalf("GetEnrolledFactors failed: %v", err)
	}
	if conn.invokeCount != 2 {
		t.Fatalf("expected 2 client invokes, got %d", conn.invokeCount)
	}

	unimplemented := UnimplementedMFAEngineServer{}
	if _, err := unimplemented.GetStatus(context.Background(), &MFAStatusRequest{}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("expected unimplemented status, got %v", err)
	}
	if _, err := unimplemented.GetEnrolledFactors(context.Background(), &FactorListRequest{}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("expected unimplemented status, got %v", err)
	}

	registrar := &mfaTestRegistrar{}
	RegisterMFAEngineServer(registrar, mfaProtoServer{})
	if registrar.desc == nil || registrar.desc.ServiceName != MFAEngine_ServiceDesc.ServiceName {
		t.Fatalf("service registration mismatch: %#v", registrar.desc)
	}

	srv := mfaProtoServer{}
	interceptor := func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		return handler(ctx, req)
	}

	for i, method := range MFAEngine_ServiceDesc.Methods {
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

func TestMFAGeneratedAdditionalBranches(t *testing.T) {
	t.Run("client invoke errors", func(t *testing.T) {
		invokeErr := errors.New("invoke failed")
		conn := &mfaTestClientConn{invokeErr: invokeErr}
		client := NewMFAEngineClient(conn)

		if _, err := client.GetStatus(context.Background(), &MFAStatusRequest{}); !errors.Is(err, invokeErr) {
			t.Fatalf("expected status invoke error, got %v", err)
		}
		if _, err := client.GetEnrolledFactors(context.Background(), &FactorListRequest{}); !errors.Is(err, invokeErr) {
			t.Fatalf("expected enrolled-factors invoke error, got %v", err)
		}
	})

	t.Run("all method decode errors", func(t *testing.T) {
		srv := mfaProtoServer{}
		for _, method := range MFAEngine_ServiceDesc.Methods {
			if _, err := method.Handler(srv, context.Background(), func(interface{}) error { return errors.New("decode failed") }, nil); err == nil {
				t.Fatalf("expected decode error for method %s", method.MethodName)
			}
		}
	})

	t.Run("nil proto reflect and init guard", func(t *testing.T) {
		var statusReqNil *MFAStatusRequest
		_ = statusReqNil.ProtoReflect()
		var statusRespNil *MFAStatusResponse
		_ = statusRespNil.ProtoReflect()
		var listReqNil *FactorListRequest
		_ = listReqNil.ProtoReflect()
		var listRespNil *FactorListResponse
		_ = listRespNil.ProtoReflect()
		var factorNil *Factor
		_ = factorNil.ProtoReflect()

		file_mfa_mfa_proto_init()
	})
}
