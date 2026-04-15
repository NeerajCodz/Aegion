package moduleserver

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var (
	signalNotify = signal.Notify
	signalStop   = signal.Stop
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
		log.Printf("[%s] listening on %s", cfg.Module, cfg.ListenAddr)
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
