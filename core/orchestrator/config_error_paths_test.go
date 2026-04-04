package orchestrator

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeMinimalMainConfig(t *testing.T, dir string) string {
	t.Helper()

	configPath := filepath.Join(dir, "aegion.yaml")
	content := `
module_versions:
  password: "v1.0.0"
module_registry:
  base_url: ""
server:
  internal_network:
    name: aegion_modules
    subnet: 10.10.0.0/16
secrets:
  internal:
    - internal-secret
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}
	return configPath
}

func TestConfigLoader_AdditionalErrorPaths(t *testing.T) {
	t.Run("Load returns wrapped read error for directory path", func(t *testing.T) {
		dir := t.TempDir()
		loader := NewConfigLoader(dir)

		_, err := loader.Load()
		if err == nil || !strings.Contains(err.Error(), "reading config file") {
			t.Fatalf("expected wrapped read error, got %v", err)
		}
	})

	t.Run("LoadModuleConfig propagates load error when config is unavailable", func(t *testing.T) {
		loader := NewConfigLoader(filepath.Join(t.TempDir(), "missing", "aegion.yaml"))

		_, err := loader.LoadModuleConfig("password")
		if !errors.Is(err, ErrConfigNotFound) {
			t.Fatalf("expected ErrConfigNotFound, got %v", err)
		}
	})

	t.Run("LoadModuleConfig returns wrapped module read error", func(t *testing.T) {
		dir := t.TempDir()
		configPath := writeMinimalMainConfig(t, dir)
		modulesDir := filepath.Join(dir, "modules")
		if err := os.MkdirAll(modulesDir, 0o755); err != nil {
			t.Fatalf("failed to create modules dir: %v", err)
		}
		if err := os.Mkdir(filepath.Join(modulesDir, "password.yaml"), 0o755); err != nil {
			t.Fatalf("failed to create module directory placeholder: %v", err)
		}

		loader := NewConfigLoader(configPath)
		_, err := loader.LoadModuleConfig("password")
		if err == nil || !strings.Contains(err.Error(), "reading module config") {
			t.Fatalf("expected wrapped module read error, got %v", err)
		}
	})

	t.Run("LoadModuleConfig validates module-specific file", func(t *testing.T) {
		dir := t.TempDir()
		configPath := writeMinimalMainConfig(t, dir)
		modulesDir := filepath.Join(dir, "modules")
		if err := os.MkdirAll(modulesDir, 0o755); err != nil {
			t.Fatalf("failed to create modules dir: %v", err)
		}
		modulePath := filepath.Join(modulesDir, "broken.yaml")
		if err := os.WriteFile(modulePath, []byte("id: broken\nname: Broken Module\n"), 0o644); err != nil {
			t.Fatalf("failed to write module config: %v", err)
		}

		loader := NewConfigLoader(configPath)
		_, err := loader.LoadModuleConfig("broken")
		if !errors.Is(err, ErrMissingImage) {
			t.Fatalf("expected ErrMissingImage, got %v", err)
		}
	})

	t.Run("LoadModuleConfig validates generated module config", func(t *testing.T) {
		dir := t.TempDir()
		configPath := writeMinimalMainConfig(t, dir)
		loader := NewConfigLoader(configPath)

		_, err := loader.LoadModuleConfig("")
		if !errors.Is(err, ErrMissingModuleID) {
			t.Fatalf("expected ErrMissingModuleID, got %v", err)
		}
	})
}

func TestApplyModuleDefaults_UsesMainNetworkDefaults(t *testing.T) {
	cfg := &ModuleConfig{
		ID:    "module-x",
		Name:  "Module X",
		Image: "repo/module-x",
	}
	mainCfg := &AegionConfig{
		Server: ServerConfig{
			InternalNetwork: NetworkConfig{
				Name:                "custom-net",
				HealthCheckInterval: 11 * time.Second,
				HealthCheckTimeout:  7 * time.Second,
				HealthCheckFailures: 9,
				StartupTimeout:      55 * time.Second,
			},
		},
	}

	applyModuleDefaults(cfg, mainCfg)

	if cfg.Network != "custom-net" {
		t.Fatalf("expected network from main config, got %q", cfg.Network)
	}
	if cfg.HealthCheck.Interval != 11*time.Second {
		t.Fatalf("expected interval from main config, got %v", cfg.HealthCheck.Interval)
	}
	if cfg.HealthCheck.Timeout != 7*time.Second {
		t.Fatalf("expected timeout from main config, got %v", cfg.HealthCheck.Timeout)
	}
	if cfg.HealthCheck.Retries != 9 {
		t.Fatalf("expected retries from main config, got %d", cfg.HealthCheck.Retries)
	}
	if cfg.HealthCheck.StartPeriod != 55*time.Second {
		t.Fatalf("expected start period from main config, got %v", cfg.HealthCheck.StartPeriod)
	}
}

func TestConfigLoader_GettersPropagateLoadErrors(t *testing.T) {
	loader := NewConfigLoader(filepath.Join(t.TempDir(), "missing", "aegion.yaml"))

	if _, err := loader.GetInternalSecret(); !errors.Is(err, ErrConfigNotFound) {
		t.Fatalf("expected ErrConfigNotFound from GetInternalSecret, got %v", err)
	}
	if _, err := loader.GetNetworkConfig(); !errors.Is(err, ErrConfigNotFound) {
		t.Fatalf("expected ErrConfigNotFound from GetNetworkConfig, got %v", err)
	}
}
