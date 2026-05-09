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

	t.Run("LoadModuleConfig rejects traversal-style module ids", func(t *testing.T) {
		dir := t.TempDir()
		configPath := writeMinimalMainConfig(t, dir)
		loader := NewConfigLoader(configPath)

		_, err := loader.LoadModuleConfig("../password")
		if err == nil || !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("expected ErrInvalidConfig for traversal module id, got %v", err)
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


func TestLoadModuleConfig_RejectsExperimentalModuleInProductionWhenOmittedFromMainConfig(t *testing.T) {
	t.Setenv("AEGION_ENV", "production")
	t.Setenv("AEGION_ALLOW_EXPERIMENTAL_MODULES", "false")

	dir := t.TempDir()
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
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	loader := NewConfigLoader(configPath)
	_, err := loader.LoadModuleConfig("mfa")
	if !errors.Is(err, ErrExperimentalModule) {
		t.Fatalf("expected ErrExperimentalModule, got %v", err)
	}
}

func TestLoadModuleConfig_AllowsExperimentalModuleInProductionWithOverride(t *testing.T) {
	t.Setenv("AEGION_ENV", "production")
	t.Setenv("AEGION_ALLOW_EXPERIMENTAL_MODULES", "true")

	dir := t.TempDir()
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
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	loader := NewConfigLoader(configPath)
	cfg, err := loader.LoadModuleConfig("mfa")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.ID != "mfa" {
		t.Fatalf("expected module id mfa, got %q", cfg.ID)
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

	t.Run("returns cyclic dependency error", func(t *testing.T) {
		original := moduleDependencies
		moduleDependencies = map[string][]string{
			"alpha": []string{"beta"},
			"beta":  []string{"alpha"},
		}
		t.Cleanup(func() {
			moduleDependencies = original
		})

		_, err := EnabledModuleOrder(map[string]string{
			"alpha": "latest",
			"beta":  "latest",
		})
		if err == nil || !strings.Contains(err.Error(), "cyclic dependency detected") {
			t.Fatalf("expected cyclic dependency error, got %v", err)
		}
	})
}

func TestValidateModuleDependencies_ProductionModuleMaturity(t *testing.T) {
	t.Run("rejects experimental modules in production by default", func(t *testing.T) {
		t.Setenv("AEGION_ENV", "production")
		t.Setenv("AEGION_ALLOW_EXPERIMENTAL_MODULES", "")

		err := ValidateModuleDependencies(&AegionConfig{
			ModuleVersions: map[string]string{
				"password": "v1.0.0",
				"mfa":      "v1.0.0",
			},
		})
		if !errors.Is(err, ErrExperimentalModule) {
			t.Fatalf("expected ErrExperimentalModule, got %v", err)
		}
		if err == nil || !strings.Contains(err.Error(), `module "mfa"`) {
			t.Fatalf("expected module detail in error, got %v", err)
		}
	})

	t.Run("allows experimental modules with explicit override", func(t *testing.T) {
		t.Setenv("AEGION_ENV", "production")
		t.Setenv("AEGION_ALLOW_EXPERIMENTAL_MODULES", "true")

		err := ValidateModuleDependencies(&AegionConfig{
			ModuleVersions: map[string]string{
				"password": "v1.0.0",
				"mfa":      "v1.0.0",
			},
		})
		if err != nil {
			t.Fatalf("expected nil error with override, got %v", err)
		}
	})

	t.Run("allows production-ready module set in production", func(t *testing.T) {
		t.Setenv("AEGION_ENV", "production")
		t.Setenv("AEGION_ALLOW_EXPERIMENTAL_MODULES", "")

		err := ValidateModuleDependencies(&AegionConfig{
			ModuleVersions: map[string]string{
				"password":   "v1.0.0",
				"oauth2":     "v1.0.0",
				"policy":     "v1.0.0",
				"admin":      "v1.0.0",
				"magic_link": "v1.0.0",
			},
		})
		if err != nil {
			t.Fatalf("expected nil error for production-ready modules, got %v", err)
		}
	})

	t.Run("ignores disabled experimental modules in production", func(t *testing.T) {
		t.Setenv("AEGION_ENV", "production")
		t.Setenv("AEGION_ALLOW_EXPERIMENTAL_MODULES", "")

		err := ValidateModuleDependencies(&AegionConfig{
			ModuleVersions: map[string]string{
				"mfa":      "off",
				"password": "v1.0.0",
			},
		})
		if err != nil {
			t.Fatalf("expected nil error when experimental module is disabled, got %v", err)
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

func TestConfigLoader_AdditionalSuccessAndParseBranches(t *testing.T) {
	t.Run("load wraps config parse failures", func(t *testing.T) {
		dir := t.TempDir()
		configPath := filepath.Join(dir, "aegion.yaml")
		if err := os.WriteFile(configPath, []byte(":\n"), 0o644); err != nil {
			t.Fatalf("failed to write config: %v", err)
		}

		loader := NewConfigLoader(configPath)
		_, err := loader.Load()
		if err == nil || !strings.Contains(err.Error(), "parsing config file") {
			t.Fatalf("expected parsing config file error, got %v", err)
		}
	})

	t.Run("load module config wraps module parse failures", func(t *testing.T) {
		dir := t.TempDir()
		configPath := writeMinimalMainConfig(t, dir)
		modulesDir := filepath.Join(dir, "modules")
		if err := os.MkdirAll(modulesDir, 0o755); err != nil {
			t.Fatalf("failed to create modules dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(modulesDir, "password.yaml"), []byte("{"), 0o644); err != nil {
			t.Fatalf("failed to write invalid module config: %v", err)
		}

		loader := NewConfigLoader(configPath)
		_, err := loader.LoadModuleConfig("password")
		if err == nil || !strings.Contains(err.Error(), "parsing module config") {
			t.Fatalf("expected parsing module config error, got %v", err)
		}
	})

	t.Run("build module config falls back to latest and registry image", func(t *testing.T) {
		loader := &ConfigLoader{
			config: &AegionConfig{
				ModuleVersions: map[string]string{},
				ModuleRegistry: RegistryConfig{
					BaseURL: "ghcr.io/aegion",
				},
			},
		}

		cfg, err := loader.buildModuleConfig("custom")
		if err != nil {
			t.Fatalf("buildModuleConfig returned error: %v", err)
		}
		if cfg.Version != "latest" {
			t.Fatalf("expected default latest version, got %q", cfg.Version)
		}
		if cfg.Image != "ghcr.io/aegion/custom" {
			t.Fatalf("expected registry image, got %q", cfg.Image)
		}
	})

	t.Run("build module config validates required id", func(t *testing.T) {
		loader := &ConfigLoader{
			config: &AegionConfig{
				ModuleVersions: map[string]string{},
			},
		}
		if _, err := loader.buildModuleConfig(""); !errors.Is(err, ErrMissingModuleID) {
			t.Fatalf("expected ErrMissingModuleID, got %v", err)
		}
	})

	t.Run("defaults without main config use hardcoded network", func(t *testing.T) {
		cfg := &ModuleConfig{
			ID:    "demo",
			Name:  "Demo",
			Image: "repo/demo",
		}
		applyModuleDefaults(cfg, nil)
		if cfg.Network != DefaultNetworkName {
			t.Fatalf("expected network %q, got %q", DefaultNetworkName, cfg.Network)
		}
	})

	t.Run("validate module config catches missing id and name", func(t *testing.T) {
		if err := ValidateModuleConfig(&ModuleConfig{Name: "x", Image: "img"}); !errors.Is(err, ErrMissingModuleID) {
			t.Fatalf("expected ErrMissingModuleID, got %v", err)
		}
		if err := ValidateModuleConfig(&ModuleConfig{ID: "x", Image: "img"}); !errors.Is(err, ErrMissingModuleName) {
			t.Fatalf("expected ErrMissingModuleName, got %v", err)
		}
	})

	t.Run("getters return loaded secret and network", func(t *testing.T) {
		dir := t.TempDir()
		configPath := writeMinimalMainConfig(t, dir)
		loader := NewConfigLoader(configPath)

		secret, err := loader.GetInternalSecret()
		if err != nil {
			t.Fatalf("GetInternalSecret returned error: %v", err)
		}
		if secret != "internal-secret" {
			t.Fatalf("expected internal-secret, got %q", secret)
		}

		network, err := loader.GetNetworkConfig()
		if err != nil {
			t.Fatalf("GetNetworkConfig returned error: %v", err)
		}
		if network == nil || network.Name != "aegion_modules" {
			t.Fatalf("unexpected network config: %#v", network)
		}
	})
}
