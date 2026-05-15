package moduleserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"
	corepb "github.com/aegion/aegion/internal/proto/core"
	"github.com/aegion/aegion/internal/xlog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

var (
	signalNotify           = signal.Notify
	signalStop             = signal.Stop
	cryptoRuntimeSelfCheck = platformcrypto.RuntimeSelfCheck
)

// Config defines a standard HTTP module process contract.
type Config struct {
	Module             string
	Version            string
	ListenAddr         string
	Capabilities       []string
	Routes             []string
	GRPCServices       []string
	EventSubscriptions []string
	RegisterHTTPRoutes func(mux *http.ServeMux)
	GRPCListenAddr     string
	CoreGRPCAddr       string
	InternalToken      string
	RegisterGRPC       func(*grpc.Server)
}

type metaResponse struct {
	Module             string   `json:"module"`
	Version            string   `json:"version"`
	Capabilities       []string `json:"capabilities"`
	Routes             []string `json:"routes"`
	GRPCServices       []string `json:"grpc_services"`
	EventSubscriptions []string `json:"event_subscriptions"`
}

func buildModuleMux(cfg Config) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "ok",
			"module": cfg.Module,
		})
	})
	mux.HandleFunc("/ready", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "ready",
			"module": cfg.Module,
		})
	})
	mux.HandleFunc("/meta", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(metaResponse{
			Module:             cfg.Module,
			Version:            cfg.Version,
			Capabilities:       cfg.Capabilities,
			Routes:             cfg.Routes,
			GRPCServices:       cfg.GRPCServices,
			EventSubscriptions: cfg.EventSubscriptions,
		})
	})
	if cfg.RegisterHTTPRoutes != nil {
		cfg.RegisterHTTPRoutes(mux)
	}
	return mux
}

// Run starts a module HTTP server that exposes /health, /ready and /meta.
func Run(cfg Config) error {
	if cfg.Module == "" {
		return errors.New("module name is required")
	}

	log := xlog.New(xlog.Config{
		Level:            os.Getenv("AEGION_LOG_LEVEL"),
		Format:           os.Getenv("AEGION_LOG_FORMAT"),
		ServiceName:      cfg.Module,
		ServiceNamespace: os.Getenv("AEGION_LOG_NAMESPACE"),
		Environment:      os.Getenv("AEGION_ENV"),
		CloudRegion:      os.Getenv("AEGION_CLOUD_REGION"),
		Developer:        os.Getenv("DEV_NAME"),
		ServiceVersion:   cfg.Version,
	})

	if err := cryptoRuntimeSelfCheck(); err != nil {
		return fmt.Errorf("[%s] crypto runtime self-check failed: %w", cfg.Module, err)
	}
	if cfg.Version == "" {
		cfg.Version = "0.1.0"
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = "0.0.0.0:9000"
	}

	srv := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      buildModuleMux(cfg),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	var grpcServer *grpc.Server
	var grpcErrCh <-chan error
	if cfg.RegisterGRPC != nil || cfg.CoreGRPCAddr != "" || len(cfg.GRPCServices) > 0 {
		if cfg.GRPCListenAddr == "" {
			cfg.GRPCListenAddr = "0.0.0.0:9100"
		}
		var err error
		grpcServer, grpcErrCh, err = startGRPCServer(cfg)
		if err != nil {
			return err
		}
	}
	if cfg.CoreGRPCAddr != "" {
		go registerWithCore(context.Background(), cfg, log)
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("module server listening",
			"module", cfg.Module,
			"listen_addr", cfg.ListenAddr,
			"version", cfg.Version,
		)
		err := srv.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	stop := make(chan os.Signal, 1)
	signalNotify(stop, os.Interrupt, syscall.SIGTERM)
	defer signalStop(stop)

	select {
	case err := <-errCh:
		return err
	case err := <-grpcErrCh:
		return err
	case <-stop:
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if grpcServer != nil {
		grpcServer.GracefulStop()
	}
	return srv.Shutdown(ctx)
}

func startGRPCServer(cfg Config) (*grpc.Server, <-chan error, error) {
	errCh := make(chan error, 1)
	log := xlog.Default().WithComponent("moduleserver.grpc")
	server := grpc.NewServer(
		grpc.UnaryInterceptor(log.UnaryServerInterceptor()),
		grpc.StreamInterceptor(log.StreamServerInterceptor()),
	)
	if cfg.RegisterGRPC != nil {
		cfg.RegisterGRPC(server)
	}
	listener, err := net.Listen("tcp", cfg.GRPCListenAddr)
	if err != nil {
		return nil, nil, fmt.Errorf("[%s] gRPC listen failed: %w", cfg.Module, err)
	}
	go func() {
		log.Info("module gRPC server listening",
			"module", cfg.Module,
			"listen_addr", cfg.GRPCListenAddr,
			"version", cfg.Version,
		)
		if err := server.Serve(listener); err != nil {
			errCh <- err
			return
		}
		errCh <- nil
	}()
	return server, errCh, nil
}

func registerWithCore(ctx context.Context, cfg Config, log *xlog.Logger) {
	conn, err := grpc.NewClient(cfg.CoreGRPCAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.ErrorContext(ctx, "core gRPC dial failed", "error", err, "core_addr", cfg.CoreGRPCAddr)
		return
	}
	defer conn.Close()
	client := corepb.NewModuleRegistryClient(conn)
	callCtx, callCancel := context.WithTimeout(ctx, 10*time.Second)
	defer callCancel()
	if cfg.InternalToken != "" {
		callCtx = metadata.AppendToOutgoingContext(callCtx, "x-aegion-internal-token", cfg.InternalToken)
	}
	resp, err := client.Register(callCtx, &corepb.RegisterRequest{
		Module:             cfg.Module,
		Version:            cfg.Version,
		Address:            cfg.GRPCListenAddr,
		Routes:             cfg.Routes,
		Capabilities:       cfg.Capabilities,
		GrpcServices:       cfg.GRPCServices,
		EventSubscriptions: cfg.EventSubscriptions,
	})
	if err != nil {
		log.ErrorContext(ctx, "core gRPC registration failed", "error", err, "module", cfg.Module)
		return
	}
	if !resp.GetSuccess() {
		log.ErrorContext(ctx, "core gRPC registration rejected", "error", resp.GetError(), "module", cfg.Module)
		return
	}
	log.InfoContext(ctx, "core gRPC registration completed", "module", cfg.Module, "instance_id", resp.GetInstanceId())
}

// EnvOrDefault returns env value if set, otherwise fallback.
func EnvOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
