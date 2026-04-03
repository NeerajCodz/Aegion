package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	defer os.Unsetenv(key)

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
	defer os.Unsetenv("DATABASE_URL")

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
}

func TestLoadConfig_ExpandEnvInFile(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "admin-env.yaml")

	if err := os.Setenv("ADMIN_DB_URL", "postgres://expanded:expanded@localhost:5432/aegion"); err != nil {
		t.Fatalf("setenv failed: %v", err)
	}
	defer os.Unsetenv("ADMIN_DB_URL")

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

func TestSetupLogger(t *testing.T) {
	prevPretty := os.Getenv("AEGION_LOG_PRETTY")
	defer func() { _ = os.Setenv("AEGION_LOG_PRETTY", prevPretty) }()

	setupLogger(struct {
		Level  string `yaml:"level"`
		Format string `yaml:"format"`
	}{
		Level:  "debug",
		Format: "json",
	})
	if zerolog.GlobalLevel() != zerolog.DebugLevel {
		t.Fatalf("expected debug log level, got %s", zerolog.GlobalLevel())
	}

	_ = os.Setenv("AEGION_LOG_PRETTY", "true")
	setupLogger(struct {
		Level  string `yaml:"level"`
		Format string `yaml:"format"`
	}{
		Level:  "info",
		Format: "pretty",
	})
	if zerolog.GlobalLevel() != zerolog.InfoLevel {
		t.Fatalf("expected info log level, got %s", zerolog.GlobalLevel())
	}
}

func TestRunMigrations_NoOp(t *testing.T) {
	if err := runMigrations(t.Context(), nil); err != nil {
		t.Fatalf("runMigrations should currently be no-op, got error: %v", err)
	}
}
