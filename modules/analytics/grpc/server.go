package grpc

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"

	pb "github.com/aegion/aegion/internal/proto/analytics"
)

// Server holds the gRPC server configuration and components.
type Server struct {
	grpcServer      *grpc.Server
	logger          zerolog.Logger
	port            int
	listener        net.Listener
	service         *Service
	unaryInterceptor grpc.UnaryServerInterceptor
	streamInterceptor grpc.StreamServerInterceptor
}

// ServerConfig holds gRPC server configuration.
type ServerConfig struct {
	Port                    int
	MaxConcurrentStreams    int
	KeepaliveTime           int
	KeepaliveTimeout        int
	MaxConnectionIdleTime   int
	Logger                  zerolog.Logger
	UnaryInterceptor        grpc.UnaryServerInterceptor
	StreamInterceptor       grpc.StreamServerInterceptor
	AuthVerifier            func(context.Context) error
	LoggingEnabled          bool
	TracingEnabled          bool
}

// NewServer creates a new gRPC server for pb.
func NewServer(cfg ServerConfig, service *Service) (*Server, error) {
	// Support cfg.Port == 0 (ephemeral port). We'll store the actual bound port from the listener.
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.Port))
	if err != nil {
		return nil, fmt.Errorf("failed to listen on port %d: %w", cfg.Port, err)
	}
	port := 0
	if addr, ok := listener.Addr().(*net.TCPAddr); ok {
		port = addr.Port
	}

	// Build gRPC server options
	opts := []grpc.ServerOption{
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             5 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.MaxHeaderListSize(16 * 1024), // 16KB default
	}
	if cfg.MaxConcurrentStreams > 0 {
		opts = append(opts, grpc.MaxConcurrentStreams(uint32(cfg.MaxConcurrentStreams)))
	}
	if cfg.KeepaliveTime > 0 || cfg.KeepaliveTimeout > 0 {
		opts = append(opts, grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    time.Duration(cfg.KeepaliveTime) * time.Second,
			Timeout: time.Duration(cfg.KeepaliveTimeout) * time.Second,
		}))
	}

	// Add interceptors
	if cfg.UnaryInterceptor != nil {
		opts = append(opts, grpc.UnaryInterceptor(cfg.UnaryInterceptor))
	}

	if cfg.StreamInterceptor != nil {
		opts = append(opts, grpc.StreamInterceptor(cfg.StreamInterceptor))
	}

	grpcServer := grpc.NewServer(opts...)

	// Register service
	pb.RegisterAnalyticsServiceServer(grpcServer, service)

	// Enable reflection for debugging
	reflection.Register(grpcServer)

	return &Server{
		grpcServer: grpcServer,
		logger:     cfg.Logger,
		port:       port,
		listener:   listener,
		service:    service,
	}, nil
}

// Start starts the gRPC server and blocks until shutdown or error.
func (s *Server) Start() error {
	s.logger.Info().Int("port", s.port).Msg("Starting gRPC server")

	if err := s.grpcServer.Serve(s.listener); err != nil {
		return fmt.Errorf("gRPC server error: %w", err)
	}

	return nil
}

// StartAsync starts the gRPC server in a background goroutine.
func (s *Server) StartAsync() error {
	go func() {
		if err := s.Start(); err != nil {
			s.logger.Error().Err(err).Msg("gRPC server error")
		}
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)
	return nil
}

// Stop gracefully stops the gRPC server.
func (s *Server) Stop(timeout time.Duration) error {
	s.logger.Info().Msg("Stopping gRPC server")

	done := make(chan struct{})
	go func() {
		s.grpcServer.GracefulStop()
		close(done)
	}()

	select {
	case <-done:
		s.logger.Info().Msg("gRPC server stopped gracefully")
		return nil
	case <-time.After(timeout):
		s.logger.Warn().Msg("gRPC server graceful stop timeout, forcing shutdown")
		s.grpcServer.Stop()
		return fmt.Errorf("graceful stop timeout")
	}
}

// Port returns the port the server is listening on.
func (s *Server) Port() int {
	return s.port
}

// IsRunning returns whether the server is currently running.
func (s *Server) IsRunning() bool {
	return s.grpcServer != nil
}
