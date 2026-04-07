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

func TestValidateModuleDependencies(t *testing.T) {
	tests := []struct {
		name string
		cfg  *AegionConfig
		err  error
		msg  string
	}{
		{
			name: "nil config is valid",
			cfg:  nil,
		},
		{
			name: "introspection requires oauth2",
			cfg: &AegionConfig{
				ModuleVersions: map[string]string{
					"introspection": "latest",
				},
			},
			err: ErrMissingDependency,
			msg: `module "introspection" requires "oauth2"`,
		},
		{
			name: "mfa requires password",
			cfg: &AegionConfig{
				ModuleVersions: map[string]string{
					"mfa": "latest",
				},
			},
			err: ErrMissingDependency,
			msg: `module "mfa" requires "password"`,
		},
		{
			name: "disabled module does not enforce deps",
			cfg: &AegionConfig{
				ModuleVersions: map[string]string{
					"introspection": "off",
				},
			},
		},
		{
			name: "admin with policy is valid",
			cfg: &AegionConfig{
				ModuleVersions: map[string]string{
					"admin":  "latest",
					"policy": "latest",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateModuleDependencies(tt.cfg)
			if !errors.Is(err, tt.err) {
				t.Fatalf("expected error %v, got %v", tt.err, err)
			}
			if tt.msg != "" && !strings.Contains(err.Error(), tt.msg) {
				t.Fatalf("expected error to contain %q, got %v", tt.msg, err)
			}
		})
	}
}

func TestConfigLoader_Load_ValidatesModuleDependencies(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "aegion.yaml")
	content := `
module_versions:
  introspection: "latest"
module_registry:
  base_url: ""
server:
  internal_network:
    name: aegion_modules
    subnet: 10.10.0.0/16
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	loader := NewConfigLoader(configPath)
	_, err := loader.Load()
	if !errors.Is(err, ErrMissingDependency) {
		t.Fatalf("expected ErrMissingDependency, got %v", err)
	}
	if !strings.Contains(err.Error(), `module "introspection" requires "oauth2"`) {
		t.Fatalf("expected dependency detail, got %v", err)
	}
}

func TestEnabledModuleOrder(t *testing.T) {
	t.Run("orders enabled modules by dependency", func(t *testing.T) {
		order, err := EnabledModuleOrder(map[string]string{
			"admin":         "latest",
			"introspection": "latest",
			"oauth2":        "latest",
			"policy":        "latest",
			"password":      "latest",
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		mustAppearAfter(t, order, "admin", "policy")
		mustAppearAfter(t, order, "introspection", "oauth2")
	})

	t.Run("disabled modules are omitted", func(t *testing.T) {
		order, err := EnabledModuleOrder(map[string]string{
			"oauth2":        "latest",
			"introspection": "off",
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(order) != 1 || order[0] != "oauth2" {
			t.Fatalf("expected only oauth2 in order, got %v", order)
		}
	})

	t.Run("returns missing dependency error", func(t *testing.T) {
		_, err := EnabledModuleOrder(map[string]string{
			"admin": "latest",
		})
		if !errors.Is(err, ErrMissingDependency) {
			t.Fatalf("expected ErrMissingDependency, got %v", err)
		}
	})
}

func mustAppearAfter(t *testing.T, order []string, module, dependency string) {
	t.Helper()

	moduleIdx := -1
	dependencyIdx := -1
	for i, item := range order {
		if item == module {
			moduleIdx = i
		}
		if item == dependency {
			dependencyIdx = i
		}
	}

	if moduleIdx == -1 {
		t.Fatalf("expected module %q in order %v", module, order)
	}
	if dependencyIdx == -1 {
		t.Fatalf("expected dependency %q in order %v", dependency, order)
	}
	if moduleIdx <= dependencyIdx {
		t.Fatalf("expected %q after %q, got %v", module, dependency, order)
	}
}
