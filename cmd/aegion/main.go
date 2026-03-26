// Package main is the entry point for the Aegion identity platform.
package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aegion/aegion/core/workers"
	"github.com/aegion/aegion/internal/platform/config"
	"github.com/aegion/aegion/internal/platform/database"
	"github.com/aegion/aegion/internal/platform/logger"
)

//go:embed migrations/*.sql
var migrations embed.FS

var (
	version   = "dev"
	buildTime = "unknown"
)

// Command line flags
type flags struct {
	configPath     string
	migrateOnly    bool
	showVersion    bool
	adminBootstrap bool
	enableWorkers  bool
}

func parseFlags() *flags {
	f := &flags{}
	flag.StringVar(&f.configPath, "config", "aegion.yaml", "Path to configuration file")
	flag.BoolVar(&f.migrateOnly, "migrate", false, "Run migrations and exit")
	flag.BoolVar(&f.showVersion, "version", false, "Show version and exit")
	flag.BoolVar(&f.adminBootstrap, "admin-bootstrap", false, "Bootstrap admin user on startup")
	flag.BoolVar(&f.enableWorkers, "workers", true, "Enable background workers")
	flag.Parse()
	return f
}

func main() {
	f := parseFlags()

	if f.showVersion {
		fmt.Printf("Aegion %s (built %s)\n", version, buildTime)
		os.Exit(0)
	}

	// Load configuration
	cfg, err := config.Load(f.configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "Invalid configuration: %v\n", err)
		os.Exit(1)
	}

	// Initialize logger
	log := logger.New(logger.Config{
		Level:  cfg.Log.Level,
		Format: cfg.Log.Format,
	})

	log.Info().
		Str("version", version).
		Str("config", f.configPath).
		Bool("workers", f.enableWorkers).
		Bool("admin_bootstrap", f.adminBootstrap).
		Msg("Starting Aegion")

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Connect to database
	db, err := database.Connect(ctx, database.Config{
		URL:             cfg.Database.URL,
		MaxOpenConns:    int32(cfg.Database.MaxOpenConns),
		MaxIdleConns:    int32(cfg.Database.MaxIdleConns),
		ConnMaxLifetime: cfg.Database.ConnMaxLifetime.Duration(),
		ConnMaxIdleTime: cfg.Database.ConnMaxIdleTime.Duration(),
	})
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to database")
	}
	defer db.Close()
	log.Info().Msg("Connected to database")

	// Run migrations
	migrator := database.NewMigrator(db, migrations, "migrations")
	if err := migrator.Migrate(ctx); err != nil {
		log.Fatal().Err(err).Msg("Failed to run migrations")
	}
	log.Info().Msg("Migrations complete")

	if f.migrateOnly || cfg.Database.MigrateOnly {
		log.Info().Msg("Migrate-only mode, exiting")
		return
	}

	// Initialize worker manager
	var workerMgr *workers.Manager
	if f.enableWorkers {
		workerMgr = workers.NewManager(workers.ManagerConfig{
			DB:  db.Pool,
			Log: log,
		})
		log.Info().Msg("Worker manager initialized")
	}

	// Initialize server with worker manager
	server, err := NewServer(ctx, &ServerConfig{
		Config:         cfg,
		DB:             db,
		Log:            log,
		WorkerManager:  workerMgr,
		AdminBootstrap: f.adminBootstrap,
	})
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize server")
	}

	// Start workers if enabled
	if workerMgr != nil {
		workerMgr.Start(ctx)
		log.Info().Msg("Background workers started")
	}

	// Start HTTP server
	httpServer := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:      server.Handler(),
		ReadTimeout:  cfg.Server.ReadTimeout.Duration(),
		WriteTimeout: cfg.Server.WriteTimeout.Duration(),
		IdleTimeout:  cfg.Server.IdleTimeout.Duration(),
	}

	// Start server in goroutine
	go func() {
		log.Info().
			Str("addr", httpServer.Addr).
			Msg("HTTP server listening")

		var err error
		if cfg.Server.TLS.Enabled {
			err = httpServer.ListenAndServeTLS(cfg.Server.TLS.CertFile, cfg.Server.TLS.KeyFile)
		} else {
			err = httpServer.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("HTTP server failed")
		}
	}()

	// Setup lifecycle manager
	lifecycle := NewLifecycle(&LifecycleConfig{
		Log:           log,
		Server:        server,
		HTTPServer:    httpServer,
		WorkerManager: workerMgr,
	})

	// Wait for interrupt signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh

	log.Info().Str("signal", sig.String()).Msg("Shutdown signal received")

	// Graceful shutdown with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := lifecycle.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("Error during shutdown")
		os.Exit(1)
	}

	log.Info().Msg("Shutdown complete")
}
