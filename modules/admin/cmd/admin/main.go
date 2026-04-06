package main

import (
	"context"
	"flag"
	"fmt"
	"io"
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

type LogConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

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
	Log LogConfig `yaml:"log"`
}

type mainFlags struct {
	configPath string
	version    bool
	migrate    bool
}

type runtimeServer interface {
	registerWithCore(ctx context.Context) error
	shutdown(ctx context.Context) error
}

type liveRuntimeServer struct {
	server     *Server
	httpServer *http.Server
}

func (s *liveRuntimeServer) registerWithCore(ctx context.Context) error {
	return s.server.registerWithCore(ctx)
}

func (s *liveRuntimeServer) shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

type mainDeps struct {
	stdout         io.Writer
	loadConfig     func(path string) (*Config, error)
	setupLogger    func(logConfig LogConfig)
	parseDBConfig  func(connString string) (*pgxpool.Config, error)
	newDBPool      func(ctx context.Context, config *pgxpool.Config) (*pgxpool.Pool, error)
	pingDB         func(ctx context.Context, db *pgxpool.Pool) error
	closeDB        func(db *pgxpool.Pool)
	runMigrations  func(ctx context.Context, db *pgxpool.Pool) error
	startServer    func(cfg *Config, db *pgxpool.Pool) (runtimeServer, error)
	newSignalChan  func() chan os.Signal
	notifySignals  func(c chan<- os.Signal, sig ...os.Signal)
	stopSignalChan func(c chan<- os.Signal)
}

func defaultMainDeps() mainDeps {
	return mainDeps{
		stdout:        os.Stdout,
		loadConfig:    loadConfig,
		setupLogger:   setupLogger,
		parseDBConfig: pgxpool.ParseConfig,
		newDBPool:     pgxpool.NewWithConfig,
		pingDB: func(ctx context.Context, db *pgxpool.Pool) error {
			return db.Ping(ctx)
		},
		closeDB: func(db *pgxpool.Pool) {
			if db != nil {
				db.Close()
			}
		},
		runMigrations: runMigrations,
		startServer:   startServerRuntime,
		newSignalChan: func() chan os.Signal {
			return make(chan os.Signal, 1)
		},
		notifySignals:  signal.Notify,
		stopSignalChan: signal.Stop,
	}
}

func main() {
	if err := run(os.Args[1:], defaultMainDeps()); err != nil {
		log.Fatal().Err(err).Msg("Admin module startup failed")
	}
}

func parseMainFlags(args []string, envLookup func(string, string) string) (*mainFlags, error) {
	fs := flag.NewFlagSet("aegion-admin", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	f := &mainFlags{}
	fs.StringVar(&f.configPath, "config", envLookup("AEGION_CONFIG_PATH", "admin.yaml"), "Configuration file path")
	fs.BoolVar(&f.version, "version", false, "Show version")
	fs.BoolVar(&f.migrate, "migrate", false, "Run migrations only")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	return f, nil
}

func run(args []string, deps mainDeps) error {
	flags, err := parseMainFlags(args, getEnv)
	if err != nil {
		return fmt.Errorf("failed to parse flags: %w", err)
	}

	if flags.version {
		_, _ = fmt.Fprintln(deps.stdout, "Aegion Admin Module v1.0.0")
		return nil
	}

	cfg, err := deps.loadConfig(flags.configPath)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	deps.setupLogger(cfg.Log)
	log.Info().Str("config", flags.configPath).Msg("Starting Aegion Admin Module")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dbConfig, err := deps.parseDBConfig(cfg.Database.URL)
	if err != nil {
		return fmt.Errorf("failed to parse database URL: %w", err)
	}

	dbConfig.MaxConns = cfg.Database.MaxConns
	dbConfig.MinConns = cfg.Database.MinConns
	if cfg.Database.MaxIdleTime != "" {
		duration, err := time.ParseDuration(cfg.Database.MaxIdleTime)
		if err != nil {
			return fmt.Errorf("failed to parse max_idle_time: %w", err)
		}
		dbConfig.MaxConnIdleTime = duration
	}

	db, err := deps.newDBPool(ctx, dbConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer deps.closeDB(db)

	if err := deps.pingDB(ctx, db); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}
	log.Info().Msg("Database connected successfully")

	if flags.migrate {
		if err := deps.runMigrations(ctx, db); err != nil {
			return fmt.Errorf("failed to run migrations: %w", err)
		}
		log.Info().Msg("Migrations completed")
		return nil
	}

	serverRuntime, err := deps.startServer(cfg, db)
	if err != nil {
		return fmt.Errorf("failed to initialize server: %w", err)
	}

	if err := serverRuntime.registerWithCore(ctx); err != nil {
		log.Error().Err(err).Msg("Failed to register with core service")
	}

	sigCh := deps.newSignalChan()
	deps.notifySignals(sigCh, os.Interrupt, syscall.SIGTERM)
	defer deps.stopSignalChan(sigCh)

	sig := <-sigCh
	log.Info().Str("signal", sig.String()).Msg("Shutting down gracefully...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := serverRuntime.shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server shutdown error: %w", err)
	}

	log.Info().Msg("Server stopped")
	return nil
}

func startServerRuntime(cfg *Config, db *pgxpool.Pool) (runtimeServer, error) {
	adminStore := store.New(db)
	adminService := service.New(adminStore, service.Config{
		BootstrapEnabled: cfg.Admin.BootstrapEnabled,
	})
	adminHandler := handler.New(adminService, handler.DefaultHandlerConfig())

	server := &Server{
		Config:  cfg,
		DB:      db,
		Handler: adminHandler,
	}

	httpServer := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Server.Address, cfg.Server.Port),
		Handler:      server.setupRouter(),
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	go func() {
		log.Info().
			Str("address", httpServer.Addr).
			Msg("Starting HTTP server")

		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("Failed to start HTTP server")
		}
	}()

	return &liveRuntimeServer{
		server:     server,
		httpServer: httpServer,
	}, nil
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

func setupLogger(logConfig LogConfig) {
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
