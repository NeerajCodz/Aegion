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

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"

	"github.com/aegion/aegion/modules/admin/handler"
	"github.com/aegion/aegion/modules/admin/service"
	"github.com/aegion/aegion/modules/admin/store"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Config struct {
	Database struct {
		URL         string `yaml:"url"`
		MaxConns    int32  `yaml:"max_conns"`
		MinConns    int32  `yaml:"min_conns"`
		MaxIdleTime string `yaml:"max_idle_time"`
	} `yaml:"database"`
	Server struct {
		Address      string        `yaml:"address"`
		Port         int           `yaml:"port"`
		ReadTimeout  time.Duration `yaml:"read_timeout"`
		WriteTimeout time.Duration `yaml:"write_timeout"`
		IdleTimeout  time.Duration `yaml:"idle_timeout"`
	} `yaml:"server"`
	Admin struct {
		Enabled          bool          `yaml:"enabled"`
		BootstrapEnabled bool          `yaml:"bootstrap_enabled"`
		Path             string        `yaml:"path"`
		SessionLifespan  time.Duration `yaml:"session_lifespan"`
	} `yaml:"admin"`
	Core struct {
		ServiceURL string `yaml:"service_url"`
		APIKey     string `yaml:"api_key"`
	} `yaml:"core"`
	Log struct {
		Level  string `yaml:"level"`
		Format string `yaml:"format"`
	} `yaml:"log"`
}

func main() {
	// Parse command line flags
	var (
		configPath = flag.String("config", getEnv("AEGION_CONFIG_PATH", "admin.yaml"), "Configuration file path")
		version    = flag.Bool("version", false, "Show version")
		migrate    = flag.Bool("migrate", false, "Run migrations only")
	)
	flag.Parse()

	if *version {
		fmt.Println("Aegion Admin Module v1.0.0")
		return
	}

	// Load configuration
	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load configuration")
	}

	// Setup logger
	setupLogger(cfg.Log)

	log.Info().Str("config", *configPath).Msg("Starting Aegion Admin Module")

	// Context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Database connection
	dbConfig, err := pgxpool.ParseConfig(cfg.Database.URL)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to parse database URL")
	}

	dbConfig.MaxConns = cfg.Database.MaxConns
	dbConfig.MinConns = cfg.Database.MinConns
	if cfg.Database.MaxIdleTime != "" {
		duration, err := time.ParseDuration(cfg.Database.MaxIdleTime)
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to parse max_idle_time")
		}
		dbConfig.MaxConnIdleTime = duration
	}

	db, err := pgxpool.NewWithConfig(ctx, dbConfig)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to database")
	}
	defer db.Close()

	// Test database connection
	if err := db.Ping(ctx); err != nil {
		log.Fatal().Err(err).Msg("Failed to ping database")
	}
	log.Info().Msg("Database connected successfully")

	// Run migrations if requested
	if *migrate {
		if err := runMigrations(ctx, db); err != nil {
			log.Fatal().Err(err).Msg("Failed to run migrations")
		}
		log.Info().Msg("Migrations completed")
		return
	}

	// Initialize service layer
	adminStore := store.New(db)
	adminService := service.New(adminStore, service.Config{
		BootstrapEnabled: cfg.Admin.BootstrapEnabled,
	})
	adminHandler := handler.New(adminService)

	// Setup server
	server := &Server{
		Config:  cfg,
		DB:      db,
		Handler: adminHandler,
	}

	// Setup HTTP server
	httpServer := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Server.Address, cfg.Server.Port),
		Handler:      server.setupRouter(),
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	// Start server in goroutine
	go func() {
		log.Info().
			Str("address", httpServer.Addr).
			Msg("Starting HTTP server")

		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("Failed to start HTTP server")
		}
	}()

	// Register with core service
	if err := server.registerWithCore(ctx); err != nil {
		log.Error().Err(err).Msg("Failed to register with core service")
	}

	// Wait for interrupt signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	<-c
	log.Info().Msg("Shutting down gracefully...")

	// Shutdown with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("Server shutdown error")
	}

	log.Info().Msg("Server stopped")
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Expand environment variables
	expanded := os.ExpandEnv(string(data))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Set defaults
	if cfg.Server.Address == "" {
		cfg.Server.Address = "0.0.0.0"
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8082
	}
	if cfg.Server.ReadTimeout == 0 {
		cfg.Server.ReadTimeout = 15 * time.Second
	}
	if cfg.Server.WriteTimeout == 0 {
		cfg.Server.WriteTimeout = 15 * time.Second
	}
	if cfg.Server.IdleTimeout == 0 {
		cfg.Server.IdleTimeout = 60 * time.Second
	}
	if cfg.Admin.Path == "" {
		cfg.Admin.Path = "/admin"
	}
	if cfg.Admin.SessionLifespan == 0 {
		cfg.Admin.SessionLifespan = 4 * time.Hour
	}
	if cfg.Database.MaxConns == 0 {
		cfg.Database.MaxConns = 25
	}
	if cfg.Database.MinConns == 0 {
		cfg.Database.MinConns = 5
	}

	// Override with environment variables
	if dbURL := getEnv("DATABASE_URL", ""); dbURL != "" {
		cfg.Database.URL = dbURL
	}

	return &cfg, nil
}

func setupLogger(logConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}) {
	// Set log level
	level := zerolog.InfoLevel
	if logConfig.Level != "" {
		if l, err := zerolog.ParseLevel(logConfig.Level); err == nil {
			level = l
		}
	}
	zerolog.SetGlobalLevel(level)

	// Set log format
	if logConfig.Format == "pretty" || os.Getenv("AEGION_LOG_PRETTY") == "true" {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	}

	log.Info().Str("level", level.String()).Msg("Logger initialized")
}

func runMigrations(ctx context.Context, db *pgxpool.Pool) error {
	// TODO: Implement migration runner for admin module
	// This should read from the embedded migrations filesystem
	// and apply SQL migrations in order
	log.Info().Msg("Migration runner not yet implemented")
	return nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
