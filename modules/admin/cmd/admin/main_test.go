package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

func TestGetEnv(t *testing.T) {
	const key = "AEGION_TEST_GETENV"
	_ = os.Unsetenv(key)

	if got := getEnv(key, "fallback"); got != "fallback" {
		t.Fatalf("expected fallback value, got %q", got)
	}

	if err := os.Setenv(key, "configured"); err != nil {
		t.Fatalf("setenv failed: %v", err)
	}
	defer func() {
		_ = os.Unsetenv(key)
	}()

	if got := getEnv(key, "fallback"); got != "configured" {
		t.Fatalf("expected configured value, got %q", got)
	}
}

func TestLoadConfig_DefaultsAndEnvOverride(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "admin.yaml")

	configYAML := strings.TrimSpace(`
database:
  url: postgres://file-user:file-pass@localhost:5432/aegion
  max_conns: 0
  min_conns: 0
server:
  address: ""
  port: 0
admin:
  path: ""
`)

	if err := os.WriteFile(cfgPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	if err := os.Setenv("DATABASE_URL", "postgres://env-user:env-pass@db:5432/aegion"); err != nil {
		t.Fatalf("setenv failed: %v", err)
	}
	defer func() {
		_ = os.Unsetenv("DATABASE_URL")
	}()

	cfg, err := loadConfig(cfgPath)
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}

	if cfg.Database.URL != "postgres://env-user:env-pass@db:5432/aegion" {
		t.Fatalf("expected DATABASE_URL override, got %q", cfg.Database.URL)
	}
	if cfg.Server.Address != "0.0.0.0" || cfg.Server.Port != 8082 {
		t.Fatalf("expected server defaults, got %s:%d", cfg.Server.Address, cfg.Server.Port)
	}
	if cfg.Admin.Path != "/admin" {
		t.Fatalf("expected admin path default /admin, got %q", cfg.Admin.Path)
	}
	if cfg.Admin.SessionLifespan != 4*time.Hour {
		t.Fatalf("expected default session lifespan 4h, got %v", cfg.Admin.SessionLifespan)
	}
	if cfg.Database.MaxConns != 25 || cfg.Database.MinConns != 5 {
		t.Fatalf("expected database defaults max/min 25/5, got %d/%d", cfg.Database.MaxConns, cfg.Database.MinConns)
	}
	if cfg.Observability.ProbeTimeout != 5*time.Second {
		t.Fatalf("expected observability probe timeout default 5s, got %v", cfg.Observability.ProbeTimeout)
	}
	if cfg.Observability.Endpoints.Grafana != "http://grafana:3000/api/health" {
		t.Fatalf("expected default grafana endpoint, got %q", cfg.Observability.Endpoints.Grafana)
	}
}

func TestLoadConfig_ExpandEnvInFile(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "admin-env.yaml")

	if err := os.Setenv("ADMIN_DB_URL", "postgres://expanded:expanded@localhost:5432/aegion"); err != nil {
		t.Fatalf("setenv failed: %v", err)
	}
	defer func() {
		_ = os.Unsetenv("ADMIN_DB_URL")
	}()

	configYAML := strings.TrimSpace(`
database:
  url: ${ADMIN_DB_URL}
server:
  address: 127.0.0.1
  port: 9090
admin:
  path: /console
`)

	if err := os.WriteFile(cfgPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := loadConfig(cfgPath)
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}

	if cfg.Database.URL != "postgres://expanded:expanded@localhost:5432/aegion" {
		t.Fatalf("expected expanded DB URL, got %q", cfg.Database.URL)
	}
	if cfg.Server.Address != "127.0.0.1" || cfg.Server.Port != 9090 {
		t.Fatalf("expected configured server values, got %s:%d", cfg.Server.Address, cfg.Server.Port)
	}
	if cfg.Admin.Path != "/console" {
		t.Fatalf("expected configured admin path /console, got %q", cfg.Admin.Path)
	}
}

func TestLoadConfig_ObservabilityEnvOverride(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "admin-observability.yaml")

	configYAML := strings.TrimSpace(`
database:
  url: postgres://localhost:5432/aegion
server:
  address: 127.0.0.1
  port: 8082
`)
	if err := os.WriteFile(cfgPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	if err := os.Setenv("AEGION_ADMIN_OBSERVABILITY_ENABLED", "true"); err != nil {
		t.Fatalf("setenv failed: %v", err)
	}
	if err := os.Setenv("AEGION_ADMIN_OBSERVABILITY_PROBE_TIMEOUT", "2s"); err != nil {
		t.Fatalf("setenv failed: %v", err)
	}
	if err := os.Setenv("AEGION_ADMIN_OBS_PROMETHEUS_URL", "http://obs-prom:9090/-/healthy"); err != nil {
		t.Fatalf("setenv failed: %v", err)
	}
	defer func() {
		_ = os.Unsetenv("AEGION_ADMIN_OBSERVABILITY_ENABLED")
		_ = os.Unsetenv("AEGION_ADMIN_OBSERVABILITY_PROBE_TIMEOUT")
		_ = os.Unsetenv("AEGION_ADMIN_OBS_PROMETHEUS_URL")
	}()

	cfg, err := loadConfig(cfgPath)
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}

	if !cfg.Observability.Enabled {
		t.Fatal("expected observability enabled from env override")
	}
	if cfg.Observability.ProbeTimeout != 2*time.Second {
		t.Fatalf("expected probe timeout 2s from env override, got %v", cfg.Observability.ProbeTimeout)
	}
	if cfg.Observability.Endpoints.Prometheus != "http://obs-prom:9090/-/healthy" {
		t.Fatalf("expected prometheus endpoint override, got %q", cfg.Observability.Endpoints.Prometheus)
	}
}

func TestLoadConfig_InvalidFileAndYAML(t *testing.T) {
	if _, err := loadConfig(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatalf("expected error when config file is missing")
	}

	cfgPath := filepath.Join(t.TempDir(), "invalid.yaml")
	if err := os.WriteFile(cfgPath, []byte("database: [invalid"), 0o644); err != nil {
		t.Fatalf("failed to write invalid config: %v", err)
	}
	if _, err := loadConfig(cfgPath); err == nil {
		t.Fatalf("expected parse error for invalid yaml")
	}
}

func TestParseMainFlags(t *testing.T) {
	t.Run("uses aegion superconfig default path", func(t *testing.T) {
		flags, err := parseMainFlags(nil, func(_ string, fallback string) string {
			return fallback
		})
		if err != nil {
			t.Fatalf("parseMainFlags returned error: %v", err)
		}
		if flags.configPath != "aegion.yaml" {
			t.Fatalf("expected default config path aegion.yaml, got %q", flags.configPath)
		}
	})

	t.Run("uses env default config path", func(t *testing.T) {
		flags, err := parseMainFlags(nil, func(key, fallback string) string {
			if key == "AEGION_CONFIG_PATH" {
				return "env-admin.yaml"
			}
			return fallback
		})
		if err != nil {
			t.Fatalf("parseMainFlags returned error: %v", err)
		}
		if flags.configPath != "env-admin.yaml" {
			t.Fatalf("expected env-admin.yaml, got %q", flags.configPath)
		}
		if flags.version || flags.migrate {
			t.Fatalf("expected false flags by default, got version=%v migrate=%v", flags.version, flags.migrate)
		}
	})

	t.Run("parses explicit args", func(t *testing.T) {
		flags, err := parseMainFlags([]string{"-config", "custom.yaml", "-version", "-migrate"}, getEnv)
		if err != nil {
			t.Fatalf("parseMainFlags returned error: %v", err)
		}
		if flags.configPath != "custom.yaml" || !flags.version || !flags.migrate {
			t.Fatalf("unexpected parsed flags: %+v", flags)
		}
	})

	t.Run("returns error for unknown flags", func(t *testing.T) {
		if _, err := parseMainFlags([]string{"-unknown"}, getEnv); err == nil {
			t.Fatalf("expected parse error for unknown flag")
		}
	})
}

func TestLoadConfig_AegionSuperconfigMapping(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "aegion.yaml")

	configYAML := strings.TrimSpace(`
module_versions:
  admin: latest

database:
  url: postgres://super:config@localhost:5432/aegion
  max_open_connections: 31
  max_idle_connections: 7
  connection_max_idle_time: 9m

server:
  host: 127.0.0.1
  port: 8092
  read_timeout: 22s
  write_timeout: 33s
  idle_timeout: 44s

admin:
  enabled: true
  path: /aegion
  session_lifespan: 6h
  default_page_size: 30
  max_page_size: 300
  api_key_prefix: aegion_live_
  api_key_lookup_prefix_len: 10
  api_key_entropy_bytes: 40
  scim:
    enabled: true
    base_path: /api/admin/scim/v2
    token_prefix: aegion_scim_live_
    token_lookup_prefix_len: 9
    token_entropy_bytes: 48
    default_page_size: 35
    max_page_size: 1200
    token_last_used_update_timeout: 3s

log:
  level: warn
  format: json
`)

	if err := os.WriteFile(cfgPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := loadConfig(cfgPath)
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}

	if cfg.Database.URL != "postgres://super:config@localhost:5432/aegion" {
		t.Fatalf("expected mapped database url, got %q", cfg.Database.URL)
	}
	if cfg.Database.MaxConns != 31 || cfg.Database.MinConns != 7 {
		t.Fatalf("expected mapped db conns 31/7, got %d/%d", cfg.Database.MaxConns, cfg.Database.MinConns)
	}
	if cfg.Database.MaxIdleTime != "9m0s" {
		t.Fatalf("expected mapped max idle time 9m0s, got %q", cfg.Database.MaxIdleTime)
	}

	if cfg.Server.Address != "127.0.0.1" || cfg.Server.Port != 8092 {
		t.Fatalf("expected mapped server 127.0.0.1:8092, got %s:%d", cfg.Server.Address, cfg.Server.Port)
	}
	if cfg.Server.ReadTimeout != 22*time.Second || cfg.Server.WriteTimeout != 33*time.Second || cfg.Server.IdleTimeout != 44*time.Second {
		t.Fatalf("expected mapped server timeouts 22s/33s/44s, got %v/%v/%v", cfg.Server.ReadTimeout, cfg.Server.WriteTimeout, cfg.Server.IdleTimeout)
	}

	if cfg.Admin.Path != "/aegion" || cfg.Admin.DefaultPageSize != 30 || cfg.Admin.MaxPageSize != 300 {
		t.Fatalf("expected mapped admin path/page settings, got path=%q default=%d max=%d", cfg.Admin.Path, cfg.Admin.DefaultPageSize, cfg.Admin.MaxPageSize)
	}
	if cfg.Admin.APIKeyPrefix != "aegion_live_" || cfg.Admin.APIKeyPrefixLen != 10 || cfg.Admin.APIKeyEntropy != 40 {
		t.Fatalf("expected mapped admin API key settings, got prefix=%q len=%d entropy=%d", cfg.Admin.APIKeyPrefix, cfg.Admin.APIKeyPrefixLen, cfg.Admin.APIKeyEntropy)
	}

	if !cfg.Admin.SCIM.Enabled {
		t.Fatalf("expected mapped scim enabled true")
	}
	if cfg.Admin.SCIM.BasePath != "/api/admin/scim/v2" {
		t.Fatalf("expected mapped scim base path, got %q", cfg.Admin.SCIM.BasePath)
	}
	if cfg.Admin.SCIM.TokenPrefix != "aegion_scim_live_" || cfg.Admin.SCIM.TokenLookupPrefixLen != 9 || cfg.Admin.SCIM.TokenEntropyBytes != 48 {
		t.Fatalf("expected mapped scim token settings, got prefix=%q len=%d entropy=%d", cfg.Admin.SCIM.TokenPrefix, cfg.Admin.SCIM.TokenLookupPrefixLen, cfg.Admin.SCIM.TokenEntropyBytes)
	}
	if cfg.Admin.SCIM.DefaultPageSize != 35 || cfg.Admin.SCIM.MaxPageSize != 1200 {
		t.Fatalf("expected mapped scim page settings 35/1200, got %d/%d", cfg.Admin.SCIM.DefaultPageSize, cfg.Admin.SCIM.MaxPageSize)
	}
	if cfg.Admin.SCIM.TokenLastUsedUpdateTimeout != 3*time.Second {
		t.Fatalf("expected mapped scim last-used timeout 3s, got %v", cfg.Admin.SCIM.TokenLastUsedUpdateTimeout)
	}
}

func TestSetupLogger(t *testing.T) {
	prevPretty := os.Getenv("AEGION_LOG_PRETTY")
	defer func() { _ = os.Setenv("AEGION_LOG_PRETTY", prevPretty) }()

	setupLogger(LogConfig{
		Level:  "debug",
		Format: "json",
	})
	if zerolog.GlobalLevel() != zerolog.DebugLevel {
		t.Fatalf("expected debug log level, got %s", zerolog.GlobalLevel())
	}

	_ = os.Setenv("AEGION_LOG_PRETTY", "true")
	setupLogger(LogConfig{
		Level:  "info",
		Format: "pretty",
	})
	if zerolog.GlobalLevel() != zerolog.InfoLevel {
		t.Fatalf("expected info log level, got %s", zerolog.GlobalLevel())
	}
}

func TestRunMigrations_RequiresDatabasePool(t *testing.T) {
	err := runMigrations(t.Context(), nil)
	if err == nil || !strings.Contains(err.Error(), "database pool is nil") {
		t.Fatalf("expected nil-db error, got %v", err)
	}
}

type testRuntimeServer struct {
	registerErr   error
	shutdownErr   error
	registerCalls int
	shutdownCalls int
}

func (s *testRuntimeServer) registerWithCore(ctx context.Context) error {
	s.registerCalls++
	return s.registerErr
}

func (s *testRuntimeServer) shutdown(ctx context.Context) error {
	s.shutdownCalls++
	return s.shutdownErr
}

func baseRunConfig() *Config {
	var cfg Config
	cfg.Database.URL = "postgres://admin:admin@localhost:5432/aegion?sslmode=disable"
	cfg.Database.MaxConns = 25
	cfg.Database.MinConns = 5
	cfg.Server.Address = "127.0.0.1"
	cfg.Server.Port = 8082
	return &cfg
}

func baseRunDeps(cfg *Config) (mainDeps, *bytes.Buffer, *testRuntimeServer) {
	stdout := &bytes.Buffer{}
	runtime := &testRuntimeServer{}

	deps := mainDeps{
		stdout: stdout,
		loadConfig: func(path string) (*Config, error) {
			return cfg, nil
		},
		setupLogger:   func(logConfig LogConfig) {},
		parseDBConfig: func(connString string) (*pgxpool.Config, error) { return &pgxpool.Config{}, nil },
		newDBPool:     func(ctx context.Context, config *pgxpool.Config) (*pgxpool.Pool, error) { return nil, nil },
		pingDB:        func(ctx context.Context, db *pgxpool.Pool) error { return nil },
		closeDB:       func(db *pgxpool.Pool) {},
		runMigrations: func(ctx context.Context, db *pgxpool.Pool) error { return nil },
		startServer: func(cfg *Config, db *pgxpool.Pool) (runtimeServer, error) {
			return runtime, nil
		},
		newSignalChan: func() chan os.Signal { return make(chan os.Signal, 1) },
		notifySignals: func(c chan<- os.Signal, sig ...os.Signal) { c <- os.Interrupt },
		stopSignalChan: func(c chan<- os.Signal) {
		},
	}

	return deps, stdout, runtime
}

func TestRun(t *testing.T) {
	t.Run("version mode only prints version", func(t *testing.T) {
		deps, stdout, _ := baseRunDeps(baseRunConfig())
		loadConfigCalled := false
		deps.loadConfig = func(path string) (*Config, error) {
			loadConfigCalled = true
			return nil, errors.New("should not be called")
		}

		if err := run([]string{"-version"}, deps); err != nil {
			t.Fatalf("run returned error in version mode: %v", err)
		}
		if loadConfigCalled {
			t.Fatalf("loadConfig should not be called in version mode")
		}
		if got := strings.TrimSpace(stdout.String()); got != "Aegion Admin Module v1.0.0" {
			t.Fatalf("unexpected version output %q", got)
		}
	})

	t.Run("load config error", func(t *testing.T) {
		deps, _, _ := baseRunDeps(baseRunConfig())
		deps.loadConfig = func(path string) (*Config, error) {
			return nil, errors.New("missing config")
		}

		err := run(nil, deps)
		if err == nil || !strings.Contains(err.Error(), "failed to load configuration") {
			t.Fatalf("expected load config error, got %v", err)
		}
	})

	t.Run("db config parse error", func(t *testing.T) {
		deps, _, _ := baseRunDeps(baseRunConfig())
		deps.parseDBConfig = func(connString string) (*pgxpool.Config, error) {
			return nil, errors.New("bad db url")
		}

		err := run(nil, deps)
		if err == nil || !strings.Contains(err.Error(), "failed to parse database URL") {
			t.Fatalf("expected parse db config error, got %v", err)
		}
	})

	t.Run("invalid max_idle_time", func(t *testing.T) {
		cfg := baseRunConfig()
		cfg.Database.MaxIdleTime = "invalid-duration"
		deps, _, _ := baseRunDeps(cfg)

		err := run(nil, deps)
		if err == nil || !strings.Contains(err.Error(), "failed to parse max_idle_time") {
			t.Fatalf("expected invalid max_idle_time error, got %v", err)
		}
	})

	t.Run("db pool creation error", func(t *testing.T) {
		deps, _, _ := baseRunDeps(baseRunConfig())
		deps.newDBPool = func(ctx context.Context, config *pgxpool.Config) (*pgxpool.Pool, error) {
			return nil, errors.New("dial failure")
		}

		err := run(nil, deps)
		if err == nil || !strings.Contains(err.Error(), "failed to connect to database") {
			t.Fatalf("expected db connect error, got %v", err)
		}
	})

	t.Run("db ping error still closes db", func(t *testing.T) {
		deps, _, _ := baseRunDeps(baseRunConfig())
		closed := false
		deps.pingDB = func(ctx context.Context, db *pgxpool.Pool) error {
			return errors.New("ping failed")
		}
		deps.closeDB = func(db *pgxpool.Pool) {
			closed = true
		}

		err := run(nil, deps)
		if err == nil || !strings.Contains(err.Error(), "failed to ping database") {
			t.Fatalf("expected ping error, got %v", err)
		}
		if !closed {
			t.Fatalf("expected closeDB to be called on ping failure")
		}
	})

	t.Run("migrate mode runs migrations and exits", func(t *testing.T) {
		deps, _, _ := baseRunDeps(baseRunConfig())
		migrationsCalled := false
		startCalled := false
		deps.runMigrations = func(ctx context.Context, db *pgxpool.Pool) error {
			migrationsCalled = true
			return nil
		}
		deps.startServer = func(cfg *Config, db *pgxpool.Pool) (runtimeServer, error) {
			startCalled = true
			return &testRuntimeServer{}, nil
		}

		if err := run([]string{"-migrate"}, deps); err != nil {
			t.Fatalf("run returned error in migrate mode: %v", err)
		}
		if !migrationsCalled {
			t.Fatalf("expected runMigrations to be called")
		}
		if startCalled {
			t.Fatalf("startServer should not be called in migrate mode")
		}
	})

	t.Run("migrate mode error", func(t *testing.T) {
		deps, _, _ := baseRunDeps(baseRunConfig())
		deps.runMigrations = func(ctx context.Context, db *pgxpool.Pool) error {
			return errors.New("migration failed")
		}

		err := run([]string{"-migrate"}, deps)
		if err == nil || !strings.Contains(err.Error(), "failed to run migrations") {
			t.Fatalf("expected migration error, got %v", err)
		}
	})

	t.Run("start server error", func(t *testing.T) {
		deps, _, _ := baseRunDeps(baseRunConfig())
		deps.startServer = func(cfg *Config, db *pgxpool.Pool) (runtimeServer, error) {
			return nil, errors.New("server init failed")
		}

		err := run(nil, deps)
		if err == nil || !strings.Contains(err.Error(), "failed to initialize server") {
			t.Fatalf("expected initialize server error, got %v", err)
		}
	})

	t.Run("register with core error does not fail run", func(t *testing.T) {
		deps, _, runtime := baseRunDeps(baseRunConfig())
		runtime.registerErr = errors.New("core unavailable")

		if err := run(nil, deps); err != nil {
			t.Fatalf("run should continue on registerWithCore error, got %v", err)
		}
		if runtime.registerCalls != 1 {
			t.Fatalf("expected registerWithCore to be called once, got %d", runtime.registerCalls)
		}
		if runtime.shutdownCalls != 1 {
			t.Fatalf("expected shutdown to be called once, got %d", runtime.shutdownCalls)
		}
	})

	t.Run("shutdown error is returned", func(t *testing.T) {
		deps, _, runtime := baseRunDeps(baseRunConfig())
		runtime.shutdownErr = errors.New("shutdown failed")

		err := run(nil, deps)
		if err == nil || !strings.Contains(err.Error(), "server shutdown error") {
			t.Fatalf("expected shutdown error, got %v", err)
		}
		if runtime.shutdownCalls != 1 {
			t.Fatalf("expected shutdown to be called once, got %d", runtime.shutdownCalls)
		}
	})

	t.Run("happy path handles configured idle timeout and signals", func(t *testing.T) {
		cfg := baseRunConfig()
		cfg.Database.MaxIdleTime = "30s"
		deps, _, runtime := baseRunDeps(cfg)

		var parsedConnString string
		var capturedMaxConns int32
		var capturedMinConns int32
		var capturedIdle time.Duration
		stoppedSignals := false

		deps.parseDBConfig = func(connString string) (*pgxpool.Config, error) {
			parsedConnString = connString
			return &pgxpool.Config{}, nil
		}
		deps.newDBPool = func(ctx context.Context, config *pgxpool.Config) (*pgxpool.Pool, error) {
			capturedMaxConns = config.MaxConns
			capturedMinConns = config.MinConns
			capturedIdle = config.MaxConnIdleTime
			return nil, nil
		}
		deps.notifySignals = func(c chan<- os.Signal, sig ...os.Signal) {
			c <- syscall.SIGTERM
		}
		deps.stopSignalChan = func(c chan<- os.Signal) {
			stoppedSignals = true
		}

		if err := run(nil, deps); err != nil {
			t.Fatalf("run returned error: %v", err)
		}
		if parsedConnString != cfg.Database.URL {
			t.Fatalf("expected parseDBConfig to receive %q, got %q", cfg.Database.URL, parsedConnString)
		}
		if capturedMaxConns != cfg.Database.MaxConns || capturedMinConns != cfg.Database.MinConns {
			t.Fatalf("unexpected conn limits max/min=%d/%d", capturedMaxConns, capturedMinConns)
		}
		if capturedIdle != 30*time.Second {
			t.Fatalf("expected idle timeout 30s, got %v", capturedIdle)
		}
		if runtime.registerCalls != 1 || runtime.shutdownCalls != 1 {
			t.Fatalf("expected lifecycle calls register/shutdown=1/1, got %d/%d", runtime.registerCalls, runtime.shutdownCalls)
		}
		if !stoppedSignals {
			t.Fatalf("expected signal stop hook to be called")
		}
	})
}
