package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"

	platformconfig "github.com/aegion/aegion/internal/platform/config"
	adminmodule "github.com/aegion/aegion/modules/admin"
	"github.com/aegion/aegion/modules/admin/handler"
	"github.com/aegion/aegion/modules/admin/scim"
	"github.com/aegion/aegion/modules/admin/service"
	"github.com/aegion/aegion/modules/admin/store"
	socialservice "github.com/aegion/aegion/modules/social/service"
	socialstore "github.com/aegion/aegion/modules/social/store"
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
		TLS          struct {
			Enabled      bool   `yaml:"enabled"`
			CertFile     string `yaml:"cert_file"`
			KeyFile      string `yaml:"key_file"`
			ClientCAFile string `yaml:"client_ca_file"`
			MinVersion   string `yaml:"min_version"`
		} `yaml:"tls"`
	} `yaml:"server"`
	Admin struct {
		Enabled          bool          `yaml:"enabled"`
		BootstrapEnabled bool          `yaml:"bootstrap_enabled"`
		Path             string        `yaml:"path"`
		SessionLifespan  time.Duration `yaml:"session_lifespan"`
		DefaultPageSize  int           `yaml:"default_page_size"`
		MaxPageSize      int           `yaml:"max_page_size"`
		APIKeyPrefix     string        `yaml:"api_key_prefix"`
		APIKeyPrefixLen  int           `yaml:"api_key_lookup_prefix_len"`
		APIKeyEntropy    int           `yaml:"api_key_entropy_bytes"`
		SCIM             struct {
			Enabled                    bool          `yaml:"enabled"`
			BasePath                   string        `yaml:"base_path"`
			TokenPrefix                string        `yaml:"token_prefix"`
			TokenLookupPrefixLen       int           `yaml:"token_lookup_prefix_len"`
			TokenEntropyBytes          int           `yaml:"token_entropy_bytes"`
			DefaultPageSize            int           `yaml:"default_page_size"`
			MaxPageSize                int           `yaml:"max_page_size"`
			TokenLastUsedUpdateTimeout time.Duration `yaml:"token_last_used_update_timeout"`
		} `yaml:"scim"`
	} `yaml:"admin"`
	Core struct {
		ServiceURL string `yaml:"service_url"`
		APIKey     string `yaml:"api_key"`
	} `yaml:"core"`
	Secrets struct {
		Cipher []string `yaml:"cipher"`
	} `yaml:"secrets"`
	Observability struct {
		Enabled      bool          `yaml:"enabled"`
		ProbeTimeout time.Duration `yaml:"probe_timeout"`
		Endpoints    struct {
			OTelCollector string `yaml:"otel_collector"`
			Prometheus    string `yaml:"prometheus"`
			Grafana       string `yaml:"grafana"`
			Tempo         string `yaml:"tempo"`
			Loki          string `yaml:"loki"`
		} `yaml:"endpoints"`
	} `yaml:"observability"`
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
	fs.StringVar(&f.configPath, "config", envLookup("AEGION_CONFIG_PATH", "aegion.yaml"), "Configuration file path")
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
	var socialProviders handler.SocialProviderManager
	if len(cfg.Secrets.Cipher) > 0 && strings.TrimSpace(cfg.Secrets.Cipher[0]) != "" {
		sum := sha256.Sum256([]byte(strings.TrimSpace(cfg.Secrets.Cipher[0])))
		socialRepo, err := socialstore.NewPostgres(db, sum[:])
		if err != nil {
			return nil, fmt.Errorf("initialize social provider manager: %w", err)
		}
		socialProviders = socialservice.New(socialRepo)
	}
	adminHandler := handler.New(adminService, handler.HandlerConfig{
		SessionTokenExpiry: cfg.Admin.SessionLifespan,
		DefaultPageSize:    cfg.Admin.DefaultPageSize,
		MaxPageSize:        cfg.Admin.MaxPageSize,
		APIKeyPrefix:       cfg.Admin.APIKeyPrefix,
		APIKeyPrefixLen:    cfg.Admin.APIKeyPrefixLen,
		APIKeyEntropyBytes: cfg.Admin.APIKeyEntropy,
		SocialProviders:    socialProviders,
	})
	var scimService *scim.Service
	var scimHandler *scim.Handler
	if cfg.Admin.SCIM.Enabled {
		scimService = scim.NewService(adminStore, nil, scim.Config{
			BasePath:                   cfg.Admin.SCIM.BasePath,
			TokenPrefix:                cfg.Admin.SCIM.TokenPrefix,
			TokenLookupPrefixLen:       cfg.Admin.SCIM.TokenLookupPrefixLen,
			TokenEntropyBytes:          cfg.Admin.SCIM.TokenEntropyBytes,
			DefaultPageSize:            cfg.Admin.SCIM.DefaultPageSize,
			MaxPageSize:                cfg.Admin.SCIM.MaxPageSize,
			TokenLastUsedUpdateTimeout: cfg.Admin.SCIM.TokenLastUsedUpdateTimeout,
		})
		scimHandler = scim.NewHandler(scimService, scim.HandlerConfig{
			DefaultPageSize: cfg.Admin.SCIM.DefaultPageSize,
			MaxPageSize:     cfg.Admin.SCIM.MaxPageSize,
		})
	}

	server := &Server{
		Config:      cfg,
		DB:          db,
		Handler:     adminHandler,
		SCIMService: scimService,
		SCIMHandler: scimHandler,
	}

	httpServer := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Server.Address, cfg.Server.Port),
		Handler:      server.setupRouter(),
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}
	if cfg.Server.TLS.Enabled {
		tlsConfig, err := buildTLSConfig(cfg)
		if err != nil {
			return nil, err
		}
		httpServer.TLSConfig = tlsConfig
	}

	go func() {
		log.Info().
			Str("address", httpServer.Addr).
			Msg("Starting HTTP server")

		var err error
		if cfg.Server.TLS.Enabled {
			err = httpServer.ListenAndServeTLS(cfg.Server.TLS.CertFile, cfg.Server.TLS.KeyFile)
		} else {
			err = httpServer.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("Failed to start HTTP server")
		}
	}()

	return &liveRuntimeServer{
		server:     server,
		httpServer: httpServer,
	}, nil
}

func loadConfig(path string) (*Config, error) {
	// #nosec G304 -- Config path is provided by trusted operator/CLI input.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Expand environment variables
	expanded := os.ExpandEnv(string(data))

	var cfg Config
	if isAegionSuperConfig([]byte(expanded)) {
		superCfg, err := platformconfig.Load(path)
		if err != nil {
			return nil, fmt.Errorf("failed to parse config: %w", err)
		}
		cfg = mapPlatformConfig(superCfg)
	} else {
		if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
			return nil, fmt.Errorf("failed to parse config: %w", err)
		}
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
	if cfg.Admin.DefaultPageSize == 0 {
		cfg.Admin.DefaultPageSize = 20
	}
	if cfg.Admin.MaxPageSize == 0 {
		cfg.Admin.MaxPageSize = 100
	}
	if cfg.Admin.APIKeyPrefix == "" {
		cfg.Admin.APIKeyPrefix = "aegion_"
	}
	if cfg.Admin.APIKeyPrefixLen == 0 {
		cfg.Admin.APIKeyPrefixLen = 12
	}
	if cfg.Admin.APIKeyEntropy == 0 {
		cfg.Admin.APIKeyEntropy = 32
	}
	if cfg.Admin.SCIM.BasePath == "" {
		cfg.Admin.SCIM.BasePath = "/scim/v2"
	}
	if cfg.Admin.SCIM.TokenPrefix == "" {
		cfg.Admin.SCIM.TokenPrefix = "aegion_scim_"
	}
	if cfg.Admin.SCIM.TokenLookupPrefixLen == 0 {
		cfg.Admin.SCIM.TokenLookupPrefixLen = 12
	}
	if cfg.Admin.SCIM.TokenEntropyBytes == 0 {
		cfg.Admin.SCIM.TokenEntropyBytes = 32
	}
	if cfg.Admin.SCIM.DefaultPageSize == 0 {
		cfg.Admin.SCIM.DefaultPageSize = 20
	}
	if cfg.Admin.SCIM.MaxPageSize == 0 {
		cfg.Admin.SCIM.MaxPageSize = 1000
	}
	if cfg.Admin.SCIM.TokenLastUsedUpdateTimeout == 0 {
		cfg.Admin.SCIM.TokenLastUsedUpdateTimeout = 2 * time.Second
	}
	if cfg.Database.MaxConns == 0 {
		cfg.Database.MaxConns = 25
	}
	if cfg.Database.MinConns == 0 {
		cfg.Database.MinConns = 5
	}
	if cfg.Observability.ProbeTimeout == 0 {
		cfg.Observability.ProbeTimeout = 5 * time.Second
	}
	if cfg.Observability.Endpoints.OTelCollector == "" {
		cfg.Observability.Endpoints.OTelCollector = "http://otel-collector:13133"
	}
	if cfg.Observability.Endpoints.Prometheus == "" {
		cfg.Observability.Endpoints.Prometheus = "http://prometheus:9090/-/healthy"
	}
	if cfg.Observability.Endpoints.Grafana == "" {
		cfg.Observability.Endpoints.Grafana = "http://grafana:3000/api/health"
	}
	if cfg.Observability.Endpoints.Tempo == "" {
		cfg.Observability.Endpoints.Tempo = "http://tempo:3200/ready"
	}
	if cfg.Observability.Endpoints.Loki == "" {
		cfg.Observability.Endpoints.Loki = "http://loki:3100/ready"
	}

	// Override with environment variables
	if dbURL := getEnv("DATABASE_URL", ""); dbURL != "" {
		cfg.Database.URL = dbURL
	}
	cfg.Observability.Enabled = getEnvBool("AEGION_ADMIN_OBSERVABILITY_ENABLED", cfg.Observability.Enabled)
	cfg.Observability.ProbeTimeout = getEnvDuration("AEGION_ADMIN_OBSERVABILITY_PROBE_TIMEOUT", cfg.Observability.ProbeTimeout)
	if value := getEnv("AEGION_ADMIN_OBS_OTEL_COLLECTOR_URL", ""); value != "" {
		cfg.Observability.Endpoints.OTelCollector = value
	}
	if value := getEnv("AEGION_ADMIN_OBS_PROMETHEUS_URL", ""); value != "" {
		cfg.Observability.Endpoints.Prometheus = value
	}
	if value := getEnv("AEGION_ADMIN_OBS_GRAFANA_URL", ""); value != "" {
		cfg.Observability.Endpoints.Grafana = value
	}
	if value := getEnv("AEGION_ADMIN_OBS_TEMPO_URL", ""); value != "" {
		cfg.Observability.Endpoints.Tempo = value
	}
	if value := getEnv("AEGION_ADMIN_OBS_LOKI_URL", ""); value != "" {
		cfg.Observability.Endpoints.Loki = value
	}

	return &cfg, nil
}

func isAegionSuperConfig(data []byte) bool {
	var root map[string]interface{}
	if err := yaml.Unmarshal(data, &root); err != nil {
		return false
	}

	if _, ok := root["module_versions"]; ok {
		return true
	}
	if _, ok := root["secrets"]; ok {
		return true
	}
	if _, ok := root["sessions"]; ok {
		return true
	}
	if _, ok := root["password"]; ok {
		return true
	}
	if _, ok := root["magic_link"]; ok {
		return true
	}

	return false
}

func mapPlatformConfig(superCfg *platformconfig.Config) Config {
	var cfg Config

	cfg.Database.URL = superCfg.Database.URL
	cfg.Database.MaxConns = safeInt32(superCfg.Database.MaxOpenConns)
	cfg.Database.MinConns = safeInt32(superCfg.Database.MaxIdleConns)
	cfg.Database.MaxIdleTime = superCfg.Database.ConnMaxIdleTime.Duration().String()

	cfg.Server.Address = superCfg.Server.Host
	cfg.Server.Port = superCfg.Server.Port
	cfg.Server.ReadTimeout = superCfg.Server.ReadTimeout.Duration()
	cfg.Server.WriteTimeout = superCfg.Server.WriteTimeout.Duration()
	cfg.Server.IdleTimeout = superCfg.Server.IdleTimeout.Duration()
	cfg.Server.TLS.Enabled = superCfg.Server.TLS.Enabled
	cfg.Server.TLS.CertFile = superCfg.Server.TLS.CertFile
	cfg.Server.TLS.KeyFile = superCfg.Server.TLS.KeyFile
	cfg.Server.TLS.ClientCAFile = superCfg.Server.TLS.ClientCAFile
	cfg.Server.TLS.MinVersion = superCfg.Server.TLS.MinVersion

	cfg.Admin.Enabled = superCfg.Admin.Enabled
	cfg.Admin.Path = superCfg.Admin.Path
	cfg.Admin.SessionLifespan = superCfg.Admin.SessionLifespan.Duration()
	cfg.Admin.DefaultPageSize = superCfg.Admin.DefaultPageSize
	cfg.Admin.MaxPageSize = superCfg.Admin.MaxPageSize
	cfg.Admin.APIKeyPrefix = superCfg.Admin.APIKeyPrefix
	cfg.Admin.APIKeyPrefixLen = superCfg.Admin.APIKeyLookupPrefixLen
	cfg.Admin.APIKeyEntropy = superCfg.Admin.APIKeyEntropyBytes
	cfg.Admin.SCIM.Enabled = superCfg.Admin.SCIM.Enabled
	cfg.Admin.SCIM.BasePath = superCfg.Admin.SCIM.BasePath
	cfg.Admin.SCIM.TokenPrefix = superCfg.Admin.SCIM.TokenPrefix
	cfg.Admin.SCIM.TokenLookupPrefixLen = superCfg.Admin.SCIM.TokenLookupPrefixLen
	cfg.Admin.SCIM.TokenEntropyBytes = superCfg.Admin.SCIM.TokenEntropyBytes
	cfg.Admin.SCIM.DefaultPageSize = superCfg.Admin.SCIM.DefaultPageSize
	cfg.Admin.SCIM.MaxPageSize = superCfg.Admin.SCIM.MaxPageSize
	cfg.Admin.SCIM.TokenLastUsedUpdateTimeout = superCfg.Admin.SCIM.TokenLastUsedUpdateTimeout.Duration()
	cfg.Secrets.Cipher = append([]string(nil), superCfg.Secrets.Cipher...)

	cfg.Log.Level = superCfg.Log.Level
	cfg.Log.Format = superCfg.Log.Format

	return cfg
}

func safeInt32(value int) int32 {
	const maxInt32 = int(^uint32(0) >> 1)
	const minInt32 = -maxInt32 - 1
	switch {
	case value > maxInt32:
		return int32(maxInt32)
	case value < minInt32:
		return int32(minInt32)
	default:
		return int32(value)
	}
}

func buildTLSConfig(cfg *Config) (*tls.Config, error) {
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}

	switch strings.TrimSpace(cfg.Server.TLS.MinVersion) {
	case "", "1.2":
		tlsConfig.MinVersion = tls.VersionTLS12
	case "1.3":
		tlsConfig.MinVersion = tls.VersionTLS13
	default:
		return nil, fmt.Errorf("unsupported tls min_version %q", cfg.Server.TLS.MinVersion)
	}

	if strings.TrimSpace(cfg.Server.TLS.ClientCAFile) == "" {
		return tlsConfig, nil
	}

	caPEM, err := os.ReadFile(cfg.Server.TLS.ClientCAFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read tls client CA file: %w", err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("failed to parse tls client CA file")
	}

	tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
	tlsConfig.ClientCAs = pool
	return tlsConfig, nil
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

type adminMigrationDB interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Begin(ctx context.Context) (pgx.Tx, error)
}

func runMigrations(ctx context.Context, db *pgxpool.Pool) error {
	return runMigrationsWithFS(ctx, db, adminmodule.GetMigrationFiles())
}

func runMigrationsWithFS(ctx context.Context, db adminMigrationDB, migrationFS fs.FS) error {
	if db == nil {
		return errors.New("database pool is nil")
	}
	if pool, ok := db.(*pgxpool.Pool); ok && pool == nil {
		return errors.New("database pool is nil")
	}

	migrations, err := loadAdminMigrations(migrationFS)
	if err != nil {
		return fmt.Errorf("failed to load admin migrations: %w", err)
	}
	if len(migrations) == 0 {
		log.Info().Msg("No admin migrations found")
		return nil
	}

	const advisoryLockID int64 = 9021045311782301
	var lockAcquired bool
	if err := db.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", advisoryLockID).Scan(&lockAcquired); err != nil {
		return fmt.Errorf("failed to acquire admin migration lock: %w", err)
	}
	if !lockAcquired {
		return errors.New("another admin migration is in progress")
	}
	defer func() {
		_, _ = db.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", advisoryLockID)
	}()

	if err := ensureAdminMigrationsTable(ctx, db); err != nil {
		return fmt.Errorf("failed to ensure admin migration table: %w", err)
	}

	applied, err := loadAppliedAdminMigrations(ctx, db)
	if err != nil {
		return fmt.Errorf("failed to load applied admin migrations: %w", err)
	}

	for _, migration := range migrations {
		if _, exists := applied[migration.Version]; exists {
			continue
		}
		if err := applyAdminMigration(ctx, db, migration); err != nil {
			return err
		}
		log.Info().
			Int("version", migration.Version).
			Str("name", migration.Name).
			Msg("Applied admin migration")
	}

	return nil
}

type adminMigration struct {
	Version int
	Name    string
	UpSQL   string
}

func loadAdminMigrations(fsys fs.FS) ([]adminMigration, error) {
	entries, err := fs.ReadDir(fsys, "migrations")
	if err != nil {
		return nil, err
	}

	migrations := make([]adminMigration, 0, len(entries))
	seen := make(map[int]struct{})

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		filename := entry.Name()
		if !strings.HasSuffix(filename, ".up.sql") {
			continue
		}

		parts := strings.SplitN(filename, "_", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid migration filename: %s", filename)
		}

		version, err := strconv.Atoi(parts[0])
		if err != nil {
			return nil, fmt.Errorf("invalid migration version in %s: %w", filename, err)
		}
		if _, exists := seen[version]; exists {
			return nil, fmt.Errorf("duplicate migration version: %d", version)
		}
		seen[version] = struct{}{}

		content, err := fs.ReadFile(fsys, "migrations/"+filename)
		if err != nil {
			return nil, fmt.Errorf("failed to read migration %s: %w", filename, err)
		}

		migrations = append(migrations, adminMigration{
			Version: version,
			Name:    strings.TrimSuffix(parts[1], ".up.sql"),
			UpSQL:   string(content),
		})
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})

	return migrations, nil
}

func ensureAdminMigrationsTable(ctx context.Context, db adminMigrationDB) error {
	_, err := db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS adm_schema_migrations (
			version    INT PRIMARY KEY,
			name       TEXT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	return err
}

func loadAppliedAdminMigrations(ctx context.Context, db adminMigrationDB) (map[int]struct{}, error) {
	rows, err := db.Query(ctx, `SELECT version FROM adm_schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := make(map[int]struct{})
	for rows.Next() {
		var version int
		if scanErr := rows.Scan(&version); scanErr != nil {
			return nil, scanErr
		}
		applied[version] = struct{}{}
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return applied, nil
}

func applyAdminMigration(ctx context.Context, db adminMigrationDB, migration adminMigration) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			_ = rollbackErr
		}
	}()

	if _, err := tx.Exec(ctx, migration.UpSQL); err != nil {
		return fmt.Errorf("failed to apply admin migration %04d_%s: %w", migration.Version, migration.Name, err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO adm_schema_migrations (version, name)
		VALUES ($1, $2)
	`, migration.Version, migration.Name); err != nil {
		return fmt.Errorf("failed to record admin migration %04d_%s: %w", migration.Version, migration.Name, err)
	}

	return tx.Commit(ctx)
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return defaultValue
	}

	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return defaultValue
	}

	return parsed
}

func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return defaultValue
	}

	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return defaultValue
	}

	return parsed
}
