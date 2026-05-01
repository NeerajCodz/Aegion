package grpc

import (
	"context"
	"time"

	"github.com/rs/zerolog"
	"google.golang.org/grpc"

	pb "github.com/aegion/aegion/internal/proto/analytics"
)

// Manager manages the gRPC server lifecycle.
type Manager struct {
	logger zerolog.Logger
	server *Server
	config ServerConfig
	store  Store
	sync   SyncManager
}

// NewManager creates a new gRPC manager.
func NewManager(
	logger zerolog.Logger,
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
			UnaryInterceptor:     nil,
			StreamInterceptor:    nil,
		},
	}
}

// SetUnaryInterceptor sets the unary interceptor.
func (m *Manager) SetUnaryInterceptor(interceptor grpc.UnaryServerInterceptor) {
	m.config.UnaryInterceptor = interceptor
}

// SetStreamInterceptor sets the stream interceptor.
func (m *Manager) SetStreamInterceptor(interceptor grpc.StreamServerInterceptor) {
	m.config.StreamInterceptor = interceptor
}

// Start starts the gRPC server.
func (m *Manager) Start(ctx context.Context) error {
	service := NewService(m.logger, m.store, m.sync, Config{
		MaxConcurrentStreams: m.config.MaxConcurrentStreams,
		KeepaliveTime:        m.config.KeepaliveTime,
		KeepaliveTimeout:     m.config.KeepaliveTimeout,
	})

	server, err := NewServer(m.config, service)
	if err != nil {
		return err
	}

	m.server = server

	// Start server in background
	go func() {
		if err := server.Start(); err != nil {
			m.logger.Error().Err(err).Msg("gRPC server error")
		}
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	m.logger.Info().Int("port", server.Port()).Msg("gRPC server started")
	return nil
}

// Stop stops the gRPC server.
func (m *Manager) Stop(ctx context.Context) error {
	if m.server == nil {
		return nil
	}

	timeout := 5 * time.Second
	if dl, ok := ctx.Deadline(); ok {
		timeout = time.Until(dl)
	}

	return m.server.Stop(timeout)
}

// Port returns the port the server is listening on.
func (m *Manager) Port() int {
	if m.server == nil {
		return 0
	}
	return m.server.Port()
}

// IsRunning returns whether the server is running.
func (m *Manager) IsRunning() bool {
	if m.server == nil {
		return false
	}
	return m.server.IsRunning()
}

// BuildInterceptorChain builds a chain of interceptors based on configuration.
func BuildInterceptorChain(
	logger zerolog.Logger,
	enableLogging bool,
	enableAuth bool,
	enableTracing bool,
) grpc.UnaryServerInterceptor {
	var interceptors []grpc.UnaryServerInterceptor

	if enableAuth {
		interceptors = append(interceptors, AuthInterceptor(nil))
	}

	if enableLogging {
		interceptors = append(interceptors, LoggingInterceptor(logger))
	}

	if len(interceptors) == 0 {
		return nil
	}

	return ChainUnaryInterceptors(interceptors...)
}

// BuildStreamInterceptorChain builds a chain of stream interceptors.
func BuildStreamInterceptorChain(
	logger zerolog.Logger,
	enableLogging bool,
	enableTracing bool,
) grpc.StreamServerInterceptor {
	if !enableLogging {
		return nil
	}

	return StreamLoggingInterceptor(logger)
}

// Register registers the gRPC analytics service with a gRPC server.
// This is useful for embedding gRPC in an existing server.
func Register(grpcServer *grpc.Server, service *Service) {
	pb.RegisterAnalyticsServiceServer(grpcServer, service)
}
