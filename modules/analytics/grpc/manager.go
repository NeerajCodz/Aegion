package grpc

import (
	"context"
	"time"

	"github.com/aegion/aegion/internal/xlog"
	"google.golang.org/grpc"

	pb "github.com/aegion/aegion/internal/proto/analytics"
)

type Manager struct {
	logger *xlog.Logger
	server *Server
	config ServerConfig
	store  Store
	sync   SyncManager
}

func NewManager(
	logger *xlog.Logger,
	store Store,
	syncManager SyncManager,
	port int,
	maxStreams int,
	keepaliveTime int,
	keepaliveTimeout int,
) *Manager {
	return &Manager{
		logger: logger,
		store:  store,
		sync:   syncManager,
		config: ServerConfig{
			Port:                 port,
			MaxConcurrentStreams: maxStreams,
			KeepaliveTime:        keepaliveTime,
			KeepaliveTimeout:     keepaliveTimeout,
			Logger:               logger,
			UnaryInterceptor:     BuildInterceptorChain(logger, false, true, false),
			StreamInterceptor:    BuildStreamInterceptorChain(logger, false, true, false),
		},
	}
}

func (m *Manager) SetUnaryInterceptor(interceptor grpc.UnaryServerInterceptor) {
	m.config.UnaryInterceptor = interceptor
}

func (m *Manager) SetStreamInterceptor(interceptor grpc.StreamServerInterceptor) {
	m.config.StreamInterceptor = interceptor
}

func (m *Manager) Start(ctx context.Context) error {
	event := m.logger.Start(ctx, "grpc.manager.start", xlog.WithKind(xlog.KindSystem)).
		Set("port", m.config.Port)

	service := NewService(m.logger, m.store, m.sync, Config{
		MaxConcurrentStreams: m.config.MaxConcurrentStreams,
		KeepaliveTime:        m.config.KeepaliveTime,
		KeepaliveTimeout:     m.config.KeepaliveTimeout,
	})

	server, err := NewServer(m.config, service)
	if err != nil {
		event.Set("error", err.Error()).Error(err)
		_ = event.Emit()
		return err
	}

	m.server = server

	go func() {
		if err := server.Start(); err != nil {
			m.logger.Start(context.Background(), "grpc.manager.error", xlog.WithKind(xlog.KindSystem)).
				Set("error", err.Error()).Error(err)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	event.Set("bound_port", server.Port())
	event.Success()
	_ = event.Emit()
	return nil
}

func (m *Manager) Stop(ctx context.Context) error {
	event := m.logger.Start(ctx, "grpc.manager.stop", xlog.WithKind(xlog.KindSystem))

	if m.server == nil {
		event.Success()
		_ = event.Emit()
		return nil
	}

	timeout := 5 * time.Second
	if dl, ok := ctx.Deadline(); ok {
		timeout = time.Until(dl)
	}

	err := m.server.Stop(timeout)
	if err != nil {
		event.Set("error", err.Error()).Error(err)
	} else {
		event.Success()
	}
	_ = event.Emit()
	return err
}

func (m *Manager) Port() int {
	if m.server == nil {
		return 0
	}
	return m.server.Port()
}

func (m *Manager) IsRunning() bool {
	if m.server == nil {
		return false
	}
	return m.server.IsRunning()
}

func BuildInterceptorChain(
	logger *xlog.Logger,
	enableLogging bool,
	enableAuth bool,
	enableTracing bool,
) grpc.UnaryServerInterceptor {
	var interceptors []grpc.UnaryServerInterceptor

	if enableAuth {
		interceptors = append(interceptors, AuthInterceptor(nil))
	}

	if enableLogging && logger != nil {
		interceptors = append(interceptors, logger.UnaryServerInterceptor())
	}

	if len(interceptors) == 0 {
		return nil
	}

	return ChainUnaryInterceptors(interceptors...)
}

func BuildStreamInterceptorChain(
	logger *xlog.Logger,
	enableLogging bool,
	enableAuth bool,
	enableTracing bool,
) grpc.StreamServerInterceptor {
	var interceptors []grpc.StreamServerInterceptor

	if enableAuth {
		interceptors = append(interceptors, StreamAuthInterceptor(nil))
	}

	if enableLogging && logger != nil {
		interceptors = append(interceptors, logger.StreamServerInterceptor())
	}

	if len(interceptors) == 0 {
		return nil
	}

	return ChainStreamInterceptors(interceptors...)
}

func Register(grpcServer *grpc.Server, service *Service) {
	pb.RegisterAnalyticsServiceServer(grpcServer, service)
}
