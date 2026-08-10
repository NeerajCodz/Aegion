package moduleserver

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"
	corepb "github.com/aegion/aegion/internal/proto/core"
	"github.com/aegion/aegion/internal/xlog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

var (
	signalNotify           = signal.Notify
	signalStop             = signal.Stop
	cryptoRuntimeSelfCheck = platformcrypto.RuntimeSelfCheck
)

// Config defines a standard HTTP module process contract.
type Config struct {
	Module                  string
	Version                 string
	ListenAddr              string
	Capabilities            []string
	Routes                  []string
	GRPCServices            []string
	EventSubscriptions      []string
	RegisterHTTPRoutes      func(mux *http.ServeMux)
	Readiness               func(context.Context) error
	GRPCListenAddr          string
	CoreGRPCAddr            string
	CoreGRPCTLS             *tls.Config
	GRPCServerTLS           *tls.Config
	BootstrapCredential     string
	BootstrapCredentialFile string
	RegisterGRPC            func(*grpc.Server)
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
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if cfg.Readiness != nil {
			if err := cfg.Readiness(r.Context()); err != nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"status": "not_ready",
					"module": cfg.Module,
				})
				return
			}
		}
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
	if err := applyControlPlaneEnvironment(&cfg); err != nil {
		return fmt.Errorf("[%s] resolve control-plane configuration: %w", cfg.Module, err)
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
	if len(cfg.GRPCServices) > 0 && cfg.RegisterGRPC == nil {
		return fmt.Errorf("[%s] advertises gRPC services without registering an implementation", cfg.Module)
	}
	if cfg.CoreGRPCAddr != "" {
		if cfg.CoreGRPCTLS == nil {
			return fmt.Errorf("[%s] core gRPC requires an mTLS client configuration", cfg.Module)
		}
		if strings.TrimSpace(cfg.BootstrapCredential) == "" {
			return fmt.Errorf("[%s] core gRPC requires a module bootstrap credential", cfg.Module)
		}
	}
	if cfg.RegisterGRPC != nil || cfg.CoreGRPCAddr != "" {
		if cfg.GRPCServerTLS == nil {
			return fmt.Errorf("[%s] module gRPC requires an mTLS server configuration", cfg.Module)
		}
	}

	ready := cfg.Readiness
	var registered atomic.Bool
	registered.Store(cfg.CoreGRPCAddr == "")
	cfg.Readiness = func(ctx context.Context) error {
		if !registered.Load() {
			return errors.New("core registration is incomplete")
		}
		if ready != nil {
			return ready(ctx)
		}
		return nil
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
	if cfg.RegisterGRPC != nil || cfg.CoreGRPCAddr != "" {
		if cfg.GRPCListenAddr == "" {
			cfg.GRPCListenAddr = "0.0.0.0:9100"
		}
		var err error
		grpcServer, grpcErrCh, err = startGRPCServer(cfg)
		if err != nil {
			return err
		}
	}

	runtimeCtx, runtimeCancel := context.WithCancel(context.Background())
	defer runtimeCancel()
	if cfg.CoreGRPCAddr != "" {
		go func() {
			retryDelay := 250 * time.Millisecond
			for {
				err := registerWithCore(runtimeCtx, cfg)
				if err == nil {
					registered.Store(true)
					return
				}
				timer := time.NewTimer(retryDelay)
				select {
				case <-runtimeCtx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
				if retryDelay < 30*time.Second {
					retryDelay *= 2
					if retryDelay > 30*time.Second {
						retryDelay = 30 * time.Second
					}
				}
			}
		}()
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
	if cfg.GRPCServerTLS == nil || cfg.GRPCServerTLS.ClientAuth < tls.RequireAnyClientCert || cfg.GRPCServerTLS.ClientCAs == nil {
		return nil, nil, fmt.Errorf("[%s] module gRPC requires a client-authenticating TLS configuration", cfg.Module)
	}
	errCh := make(chan error, 1)
	log := xlog.Default().WithComponent("moduleserver.grpc")
	server := grpc.NewServer(
		grpc.Creds(credentials.NewTLS(cfg.GRPCServerTLS.Clone())),
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
		if err := server.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			errCh <- err
			return
		}
		errCh <- nil
	}()
	return server, errCh, nil
}

func registerWithCore(ctx context.Context, cfg Config) error {
	if cfg.CoreGRPCTLS == nil {
		return errors.New("core gRPC TLS configuration is required")
	}
	conn, err := grpc.NewClient(cfg.CoreGRPCAddr, grpc.WithTransportCredentials(credentials.NewTLS(cfg.CoreGRPCTLS.Clone())))
	if err != nil {
		return fmt.Errorf("dial core gRPC: %w", err)
	}
	defer conn.Close()
	client := corepb.NewModuleRegistryClient(conn)
	callCtx, callCancel := context.WithTimeout(ctx, 10*time.Second)
	defer callCancel()
	tokenResponse, err := corepb.NewInternalTokenServiceClient(conn).GetCurrent(callCtx, &corepb.GetCurrentRequest{
		Module: cfg.Module,
		BootstrapProof: &corepb.GetCurrentRequest_BootstrapSecret{
			BootstrapSecret: cfg.BootstrapCredential,
		},
	})
	if err != nil {
		return fmt.Errorf("exchange module credential: %w", err)
	}
	if strings.TrimSpace(tokenResponse.GetToken()) == "" {
		return errors.New("core returned an empty module token")
	}
	callCtx = metadata.AppendToOutgoingContext(callCtx, "x-aegion-internal-token", tokenResponse.GetToken())
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
		return fmt.Errorf("register with core: %w", err)
	}
	if !resp.GetSuccess() {
		return fmt.Errorf("core rejected registration: %s", resp.GetError())
	}
	return nil
}

// NewMutualTLSClientConfig builds a core-control-plane client configuration
// from mounted PEM files. The caller's certificate is mandatory.
func NewMutualTLSClientConfig(certFile, keyFile, caFile, serverName string) (*tls.Config, error) {
	certificate, roots, err := loadMutualTLSMaterial(certFile, keyFile, caFile)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		Certificates: []tls.Certificate{certificate},
		RootCAs:      roots,
		ServerName:   strings.TrimSpace(serverName),
		MinVersion:   tls.VersionTLS12,
	}, nil
}

// NewMutualTLSServerConfig builds a module gRPC server configuration. Every
// caller must present a certificate signed by the configured client CA.
func NewMutualTLSServerConfig(certFile, keyFile, clientCAFile string) (*tls.Config, error) {
	certificate, clientCAs, err := loadMutualTLSMaterial(certFile, keyFile, clientCAFile)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		Certificates: []tls.Certificate{certificate},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientCAs,
		MinVersion:   tls.VersionTLS12,
	}, nil
}

func loadMutualTLSMaterial(certFile, keyFile, caFile string) (tls.Certificate, *x509.CertPool, error) {
	if strings.TrimSpace(certFile) == "" || strings.TrimSpace(keyFile) == "" || strings.TrimSpace(caFile) == "" {
		return tls.Certificate{}, nil, errors.New("certificate, key, and CA files are required")
	}
	certificate, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("load TLS certificate: %w", err)
	}
	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("read TLS CA file: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return tls.Certificate{}, nil, errors.New("TLS CA file does not contain a certificate")
	}
	return certificate, pool, nil
}
func applyControlPlaneEnvironment(cfg *Config) error {
	if cfg == nil {
		return errors.New("module server configuration is required")
	}
	if strings.TrimSpace(cfg.CoreGRPCAddr) == "" {
		cfg.CoreGRPCAddr = strings.TrimSpace(os.Getenv("AEGION_CORE_GRPC_ADDR"))
	}
	if cfg.CoreGRPCAddr == "" {
		return nil
	}
	if strings.TrimSpace(cfg.BootstrapCredentialFile) == "" {
		cfg.BootstrapCredentialFile = strings.TrimSpace(os.Getenv("AEGION_MODULE_CREDENTIAL_FILE"))
	}
	if cfg.BootstrapCredential == "" && cfg.BootstrapCredentialFile != "" {
		credential, err := readCredentialFile(cfg.BootstrapCredentialFile)
		if err != nil {
			return err
		}
		cfg.BootstrapCredential = credential
	}

	certFile := strings.TrimSpace(os.Getenv("AEGION_MODULE_TLS_CERT_FILE"))
	keyFile := strings.TrimSpace(os.Getenv("AEGION_MODULE_TLS_KEY_FILE"))
	caFile := strings.TrimSpace(os.Getenv("AEGION_MODULE_CA_CERT_FILE"))
	if cfg.CoreGRPCTLS == nil && certFile != "" && keyFile != "" && caFile != "" {
		coreTLS, err := NewMutualTLSClientConfig(certFile, keyFile, caFile, os.Getenv("AEGION_CORE_GRPC_SERVER_NAME"))
		if err != nil {
			return err
		}
		cfg.CoreGRPCTLS = coreTLS
	}
	if cfg.GRPCServerTLS == nil && certFile != "" && keyFile != "" && caFile != "" {
		serverTLS, err := NewMutualTLSServerConfig(certFile, keyFile, caFile)
		if err != nil {
			return err
		}
		cfg.GRPCServerTLS = serverTLS
	}
	return nil
}

func readCredentialFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open module credential file: %w", err)
	}
	defer file.Close()
	const maxCredentialBytes = 4096
	value, err := io.ReadAll(io.LimitReader(file, maxCredentialBytes+1))
	if err != nil {
		return "", fmt.Errorf("read module credential file: %w", err)
	}
	if len(value) > maxCredentialBytes {
		return "", errors.New("module credential file exceeds maximum size")
	}
	credential := strings.TrimSpace(string(value))
	if credential == "" {
		return "", errors.New("module credential file is empty")
	}
	return credential, nil
}

// EnvOrDefault returns env value if set, otherwise fallback.
func EnvOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
