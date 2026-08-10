package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"

	"github.com/aegion/aegion/core/moduleauth"
	"github.com/aegion/aegion/core/registry"
	"github.com/aegion/aegion/internal/platform/config"
	corepb "github.com/aegion/aegion/internal/proto/core"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// grpcControlPlane is optional for test doubles while production Server starts
// its registry before the HTTP process accepts traffic.
type grpcControlPlane interface {
	StartGRPCControlPlane() error
}

type registryGRPCServer struct {
	server   *grpc.Server
	listener net.Listener
	mu       sync.Mutex
}

// StartGRPCControlPlane starts the core registry on its separate, mTLS-only
// listener. An empty listen address intentionally disables this control plane
// for isolated development and tests; production validation rejects that state.
func (s *Server) StartGRPCControlPlane() error {
	if s.registryGRPC != nil {
		return nil
	}
	listenAddr := strings.TrimSpace(s.cfg.Server.Registry.GRPCListenAddr)
	if listenAddr == "" {
		return nil
	}
	if s.moduleAuth == nil {
		return errors.New("module credential manager is unavailable")
	}
	tlsConfig, err := registryTLSConfig(s.cfg.Server.TLS)
	if err != nil {
		return fmt.Errorf("configure registry gRPC TLS: %w", err)
	}
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("listen for registry gRPC: %w", err)
	}

	requirements := map[string]moduleauth.Requirement{
		corepb.InternalTokenService_GetCurrent_FullMethodName: {Bootstrap: true},
		corepb.InternalTokenService_Validate_FullMethodName: {
			Audience: "core.registry", Permission: "registry:register",
		},
		corepb.ModuleRegistry_Register_FullMethodName: {
			Audience: "core.registry", Permission: "registry:register",
		},
		corepb.ModuleRegistry_Deregister_FullMethodName: {
			Audience: "core.registry", Permission: "registry:deregister",
		},
		corepb.ModuleRegistry_Heartbeat_FullMethodName: {
			Audience: "core.registry", Permission: "registry:heartbeat",
		},
		corepb.ModuleRegistry_GetModules_FullMethodName: {
			Audience: "core.registry", Permission: "registry:read",
		},
	}
	grpcServer := grpc.NewServer(
		grpc.Creds(credentials.NewTLS(tlsConfig)),
		grpc.ChainUnaryInterceptor(
			s.log.UnaryServerInterceptor(),
			s.moduleAuth.UnaryServerInterceptor(requirements),
		),
		grpc.StreamInterceptor(s.log.StreamServerInterceptor()),
	)
	corepb.RegisterInternalTokenServiceServer(grpcServer, moduleauth.NewGRPCService(s.moduleAuth))
	corepb.RegisterModuleRegistryServer(grpcServer, registry.NewGRPCService(s.registry))
	controlPlane := &registryGRPCServer{server: grpcServer, listener: listener}
	s.registryGRPC = controlPlane
	go func() {
		if err := grpcServer.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			s.log.LogWideEvent(context.Background(), "registry gRPC listener stopped", map[string]any{
				"event.kind":    "system",
				"event.outcome": "error",
				"error.type":    fmt.Sprintf("%T", err),
				"error.message": err.Error(),
			})
		}
	}()
	return nil
}

func (s *Server) shutdownGRPCControlPlane(ctx context.Context) error {
	if s.registryGRPC == nil {
		return nil
	}
	s.registryGRPC.mu.Lock()
	defer s.registryGRPC.mu.Unlock()
	if s.registryGRPC.server == nil {
		return nil
	}
	done := make(chan struct{})
	go func() {
		s.registryGRPC.server.GracefulStop()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		s.registryGRPC.server.Stop()
		return ctx.Err()
	}
}

func registryTLSConfig(cfg config.TLSConfig) (*tls.Config, error) {
	if !cfg.Enabled {
		return nil, errors.New("server TLS must be enabled")
	}
	if strings.TrimSpace(cfg.CertFile) == "" || strings.TrimSpace(cfg.KeyFile) == "" {
		return nil, errors.New("server certificate and key files are required")
	}
	if strings.TrimSpace(cfg.ClientCAFile) == "" {
		return nil, errors.New("client CA file is required")
	}
	certificate, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load server certificate: %w", err)
	}
	caPEM, err := os.ReadFile(cfg.ClientCAFile)
	if err != nil {
		return nil, fmt.Errorf("read client CA: %w", err)
	}
	clientCAs := x509.NewCertPool()
	if !clientCAs.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("client CA file does not contain a certificate")
	}

	minVersion, err := tlsVersion(cfg.MinVersion)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		Certificates: []tls.Certificate{certificate},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientCAs,
		MinVersion:   minVersion,
	}, nil
}

func tlsVersion(version string) (uint16, error) {
	switch strings.TrimSpace(version) {
	case "", "1.2":
		return tls.VersionTLS12, nil
	case "1.3":
		return tls.VersionTLS13, nil
	default:
		return 0, fmt.Errorf("unsupported minimum TLS version %q", version)
	}
}
