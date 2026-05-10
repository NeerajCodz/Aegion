package moduleserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"
	"github.com/aegion/aegion/internal/platform/logger"
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

	// Initialize standardized logger
	logger.New(logger.Config{
		Level:            os.Getenv("AEGION_LOG_LEVEL"),
		Format:           os.Getenv("AEGION_LOG_FORMAT"),
		ServiceName:      cfg.Module,
		ServiceNamespace: os.Getenv("AEGION_LOG_NAMESPACE"),
		Environment:      os.Getenv("AEGION_ENV"),
		CloudRegion:      os.Getenv("AEGION_CLOUD_REGION"),
		Developer:        os.Getenv("DEV_NAME"),
		Version:          cfg.Version,
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

	errCh := make(chan error, 1)
	go func() {
		slog.Info("module server listening",
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
	case <-stop:
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}

// EnvOrDefault returns env value if set, otherwise fallback.
func EnvOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
