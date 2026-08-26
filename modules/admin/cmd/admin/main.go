package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"gopkg.in/yaml.v3"

	platformconfig "github.com/aegion/aegion/internal/platform/config"
	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"
	"github.com/aegion/aegion/internal/platform/moduleserver"
	platformobservability "github.com/aegion/aegion/internal/platform/observability"
	"github.com/aegion/aegion/internal/xlog"
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
	// Core lifecycle configuration is supplied exclusively through
	// internal/platform/moduleserver's environment/file contract.
	Secrets struct {
		Cipher []string `yaml:"cipher"`
	} `yaml:"secrets"`
	Observability struct {
		Enabled      bool          `yaml:"enabled"`
		ProbeTimeout time.Duration `yaml:"probe_timeout"`
		Endpoints    struct {
			LozaCollector string `yaml:"loza_collector"`
			Prometheus    string `yaml:"prometheus"`
			Grafana       string `yaml:"grafana"`
			Tempo         string `yaml:"tempo"`
		} `yaml:"endpoints"`
		Telemetry struct {
			ServiceName          string        `yaml:"service_name"`
			ServiceVersion       string        `yaml:"service_version"`
			Environment          string        `yaml:"environment"`
			InstanceID           string        `yaml:"instance_id"`
			TracesEndpoint       string        `yaml:"traces_endpoint"`
			MetricsEndpoint      string        `yaml:"metrics_endpoint"`
			TraceSamplingRatio   float64       `yaml:"trace_sampling_ratio"`
			MetricExportInterval time.Duration `yaml:"metric_export_interval"`
			TraceExportTimeout   time.Duration `yaml:"trace_export_timeout"`
			Insecure             bool          `yaml:"insecure"`
			EnableTraces         bool          `yaml:"enable_traces"`
			EnableMetrics        bool          `yaml:"enable_metrics"`
		} `yaml:"telemetry"`
	} `yaml:"observability"`
	Log LogConfig `yaml:"log"`
}

const (
	adminModuleVersion     = "1.0.0"
	adminPublicRoutePrefix = "/aegion"
)

type mainFlags struct {
	configPath string
	version    bool
	migrate    bool
}

type moduleRuntime struct {
	registerHTTPRoutes func(*http.ServeMux)
	readiness          func(context.Context) error
	cleanup            func()
}

type mainDeps struct {
	stdout          io.Writer
	loadConfig      func(path string) (*Config, error)
	cryptoSelfCheck func() error
	setupLogger     func(logConfig LogConfig)
	parseDBConfig   func(connString string) (*pgxpool.Config, error)
	newDBPool       func(ctx context.Context, config *pgxpool.Config) (*pgxpool.Pool, error)
	pingDB          func(ctx context.Context, db *pgxpool.Pool) error
	closeDB         func(db *pgxpool.Pool)
	runMigrations   func(ctx context.Context, db *pgxpool.Pool) error
	buildRuntime    func(cfg *Config, db *pgxpool.Pool) (*moduleRuntime, error)
	runModuleServer func(moduleserver.Config) error
}

func defaultMainDeps() mainDeps {
	return mainDeps{
		stdout:          os.Stdout,
		loadConfig:      loadConfig,
		cryptoSelfCheck: platformcrypto.RuntimeSelfCheck,
		setupLogger:     setupLogger,
		parseDBConfig:   pgxpool.ParseConfig,
		newDBPool:       pgxpool.NewWithConfig,
		pingDB: func(ctx context.Context, db *pgxpool.Pool) error {
			return db.Ping(ctx)
		},
		closeDB: func(db *pgxpool.Pool) {
			if db != nil {
				db.Close()
			}
		},
		runMigrations:   runMigrations,
		buildRuntime:    buildRuntime,
		runModuleServer: moduleserver.Run,
	}
}

func main() {
	if err := run(os.Args[1:], defaultMainDeps()); err != nil {
		xlog.Default().Error("Admin module startup failed", "error", err)
		os.Exit(1)
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
	if deps.cryptoSelfCheck == nil {
		deps.cryptoSelfCheck = platformcrypto.RuntimeSelfCheck
	}

	flags, err := parseMainFlags(args, getEnv)
	if err != nil {
		return fmt.Errorf("failed to parse flags: %w", err)
	}

	if flags.version {
		_, _ = fmt.Fprintln(deps.stdout, "Aegion Admin Module v1.0.0")
		return nil
	}
	if err := deps.cryptoSelfCheck(); err != nil {
		return fmt.Errorf("failed crypto runtime self-check: %w", err)
	}

	cfg, err := deps.loadConfig(flags.configPath)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	deps.setupLogger(cfg.Log)
	xlog.Default().Info("Starting Aegion Admin Module", "config", flags.configPath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if strings.TrimSpace(cfg.Database.URL) == "" {
		return errors.New("admin database URL is required")
	}

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
	xlog.Default().Info("Database connected successfully")

	if flags.migrate {
		if err := deps.runMigrations(ctx, db); err != nil {
			return fmt.Errorf("failed to run migrations: %w", err)
		}
		xlog.Default().Info("Migrations completed")
		return nil
	}

	runtime, err := deps.buildRuntime(cfg, db)
	if err != nil {
		return fmt.Errorf("failed to initialize admin runtime: %w", err)
	}
	if runtime.cleanup != nil {
		defer runtime.cleanup()
	}

	if err := deps.runModuleServer(adminModuleConfig(cfg, runtime)); err != nil {
		return fmt.Errorf("admin module server: %w", err)
	}
	return nil
}

func adminModuleConfig(cfg *Config, runtime *moduleRuntime) moduleserver.Config {
	capabilities := []string{
		"admin.identities",
		"admin.operators",
		"admin.sessions",
		"admin.roles",
		"admin.audit",
		"admin.configuration",
		"admin.oauth2",
	}
	if cfg.Admin.SCIM.Enabled {
		capabilities = append(capabilities, "admin.scim")
	}

	return moduleserver.Config{
		Module:             "admin",
		Version:            adminModuleVersion,
		ListenAddr:         net.JoinHostPort(cfg.Server.Address, strconv.Itoa(cfg.Server.Port)),
		Capabilities:       capabilities,
		Routes:             []string{adminPublicRoutePrefix},
		GRPCServices:       nil,
		EventSubscriptions: nil,
		RegisterHTTPRoutes: runtime.registerHTTPRoutes,
		Readiness:          runtime.readiness,
	}
}

func buildRuntime(cfg *Config, db *pgxpool.Pool) (*moduleRuntime, error) {
	if cfg == nil {
		return nil, errors.New("configuration is required")
	}
	if db == nil {
		return nil, errors.New("database pool is required")
	}
	if len(cfg.Secrets.Cipher) == 0 || strings.TrimSpace(cfg.Secrets.Cipher[0]) == "" {
		return nil, errors.New("admin social-provider management requires a configured cipher secret")
	}

	preparePublicRouteConfig(cfg)
	adminStore := store.New(db)
	adminService := service.New(adminStore, service.Config{
		BootstrapEnabled: cfg.Admin.BootstrapEnabled,
	})

	sum := sha256.Sum256([]byte(strings.TrimSpace(cfg.Secrets.Cipher[0])))
	socialRepo, err := socialstore.NewPostgres(db, sum[:])
	if err != nil {
		return nil, fmt.Errorf("initialize social provider manager: %w", err)
	}
	adminHandler := handler.New(adminService, handler.HandlerConfig{
		SessionTokenExpiry: cfg.Admin.SessionLifespan,
		DefaultPageSize:    cfg.Admin.DefaultPageSize,
		MaxPageSize:        cfg.Admin.MaxPageSize,
		APIKeyPrefix:       cfg.Admin.APIKeyPrefix,
		APIKeyPrefixLen:    cfg.Admin.APIKeyPrefixLen,
		APIKeyEntropyBytes: cfg.Admin.APIKeyEntropy,
		SocialProviders:    socialservice.New(socialRepo),
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
	router := server.setupPublicRouter()
	return &moduleRuntime{
		registerHTTPRoutes: func(mux *http.ServeMux) {
			mux.Handle(adminPublicRoutePrefix, router)
			mux.Handle(adminPublicRoutePrefix+"/", router)
		},
		readiness: func(ctx context.Context) error {
			return checkAdminRuntimeReadiness(ctx, db, cfg.Admin.SCIM.Enabled)
		},
	}, nil
}

func preparePublicRouteConfig(cfg *Config) {
	cfg.Admin.Path = adminPublicRoutePrefix
	if !cfg.Admin.SCIM.Enabled {
		return
	}

	scimPath := normalizeMountedPath(cfg.Admin.SCIM.BasePath)
	if scimPath != adminPublicRoutePrefix && !strings.HasPrefix(scimPath, adminPublicRoutePrefix+"/") {
		scimPath = adminPublicRoutePrefix + scimPath
	}
	cfg.Admin.SCIM.BasePath = scimPath
}

func checkAdminRuntimeReadiness(ctx context.Context, db *pgxpool.Pool, scimEnabled bool) error {
	if db == nil {
		return errors.New("database pool is required")
	}
	readinessCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.Ping(readinessCtx); err != nil {
		return fmt.Errorf("database unavailable: %w", err)
	}

	migrations, err := loadAdminMigrations(adminmodule.GetMigrationFiles())
	if err != nil {
		return fmt.Errorf("load schema contract: %w", err)
	}
	if len(migrations) == 0 {
		return errors.New("admin schema contract has no migrations")
	}
	latestVersion := migrations[len(migrations)-1].Version
	var latestApplied bool
	if err := db.QueryRow(readinessCtx,
		`SELECT EXISTS (SELECT 1 FROM adm_schema_migrations WHERE version = $1)`,
		latestVersion,
	).Scan(&latestApplied); err != nil {
		return fmt.Errorf("check admin schema version: %w", err)
	}
	if !latestApplied {
		return fmt.Errorf("admin schema migration %d is not applied", latestVersion)
	}

	relations := []string{
		"core_identities",
		"core_identity_addresses",
		"core_identity_schemas",
		"core_sessions",
		"core_system_config",
		"adm_roles",
		"adm_operators",
		"adm_api_keys",
		"adm_audit_log",
		"adm_ip_bans",
		"mfa_enrollments",
		"mfa_backup_codes",
		"mfa_totp_factors",
		"soc_providers",
		"soc_identity_links",
		"sso_connections",
		"proxy_upstreams",
		"proxy_routes",
		"oa2_clients",
		"oa2_access_tokens",
		"oa2_refresh_tokens",
		"oa2_id_tokens",
		"oa2_token_revocations",
		"pol_abac_rules",
		"pol_rebac_tuples",
		"pol_rebac_namespaces",
	}
	if scimEnabled {
		relations = append(relations, "adm_scim_groups", "adm_scim_tokens", "adm_scim_mappings")
	}
	for _, relation := range relations {
		var exists bool
		if err := db.QueryRow(readinessCtx, `SELECT to_regclass($1) IS NOT NULL`, relation).Scan(&exists); err != nil {
			return fmt.Errorf("check required relation %q: %w", relation, err)
		}
		if !exists {
			return fmt.Errorf("required relation %q is missing", relation)
		}
	}
	return nil
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
	if cfg.Admin.APIKeyPrefixLen <= 0 {
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
	if cfg.Observability.Endpoints.LozaCollector == "" {
		cfg.Observability.Endpoints.LozaCollector = "http://loza-collector:9308/health"
	}
	if cfg.Observability.Endpoints.Grafana == "" {
		cfg.Observability.Endpoints.Grafana = "http://grafana:3000/api/health"
	}
	if cfg.Observability.Endpoints.Tempo == "" {
		cfg.Observability.Endpoints.Tempo = "http://tempo:3200/ready"
	}
	applyObservabilityTelemetryDefaults(&cfg)

	// Override with environment variables
	if dbURL := getEnv("DATABASE_URL", ""); dbURL != "" {
		cfg.Database.URL = dbURL
	}
	cfg.Observability.Enabled = getEnvBool("AEGION_ADMIN_OBSERVABILITY_ENABLED", cfg.Observability.Enabled)
	cfg.Observability.ProbeTimeout = getEnvDuration("AEGION_ADMIN_OBSERVABILITY_PROBE_TIMEOUT", cfg.Observability.ProbeTimeout)
	if value := getEnv("AEGION_LOZA_COLLECTOR_URL", ""); value != "" {
		cfg.Observability.Endpoints.LozaCollector = value
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
	if value := getEnv("OTEL_SERVICE_NAME", ""); value != "" {
		cfg.Observability.Telemetry.ServiceName = value
	}
	if value := getEnv("AEGION_SERVICE_VERSION", ""); value != "" {
		cfg.Observability.Telemetry.ServiceVersion = value
	}
	if value := getEnv("AEGION_ENVIRONMENT", ""); value != "" {
		cfg.Observability.Telemetry.Environment = value
	}
	if value := getEnv("AEGION_INSTANCE_ID", ""); value != "" {
		cfg.Observability.Telemetry.InstanceID = value
	}
	if value := getEnv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", ""); value != "" {
		cfg.Observability.Telemetry.TracesEndpoint = value
	}
	if value := getEnv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", ""); value != "" {
		cfg.Observability.Telemetry.MetricsEndpoint = value
	}
	cfg.Observability.Telemetry.TraceSamplingRatio = getEnvFloat64("OTEL_TRACES_SAMPLER_ARG", cfg.Observability.Telemetry.TraceSamplingRatio)
	cfg.Observability.Telemetry.MetricExportInterval = getEnvDuration("OTEL_METRIC_EXPORT_INTERVAL", cfg.Observability.Telemetry.MetricExportInterval)
	cfg.Observability.Telemetry.TraceExportTimeout = getEnvDuration("OTEL_BSP_EXPORT_TIMEOUT", cfg.Observability.Telemetry.TraceExportTimeout)
	cfg.Observability.Telemetry.Insecure = getEnvBool("OTEL_EXPORTER_OTLP_INSECURE", cfg.Observability.Telemetry.Insecure)
	cfg.Observability.Telemetry.EnableTraces = getEnvBool("AEGION_OBS_ENABLE_TRACES", cfg.Observability.Telemetry.EnableTraces)
	cfg.Observability.Telemetry.EnableMetrics = getEnvBool("AEGION_OBS_ENABLE_METRICS", cfg.Observability.Telemetry.EnableMetrics)

	return &cfg, nil
}

func applyObservabilityTelemetryDefaults(cfg *Config) {
	defaults := platformobservability.DefaultConfig()

	if cfg.Observability.Telemetry.ServiceName == "" {
		cfg.Observability.Telemetry.ServiceName = defaults.ServiceName
	}
	if cfg.Observability.Telemetry.ServiceVersion == "" {
		cfg.Observability.Telemetry.ServiceVersion = defaults.ServiceVersion
	}
	if cfg.Observability.Telemetry.Environment == "" {
		cfg.Observability.Telemetry.Environment = defaults.Environment
	}
	if cfg.Observability.Telemetry.InstanceID == "" {
		cfg.Observability.Telemetry.InstanceID = defaults.InstanceID
	}
	if cfg.Observability.Telemetry.TracesEndpoint == "" {
		cfg.Observability.Telemetry.TracesEndpoint = defaults.TracesEndpoint
	}
	if cfg.Observability.Telemetry.MetricsEndpoint == "" {
		cfg.Observability.Telemetry.MetricsEndpoint = defaults.MetricsEndpoint
	}
	if cfg.Observability.Telemetry.TraceSamplingRatio == 0 {
		cfg.Observability.Telemetry.TraceSamplingRatio = defaults.TraceSamplingRatio
	}
	if cfg.Observability.Telemetry.MetricExportInterval == 0 {
		cfg.Observability.Telemetry.MetricExportInterval = defaults.MetricExportInterval
	}
	if cfg.Observability.Telemetry.TraceExportTimeout == 0 {
		cfg.Observability.Telemetry.TraceExportTimeout = defaults.TraceExportTimeout
	}
	if !cfg.Observability.Telemetry.EnableTraces &&
		!cfg.Observability.Telemetry.EnableMetrics {
		cfg.Observability.Telemetry.EnableTraces = defaults.EnableTraces
		cfg.Observability.Telemetry.EnableMetrics = defaults.EnableMetrics
	}
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

func setupLogger(logConfig LogConfig) {
	xlog.New(xlog.Config{
		Level:            logConfig.Level,
		Format:           logConfig.Format,
		ServiceName:      "aegion-admin",
		ServiceNamespace: os.Getenv("AEGION_LOG_NAMESPACE"),
		Environment:      os.Getenv("AEGION_ENV"),
		CloudRegion:      os.Getenv("AEGION_CLOUD_REGION"),
		Developer:        os.Getenv("DEV_NAME"),
		ServiceVersion:   "1.0.0",
	})

	xlog.Default().Info("Logger initialized")
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
		xlog.Default().InfoContext(ctx, "No admin migrations found")
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
		xlog.Default().InfoContext(ctx, "Applied admin migration", "version", migration.Version, "name", migration.Name)
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

func getEnvFloat64(key string, defaultValue float64) float64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return defaultValue
	}

	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return defaultValue
	}

	return parsed
}
