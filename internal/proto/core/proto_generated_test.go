package core

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type coreTestClientConn struct {
	invokeCount    int
	newStreamCount int
	invokeErr      error
	newStreamErr   error
	stream         grpc.ClientStream
}

func (c *coreTestClientConn) Invoke(ctx context.Context, method string, args interface{}, reply interface{}, opts ...grpc.CallOption) error {
	c.invokeCount++
	return c.invokeErr
}

func (c *coreTestClientConn) NewStream(ctx context.Context, desc *grpc.StreamDesc, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	c.newStreamCount++
	if c.newStreamErr != nil {
		return nil, c.newStreamErr
	}
	if c.stream != nil {
		return c.stream, nil
	}
	return &coreTestClientStream{ctx: ctx, recvErr: io.EOF}, nil
}

type coreTestClientStream struct {
	ctx          context.Context
	sendErr      error
	closeSendErr error
	recvErr      error
}

func (s *coreTestClientStream) Header() (metadata.MD, error) {
	return metadata.MD{}, nil
}

func (s *coreTestClientStream) Trailer() metadata.MD {
	return metadata.MD{}
}

func (s *coreTestClientStream) CloseSend() error {
	return s.closeSendErr
}

func (s *coreTestClientStream) Context() context.Context {
	if s.ctx != nil {
		return s.ctx
	}
	return context.Background()
}

func (s *coreTestClientStream) SendMsg(m interface{}) error {
	return s.sendErr
}

func (s *coreTestClientStream) RecvMsg(m interface{}) error {
	if s.recvErr != nil {
		return s.recvErr
	}
	return io.EOF
}

type coreTestServerStream struct {
	ctx     context.Context
	recvErr error
}

func (s *coreTestServerStream) SetHeader(metadata.MD) error {
	return nil
}

func (s *coreTestServerStream) SendHeader(metadata.MD) error {
	return nil
}

func (s *coreTestServerStream) SetTrailer(metadata.MD) {}

func (s *coreTestServerStream) Context() context.Context {
	if s.ctx != nil {
		return s.ctx
	}
	return context.Background()
}

func (s *coreTestServerStream) SendMsg(interface{}) error {
	return nil
}

func (s *coreTestServerStream) RecvMsg(interface{}) error {
	if s.recvErr != nil {
		return s.recvErr
	}
	return nil
}

type coreTestRegistrar struct {
	descs []*grpc.ServiceDesc
	impls []interface{}
}

func (r *coreTestRegistrar) RegisterService(desc *grpc.ServiceDesc, impl interface{}) {
	r.descs = append(r.descs, desc)
	r.impls = append(r.impls, impl)
}

type sessionProtoServer struct {
	UnimplementedSessionServiceServer
}

func (sessionProtoServer) Resolve(context.Context, *ResolveRequest) (*ResolveResponse, error) {
	return &ResolveResponse{}, nil
}
func (sessionProtoServer) Create(context.Context, *CreateSessionRequest) (*CreateSessionResponse, error) {
	return &CreateSessionResponse{}, nil
}
func (sessionProtoServer) Update(context.Context, *UpdateSessionRequest) (*UpdateSessionResponse, error) {
	return &UpdateSessionResponse{}, nil
}
func (sessionProtoServer) Revoke(context.Context, *RevokeSessionRequest) (*RevokeSessionResponse, error) {
	return &RevokeSessionResponse{}, nil
}
func (sessionProtoServer) RevokeAll(context.Context, *RevokeAllRequest) (*RevokeAllResponse, error) {
	return &RevokeAllResponse{}, nil
}
func (sessionProtoServer) List(context.Context, *ListSessionsRequest) (*ListSessionsResponse, error) {
	return &ListSessionsResponse{}, nil
}

type moduleRegistryProtoServer struct {
	UnimplementedModuleRegistryServer
}

func (moduleRegistryProtoServer) Register(context.Context, *RegisterRequest) (*RegisterResponse, error) {
	return &RegisterResponse{}, nil
}
func (moduleRegistryProtoServer) Deregister(context.Context, *DeregisterRequest) (*DeregisterResponse, error) {
	return &DeregisterResponse{}, nil
}
func (moduleRegistryProtoServer) Heartbeat(context.Context, *HeartbeatRequest) (*HeartbeatResponse, error) {
	return &HeartbeatResponse{}, nil
}
func (moduleRegistryProtoServer) GetModules(context.Context, *GetModulesRequest) (*GetModulesResponse, error) {
	return &GetModulesResponse{}, nil
}

type internalTokenProtoServer struct {
	UnimplementedInternalTokenServiceServer
}

func (internalTokenProtoServer) Validate(context.Context, *ValidateRequest) (*ValidateResponse, error) {
	return &ValidateResponse{}, nil
}
func (internalTokenProtoServer) GetCurrent(context.Context, *GetCurrentRequest) (*GetCurrentResponse, error) {
	return &GetCurrentResponse{}, nil
}
func (internalTokenProtoServer) SubscribeRotation(*SubscribeRotationRequest, grpc.ServerStreamingServer[TokenRotationEvent]) error {
	return nil
}

type eventBusProtoServer struct {
	UnimplementedEventBusServiceServer
}

func (eventBusProtoServer) Publish(context.Context, *PublishRequest) (*PublishResponse, error) {
	return &PublishResponse{}, nil
}
func (eventBusProtoServer) Subscribe(*SubscribeRequest, grpc.ServerStreamingServer[Event]) error {
	return nil
}
func (eventBusProtoServer) Acknowledge(context.Context, *AcknowledgeRequest) (*AcknowledgeResponse, error) {
	return &AcknowledgeResponse{}, nil
}

type courierProtoServer struct {
	UnimplementedCourierServiceServer
}

func (courierProtoServer) Enqueue(context.Context, *EnqueueRequest) (*EnqueueResponse, error) {
	return &EnqueueResponse{}, nil
}
func (courierProtoServer) GetStatus(context.Context, *GetStatusRequest) (*GetStatusResponse, error) {
	return &GetStatusResponse{}, nil
}
func (courierProtoServer) Cancel(context.Context, *CancelRequest) (*CancelResponse, error) {
	return &CancelResponse{}, nil
}

func exerciseCoreProtoMessage(t *testing.T, msg proto.Message) {
	t.Helper()

	if x, ok := any(msg).(interface{ Reset() }); ok {
		x.Reset()
	}
	if x, ok := any(msg).(interface{ String() string }); ok {
		_ = x.String()
	}
	if x, ok := any(msg).(interface{ ProtoMessage() }); ok {
		x.ProtoMessage()
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
	for _, methodName := range []string{"ProtoMessage", "ProtoReflect"} {
		if m := receiver.MethodByName(methodName); m.IsValid() && m.Type().NumIn() == 0 {
			m.Call(nil)
		}
		if m := nilReceiver.MethodByName(methodName); m.IsValid() && m.Type().NumIn() == 0 {
			func() {
				defer func() {
					_ = recover()
				}()
				m.Call(nil)
			}()
		}
	}
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

func TestCoreGeneratedMessages(t *testing.T) {
	messages := []proto.Message{
		&PublishRequest{},
		&PublishResponse{},
		&SubscribeRequest{},
		&Event{},
		&AcknowledgeRequest{},
		&AcknowledgeResponse{},
		&ValidateRequest{},
		&ValidateResponse{},
		&TokenMetadata{},
		&GetCurrentRequest{},
		&GetCurrentResponse{},
		&SubscribeRotationRequest{},
		&TokenRotationEvent{},
		&ResolveRequest{},
		&ResolveResponse{},
		&SessionContext{},
		&AuthMethod{},
		&CreateSessionRequest{},
		&CreateSessionResponse{},
		&UpdateSessionRequest{},
		&UpdateSessionResponse{},
		&RevokeSessionRequest{},
		&RevokeSessionResponse{},
		&RevokeAllRequest{},
		&RevokeAllResponse{},
		&ListSessionsRequest{},
		&ListSessionsResponse{},
		&SessionInfo{},
		&DeviceInfo{},
		&EnqueueRequest{},
		&EnqueueResponse{},
		&GetStatusRequest{},
		&GetStatusResponse{},
		&CancelRequest{},
		&CancelResponse{},
		&RegisterRequest{},
		&RegisterResponse{},
		&DeregisterRequest{},
		&DeregisterResponse{},
		&HeartbeatRequest{},
		&HeartbeatResponse{},
		&GetModulesRequest{},
		&GetModulesResponse{},
		&ModuleInstance{},
		&ModuleMetrics{},
	}
	for _, msg := range messages {
		exerciseCoreProtoMessage(t, msg)
	}
}

func TestCoreGeneratedEnums(t *testing.T) {
	for _, v := range []MessageType{
		MessageType_MESSAGE_TYPE_UNSPECIFIED,
		MessageType_MESSAGE_TYPE_EMAIL,
		MessageType_MESSAGE_TYPE_SMS,
	} {
		_ = v.String()
		_ = v.Enum()
		_ = v.Descriptor()
		_ = v.Type()
		_ = v.Number()
		_, _ = v.EnumDescriptor()
	}

	for _, v := range []DeliveryStatus{
		DeliveryStatus_DELIVERY_STATUS_UNSPECIFIED,
		DeliveryStatus_DELIVERY_STATUS_QUEUED,
		DeliveryStatus_DELIVERY_STATUS_PROCESSING,
		DeliveryStatus_DELIVERY_STATUS_SENT,
		DeliveryStatus_DELIVERY_STATUS_FAILED,
		DeliveryStatus_DELIVERY_STATUS_ABANDONED,
		DeliveryStatus_DELIVERY_STATUS_CANCELLED,
	} {
		_ = v.String()
		_ = v.Enum()
		_ = v.Descriptor()
		_ = v.Type()
		_ = v.Number()
		_, _ = v.EnumDescriptor()
	}

	for _, v := range []HealthStatus{
		HealthStatus_HEALTH_STATUS_UNKNOWN,
		HealthStatus_HEALTH_STATUS_HEALTHY,
		HealthStatus_HEALTH_STATUS_DEGRADED,
		HealthStatus_HEALTH_STATUS_UNHEALTHY,
	} {
		_ = v.String()
		_ = v.Enum()
		_ = v.Descriptor()
		_ = v.Type()
		_ = v.Number()
		_, _ = v.EnumDescriptor()
	}
}

func TestCoreGeneratedClients(t *testing.T) {
	conn := &coreTestClientConn{}

	sessionClient := NewSessionServiceClient(conn)
	_, _ = sessionClient.Resolve(context.Background(), &ResolveRequest{})
	_, _ = sessionClient.Create(context.Background(), &CreateSessionRequest{})
	_, _ = sessionClient.Update(context.Background(), &UpdateSessionRequest{})
	_, _ = sessionClient.Revoke(context.Background(), &RevokeSessionRequest{})
	_, _ = sessionClient.RevokeAll(context.Background(), &RevokeAllRequest{})
	_, _ = sessionClient.List(context.Background(), &ListSessionsRequest{})

	registryClient := NewModuleRegistryClient(conn)
	_, _ = registryClient.Register(context.Background(), &RegisterRequest{})
	_, _ = registryClient.Deregister(context.Background(), &DeregisterRequest{})
	_, _ = registryClient.Heartbeat(context.Background(), &HeartbeatRequest{})
	_, _ = registryClient.GetModules(context.Background(), &GetModulesRequest{})

	internalTokenClient := NewInternalTokenServiceClient(conn)
	_, _ = internalTokenClient.Validate(context.Background(), &ValidateRequest{})
	_, _ = internalTokenClient.GetCurrent(context.Background(), &GetCurrentRequest{})
	rotationStream, err := internalTokenClient.SubscribeRotation(context.Background(), &SubscribeRotationRequest{})
	if err != nil {
		t.Fatalf("SubscribeRotation failed: %v", err)
	}
	if _, err := rotationStream.Recv(); !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF from rotation stream, got %v", err)
	}

	eventClient := NewEventBusServiceClient(conn)
	_, _ = eventClient.Publish(context.Background(), &PublishRequest{})
	_, _ = eventClient.Acknowledge(context.Background(), &AcknowledgeRequest{})
	eventStream, err := eventClient.Subscribe(context.Background(), &SubscribeRequest{})
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}
	if _, err := eventStream.Recv(); !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF from event stream, got %v", err)
	}

	courierClient := NewCourierServiceClient(conn)
	_, _ = courierClient.Enqueue(context.Background(), &EnqueueRequest{})
	_, _ = courierClient.GetStatus(context.Background(), &GetStatusRequest{})
	_, _ = courierClient.Cancel(context.Background(), &CancelRequest{})

	if conn.invokeCount == 0 || conn.newStreamCount == 0 {
		t.Fatalf("expected invoke and stream paths to execute, got invokes=%d streams=%d", conn.invokeCount, conn.newStreamCount)
	}

	conn = &coreTestClientConn{newStreamErr: errors.New("new stream failed")}
	internalTokenClient = NewInternalTokenServiceClient(conn)
	if _, err := internalTokenClient.SubscribeRotation(context.Background(), &SubscribeRotationRequest{}); err == nil {
		t.Fatal("expected SubscribeRotation new stream error")
	}
	eventClient = NewEventBusServiceClient(conn)
	if _, err := eventClient.Subscribe(context.Background(), &SubscribeRequest{}); err == nil {
		t.Fatal("expected Subscribe new stream error")
	}

	conn = &coreTestClientConn{stream: &coreTestClientStream{ctx: context.Background(), sendErr: errors.New("send failed")}}
	internalTokenClient = NewInternalTokenServiceClient(conn)
	if _, err := internalTokenClient.SubscribeRotation(context.Background(), &SubscribeRotationRequest{}); err == nil {
		t.Fatal("expected SubscribeRotation send error")
	}
	eventClient = NewEventBusServiceClient(conn)
	if _, err := eventClient.Subscribe(context.Background(), &SubscribeRequest{}); err == nil {
		t.Fatal("expected Subscribe send error")
	}

	conn = &coreTestClientConn{stream: &coreTestClientStream{ctx: context.Background(), closeSendErr: errors.New("close failed")}}
	internalTokenClient = NewInternalTokenServiceClient(conn)
	if _, err := internalTokenClient.SubscribeRotation(context.Background(), &SubscribeRotationRequest{}); err == nil {
		t.Fatal("expected SubscribeRotation close send error")
	}
	eventClient = NewEventBusServiceClient(conn)
	if _, err := eventClient.Subscribe(context.Background(), &SubscribeRequest{}); err == nil {
		t.Fatal("expected Subscribe close send error")
	}
}

func TestCoreGeneratedClientInvokeErrors(t *testing.T) {
	invokeErr := errors.New("invoke failed")
	conn := &coreTestClientConn{invokeErr: invokeErr}

	sessionClient := NewSessionServiceClient(conn)
	if _, err := sessionClient.Resolve(context.Background(), &ResolveRequest{}); !errors.Is(err, invokeErr) {
		t.Fatalf("expected resolve invoke error, got %v", err)
	}
	if _, err := sessionClient.Create(context.Background(), &CreateSessionRequest{}); !errors.Is(err, invokeErr) {
		t.Fatalf("expected create invoke error, got %v", err)
	}
	if _, err := sessionClient.Update(context.Background(), &UpdateSessionRequest{}); !errors.Is(err, invokeErr) {
		t.Fatalf("expected update invoke error, got %v", err)
	}
	if _, err := sessionClient.Revoke(context.Background(), &RevokeSessionRequest{}); !errors.Is(err, invokeErr) {
		t.Fatalf("expected revoke invoke error, got %v", err)
	}
	if _, err := sessionClient.RevokeAll(context.Background(), &RevokeAllRequest{}); !errors.Is(err, invokeErr) {
		t.Fatalf("expected revoke-all invoke error, got %v", err)
	}
	if _, err := sessionClient.List(context.Background(), &ListSessionsRequest{}); !errors.Is(err, invokeErr) {
		t.Fatalf("expected list invoke error, got %v", err)
	}

	registryClient := NewModuleRegistryClient(conn)
	if _, err := registryClient.Register(context.Background(), &RegisterRequest{}); !errors.Is(err, invokeErr) {
		t.Fatalf("expected register invoke error, got %v", err)
	}
	if _, err := registryClient.Deregister(context.Background(), &DeregisterRequest{}); !errors.Is(err, invokeErr) {
		t.Fatalf("expected deregister invoke error, got %v", err)
	}
	if _, err := registryClient.Heartbeat(context.Background(), &HeartbeatRequest{}); !errors.Is(err, invokeErr) {
		t.Fatalf("expected heartbeat invoke error, got %v", err)
	}
	if _, err := registryClient.GetModules(context.Background(), &GetModulesRequest{}); !errors.Is(err, invokeErr) {
		t.Fatalf("expected get-modules invoke error, got %v", err)
	}

	internalTokenClient := NewInternalTokenServiceClient(conn)
	if _, err := internalTokenClient.Validate(context.Background(), &ValidateRequest{}); !errors.Is(err, invokeErr) {
		t.Fatalf("expected validate invoke error, got %v", err)
	}
	if _, err := internalTokenClient.GetCurrent(context.Background(), &GetCurrentRequest{}); !errors.Is(err, invokeErr) {
		t.Fatalf("expected get-current invoke error, got %v", err)
	}

	eventClient := NewEventBusServiceClient(conn)
	if _, err := eventClient.Publish(context.Background(), &PublishRequest{}); !errors.Is(err, invokeErr) {
		t.Fatalf("expected publish invoke error, got %v", err)
	}
	if _, err := eventClient.Acknowledge(context.Background(), &AcknowledgeRequest{}); !errors.Is(err, invokeErr) {
		t.Fatalf("expected acknowledge invoke error, got %v", err)
	}

	courierClient := NewCourierServiceClient(conn)
	if _, err := courierClient.Enqueue(context.Background(), &EnqueueRequest{}); !errors.Is(err, invokeErr) {
		t.Fatalf("expected enqueue invoke error, got %v", err)
	}
	if _, err := courierClient.GetStatus(context.Background(), &GetStatusRequest{}); !errors.Is(err, invokeErr) {
		t.Fatalf("expected get-status invoke error, got %v", err)
	}
	if _, err := courierClient.Cancel(context.Background(), &CancelRequest{}); !errors.Is(err, invokeErr) {
		t.Fatalf("expected cancel invoke error, got %v", err)
	}
}

func TestCoreGeneratedHandlerDecodeErrorsAllMethods(t *testing.T) {
	descriptors := []struct {
		desc *grpc.ServiceDesc
		srv  interface{}
	}{
		{desc: &SessionService_ServiceDesc, srv: sessionProtoServer{}},
		{desc: &ModuleRegistry_ServiceDesc, srv: moduleRegistryProtoServer{}},
		{desc: &InternalTokenService_ServiceDesc, srv: internalTokenProtoServer{}},
		{desc: &EventBusService_ServiceDesc, srv: eventBusProtoServer{}},
		{desc: &CourierService_ServiceDesc, srv: courierProtoServer{}},
	}

	for _, tc := range descriptors {
		for _, method := range tc.desc.Methods {
			if _, err := method.Handler(tc.srv, context.Background(), func(interface{}) error { return errors.New("decode failed") }, nil); err == nil {
				t.Fatalf("%s method %s expected decode error", tc.desc.ServiceName, method.MethodName)
			}
		}
	}
}

func TestCoreGeneratedInitGuardsAndNilProtoReflect(t *testing.T) {
	var eventNil *Event
	_ = eventNil.ProtoReflect()
	var sessionNil *ResolveRequest
	_ = sessionNil.ProtoReflect()
	var courierNil *EnqueueRequest
	_ = courierNil.ProtoReflect()
	var registryNil *RegisterRequest
	_ = registryNil.ProtoReflect()
	var internalTokenNil *ValidateRequest
	_ = internalTokenNil.ProtoReflect()

	file_core_events_proto_init()
	file_core_session_proto_init()
	file_core_courier_proto_init()
	file_core_registry_proto_init()
	file_core_internal_token_proto_init()
}

func TestCoreGeneratedUnimplementedServers(t *testing.T) {
	ctx := context.Background()

	sessionUnimplemented := UnimplementedSessionServiceServer{}
	_, _ = sessionUnimplemented.Resolve(ctx, &ResolveRequest{})
	_, _ = sessionUnimplemented.Create(ctx, &CreateSessionRequest{})
	_, _ = sessionUnimplemented.Update(ctx, &UpdateSessionRequest{})
	_, _ = sessionUnimplemented.Revoke(ctx, &RevokeSessionRequest{})
	_, _ = sessionUnimplemented.RevokeAll(ctx, &RevokeAllRequest{})
	_, _ = sessionUnimplemented.List(ctx, &ListSessionsRequest{})
	sessionUnimplemented.mustEmbedUnimplementedSessionServiceServer()
	sessionUnimplemented.testEmbeddedByValue()

	registryUnimplemented := UnimplementedModuleRegistryServer{}
	_, _ = registryUnimplemented.Register(ctx, &RegisterRequest{})
	_, _ = registryUnimplemented.Deregister(ctx, &DeregisterRequest{})
	_, _ = registryUnimplemented.Heartbeat(ctx, &HeartbeatRequest{})
	_, _ = registryUnimplemented.GetModules(ctx, &GetModulesRequest{})
	registryUnimplemented.mustEmbedUnimplementedModuleRegistryServer()
	registryUnimplemented.testEmbeddedByValue()

	internalTokenUnimplemented := UnimplementedInternalTokenServiceServer{}
	if _, err := internalTokenUnimplemented.Validate(ctx, &ValidateRequest{}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("expected unimplemented status, got %v", err)
	}
	if _, err := internalTokenUnimplemented.GetCurrent(ctx, &GetCurrentRequest{}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("expected unimplemented status, got %v", err)
	}
	if err := internalTokenUnimplemented.SubscribeRotation(&SubscribeRotationRequest{}, &grpc.GenericServerStream[SubscribeRotationRequest, TokenRotationEvent]{ServerStream: &coreTestServerStream{}}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("expected unimplemented status, got %v", err)
	}
	internalTokenUnimplemented.mustEmbedUnimplementedInternalTokenServiceServer()
	internalTokenUnimplemented.testEmbeddedByValue()

	eventUnimplemented := UnimplementedEventBusServiceServer{}
	if _, err := eventUnimplemented.Publish(ctx, &PublishRequest{}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("expected unimplemented status, got %v", err)
	}
	if err := eventUnimplemented.Subscribe(&SubscribeRequest{}, &grpc.GenericServerStream[SubscribeRequest, Event]{ServerStream: &coreTestServerStream{}}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("expected unimplemented status, got %v", err)
	}
	if _, err := eventUnimplemented.Acknowledge(ctx, &AcknowledgeRequest{}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("expected unimplemented status, got %v", err)
	}
	eventUnimplemented.mustEmbedUnimplementedEventBusServiceServer()
	eventUnimplemented.testEmbeddedByValue()

	courierUnimplemented := UnimplementedCourierServiceServer{}
	if _, err := courierUnimplemented.Enqueue(ctx, &EnqueueRequest{}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("expected unimplemented status, got %v", err)
	}
	if _, err := courierUnimplemented.GetStatus(ctx, &GetStatusRequest{}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("expected unimplemented status, got %v", err)
	}
	if _, err := courierUnimplemented.Cancel(ctx, &CancelRequest{}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("expected unimplemented status, got %v", err)
	}
	courierUnimplemented.mustEmbedUnimplementedCourierServiceServer()
	courierUnimplemented.testEmbeddedByValue()
}

func TestCoreGeneratedRegisterAndHandlers(t *testing.T) {
	descriptors := []struct {
		desc *grpc.ServiceDesc
		srv  interface{}
	}{
		{desc: &SessionService_ServiceDesc, srv: sessionProtoServer{}},
		{desc: &ModuleRegistry_ServiceDesc, srv: moduleRegistryProtoServer{}},
		{desc: &InternalTokenService_ServiceDesc, srv: internalTokenProtoServer{}},
		{desc: &EventBusService_ServiceDesc, srv: eventBusProtoServer{}},
		{desc: &CourierService_ServiceDesc, srv: courierProtoServer{}},
	}

	registrar := &coreTestRegistrar{}
	RegisterSessionServiceServer(registrar, sessionProtoServer{})
	RegisterModuleRegistryServer(registrar, moduleRegistryProtoServer{})
	RegisterInternalTokenServiceServer(registrar, internalTokenProtoServer{})
	RegisterEventBusServiceServer(registrar, eventBusProtoServer{})
	RegisterCourierServiceServer(registrar, courierProtoServer{})
	if len(registrar.descs) != 5 {
		t.Fatalf("expected 5 service registrations, got %d", len(registrar.descs))
	}

	interceptor := func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		return handler(ctx, req)
	}

	for _, tc := range descriptors {
		for i, method := range tc.desc.Methods {
			if _, err := method.Handler(tc.srv, context.Background(), func(interface{}) error { return nil }, nil); err != nil {
				t.Fatalf("%s method %s failed without interceptor: %v", tc.desc.ServiceName, method.MethodName, err)
			}
			if _, err := method.Handler(tc.srv, context.Background(), func(interface{}) error { return nil }, interceptor); err != nil {
				t.Fatalf("%s method %s failed with interceptor: %v", tc.desc.ServiceName, method.MethodName, err)
			}
			if i == 0 {
				if _, err := method.Handler(tc.srv, context.Background(), func(interface{}) error { return errors.New("decode failed") }, nil); err == nil {
					t.Fatalf("%s method %s expected decode error", tc.desc.ServiceName, method.MethodName)
				}
			}
		}

		for i, stream := range tc.desc.Streams {
			if err := stream.Handler(tc.srv, &coreTestServerStream{}); err != nil {
				t.Fatalf("%s stream %s failed: %v", tc.desc.ServiceName, stream.StreamName, err)
			}
			if i == 0 {
				if err := stream.Handler(tc.srv, &coreTestServerStream{recvErr: errors.New("recv failed")}); err == nil {
					t.Fatalf("%s stream %s expected recv error", tc.desc.ServiceName, stream.StreamName)
				}
			}
		}
	}
}
