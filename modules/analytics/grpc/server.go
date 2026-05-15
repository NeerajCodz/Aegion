package grpc

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/aegion/aegion/internal/xlog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"

	pb "github.com/aegion/aegion/internal/proto/analytics"
)

type Server struct {
	grpcServer        *grpc.Server
	logger            *xlog.Logger
	port              int
	listener          net.Listener
	service           *Service
	unaryInterceptor  grpc.UnaryServerInterceptor
	streamInterceptor grpc.StreamServerInterceptor
}

type ServerConfig struct {
	Port                  int
	MaxConcurrentStreams  int
	KeepaliveTime         int
	KeepaliveTimeout      int
	MaxConnectionIdleTime int
	Logger                *xlog.Logger
	UnaryInterceptor      grpc.UnaryServerInterceptor
	StreamInterceptor     grpc.StreamServerInterceptor
	AuthVerifier          func(context.Context) error
	LoggingEnabled        bool
	TracingEnabled        bool
}

func NewServer(cfg ServerConfig, service *Service) (*Server, error) {
	event := cfg.Logger.Start(context.Background(), "grpc.server.start", xlog.WithKind(xlog.KindSystem)).
		Set("port", cfg.Port)

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.Port))
	if err != nil {
		event.Set("error", err.Error()).Error(err)
		_ = event.Emit()
		return nil, fmt.Errorf("failed to listen on port %d: %w", cfg.Port, err)
	}
	port := 0
	if addr, ok := listener.Addr().(*net.TCPAddr); ok {
		port = addr.Port
	}

	opts := []grpc.ServerOption{
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             5 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.MaxHeaderListSize(16 * 1024),
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

	if cfg.UnaryInterceptor != nil {
		opts = append(opts, grpc.UnaryInterceptor(cfg.UnaryInterceptor))
	}

	if cfg.StreamInterceptor != nil {
		opts = append(opts, grpc.StreamInterceptor(cfg.StreamInterceptor))
	}

	grpcServer := grpc.NewServer(opts...)

	pb.RegisterAnalyticsServiceServer(grpcServer, service)

	reflection.Register(grpcServer)

	event.Set("bound_port", port)
	event.Success()
	_ = event.Emit()

	return &Server{
		grpcServer: grpcServer,
		logger:     cfg.Logger,
		port:       port,
		listener:   listener,
		service:    service,
	}, nil
}

func (s *Server) Start() error {
	event := s.logger.Start(context.Background(), "grpc.server.serve", xlog.WithKind(xlog.KindSystem)).
		Set("port", s.port)

	if err := s.grpcServer.Serve(s.listener); err != nil {
		event.Set("error", err.Error()).Error(err)
		_ = event.Emit()
		return fmt.Errorf("gRPC server error: %w", err)
	}

	event.Success()
	_ = event.Emit()
	return nil
}

func (s *Server) StartAsync() error {
	go func() {
		if err := s.Start(); err != nil {
			s.logger.Start(context.Background(), "grpc.server.error", xlog.WithKind(xlog.KindSystem)).
				Set("error", err.Error()).Error(err)
		}
	}()

	time.Sleep(100 * time.Millisecond)
	return nil
}

func (s *Server) Stop(timeout time.Duration) error {
	event := s.logger.Start(context.Background(), "grpc.server.stop", xlog.WithKind(xlog.KindSystem))

	done := make(chan struct{})
	go func() {
		s.grpcServer.GracefulStop()
		close(done)
	}()

	select {
	case <-done:
		event.Success()
		_ = event.Emit()
		return nil
	case <-time.After(timeout):
		event.Timeout(fmt.Errorf("graceful stop timeout"))
		_ = event.Emit()
		s.grpcServer.Stop()
		return fmt.Errorf("graceful stop timeout")
	}
}

func (s *Server) Port() int {
	return s.port
}

func (s *Server) IsRunning() bool {
	return s.grpcServer != nil
}
