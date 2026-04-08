// Package orchestrator manages module container lifecycle.
package orchestrator

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var (
	ErrConfigNotFound     = errors.New("configuration file not found")
	ErrInvalidConfig      = errors.New("invalid module configuration")
	ErrMissingImage       = errors.New("module image is required")
	ErrMissingModuleID    = errors.New("module ID is required")
	ErrMissingModuleName  = errors.New("module name is required")
	ErrMissingDependency  = errors.New("module dependency not satisfied")
	ErrExperimentalModule = errors.New("experimental module is not allowed in production")
)

// ModuleConfig defines the configuration for a module container.
type ModuleConfig struct {
	// ID is the unique module identifier
	ID string `yaml:"id"`
	// Name is the human-readable module name
	Name string `yaml:"name"`
	// Image is the Docker image to use
	Image string `yaml:"image"`
	// Version is the image tag (defaults to "latest")
	Version string `yaml:"version"`
	// Environment variables to pass to the container
	Env map[string]string `yaml:"env"`
	// Ports maps container ports to host ports (hostPort:containerPort)
	Ports []PortMapping `yaml:"ports"`
	// Volumes maps host paths to container paths
	Volumes []VolumeMapping `yaml:"volumes"`
	// Resources defines CPU and memory limits
	Resources ResourceConfig `yaml:"resources"`
	// HealthCheck configuration
	HealthCheck HealthCheckConfig `yaml:"health_check"`
	// Network is the Docker network name (defaults to aegion_modules)
	Network string `yaml:"network"`
	// Labels to apply to the container
	Labels map[string]string `yaml:"labels"`
	// RestartPolicy defines container restart behavior
	RestartPolicy string `yaml:"restart_policy"`
	// DependsOn lists modules that must be running first
	DependsOn []string `yaml:"depends_on"`
}

// PortMapping defines a port mapping between host and container.
type PortMapping struct {
	HostPort      string `yaml:"host"`
	ContainerPort string `yaml:"container"`
	Protocol      string `yaml:"protocol"` // tcp or udp, defaults to tcp
}

// VolumeMapping defines a volume mount.
type VolumeMapping struct {
	HostPath      string `yaml:"host"`
	ContainerPath string `yaml:"container"`
	ReadOnly      bool   `yaml:"read_only"`
}

// ResourceConfig defines container resource limits.
type ResourceConfig struct {
	// CPULimit in cores (e.g., "0.5" for half a core)
	CPULimit string `yaml:"cpu_limit"`
	// MemoryLimit (e.g., "256m", "1g")
	MemoryLimit string `yaml:"memory_limit"`
	// CPUReservation is the minimum CPU allocation
	CPUReservation string `yaml:"cpu_reservation"`
	// MemoryReservation is the minimum memory allocation
	MemoryReservation string `yaml:"memory_reservation"`
}

// HealthCheckConfig defines health check parameters.
type HealthCheckConfig struct {
	// Endpoint is the health check URL path (e.g., "/health")
	Endpoint string `yaml:"endpoint"`
	// Interval between health checks
	Interval time.Duration `yaml:"interval"`
	// Timeout for each health check
	Timeout time.Duration `yaml:"timeout"`
	// Retries before marking unhealthy
	Retries int `yaml:"retries"`
	// StartPeriod grace period for startup
	StartPeriod time.Duration `yaml:"start_period"`
}

// AegionConfig represents the complete aegion.yaml configuration.
type AegionConfig struct {
	// ModuleVersions maps module names to versions
	ModuleVersions map[string]string `yaml:"module_versions"`
	// ModuleRegistry configuration
	ModuleRegistry RegistryConfig `yaml:"module_registry"`
	// Server configuration
	Server ServerConfig `yaml:"server"`
	// Secrets configuration
	Secrets SecretsConfig `yaml:"secrets"`
}

// RegistryConfig defines module registry settings.
type RegistryConfig struct {
	BaseURL    string `yaml:"base_url"`
	PullPolicy string `yaml:"pull_policy"`
}

// ServerConfig holds server-related configuration.
type ServerConfig struct {
	InternalNetwork NetworkConfig `yaml:"internal_network"`
}

// NetworkConfig defines internal network settings.
type NetworkConfig struct {
	Name                string        `yaml:"name"`
	Subnet              string        `yaml:"subnet"`
	HealthCheckInterval time.Duration `yaml:"health_check_interval"`
	HealthCheckTimeout  time.Duration `yaml:"health_check_timeout"`
	HealthCheckFailures int           `yaml:"health_check_failures"`
	RestartOnFailure    bool          `yaml:"restart_on_failure"`
	StartupTimeout      time.Duration `yaml:"startup_timeout"`
}

// SecretsConfig holds secret values.
type SecretsConfig struct {
	Cookie   []string `yaml:"cookie"`
	Cipher   []string `yaml:"cipher"`
	Internal []string `yaml:"internal"`
}

// ConfigLoader loads module configurations.
type ConfigLoader struct {
	configPath string
	config     *AegionConfig
}

// NewConfigLoader creates a new configuration loader.
func NewConfigLoader(configPath string) *ConfigLoader {
	return &ConfigLoader{
		configPath: configPath,
	}
}

// Load loads and parses the aegion.yaml configuration.
func (l *ConfigLoader) Load() (*AegionConfig, error) {
	data, err := os.ReadFile(l.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrConfigNotFound
		}
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	var cfg AegionConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	if err := ValidateModuleDependencies(&cfg); err != nil {
		return nil, err
	}

	l.config = &cfg
	return &cfg, nil
}

var moduleDependencies = map[string][]string{
	"password":      {"core"},
	"mfa":           {"core", "password"},
	"passkeys":      {"core"},
	"magic_link":    {"core"},
	"social":        {"core"},
	"sso":           {"core"},
	"oauth2":        {"core"},
	"introspection": {"oauth2"},
	"policy":        {"core"},
	"proxy":         {"core"},
	"admin":         {"core", "policy"},
	"cli":           {"core"},
}

var productionReadyModules = map[string]struct{}{
	"core":       {},
	"password":   {},
	"magic_link": {},
	"oauth2":     {},
	"policy":     {},
	"admin":      {},
}

// ValidateModuleDependencies validates enabled module dependency constraints.
func ValidateModuleDependencies(cfg *AegionConfig) error {
	if cfg == nil {
		return nil
	}

	for module, deps := range moduleDependencies {
		if !isEnabledByVersion(cfg.ModuleVersions[module]) {
			continue
		}
		for _, dep := range deps {
			if dep == "core" || isEnabledByVersion(cfg.ModuleVersions[dep]) {
				continue
			}
			return fmt.Errorf(
				"%w: module %q requires %q",
				ErrMissingDependency,
				module,
				dep,
			)
		}
	}

	if err := validateProductionModuleMaturity(cfg); err != nil {
		return err
	}

	return nil
}

func validateProductionModuleMaturity(cfg *AegionConfig) error {
	if cfg == nil || !isProductionEnvironment() || allowExperimentalModules() {
		return nil
	}
	for moduleID, version := range cfg.ModuleVersions {
		if !isEnabledByVersion(version) {
			continue
		}
		if _, ready := productionReadyModules[moduleID]; ready {
			continue
		}
		return fmt.Errorf(
			"%w: module %q is not production-ready; disable it or set AEGION_ALLOW_EXPERIMENTAL_MODULES=true",
			ErrExperimentalModule,
			moduleID,
		)
	}
	return nil
}

func isProductionEnvironment() bool {
	for _, key := range []string{"AEGION_ENV", "AEGION_ENVIRONMENT"} {
		env := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
		if env == "production" || env == "prod" {
			return true
		}
	}
	return false
}

func allowExperimentalModules() bool {
	override := strings.ToLower(strings.TrimSpace(os.Getenv("AEGION_ALLOW_EXPERIMENTAL_MODULES")))
	return override == "1" || override == "true" || override == "yes" || override == "on"
}

// EnabledModuleOrder returns enabled modules in deterministic dependency order.
func EnabledModuleOrder(moduleVersions map[string]string) ([]string, error) {
	enabled := make(map[string]struct{})
	for moduleID, version := range moduleVersions {
		if !isEnabledByVersion(version) {
			continue
		}
		enabled[moduleID] = struct{}{}
	}
	delete(enabled, "core")

	indegree := make(map[string]int, len(enabled))
	dependents := make(map[string][]string, len(enabled))
	for moduleID := range enabled {
		indegree[moduleID] = 0
	}

	for moduleID := range enabled {
		for _, dep := range moduleDependencies[moduleID] {
			if dep == "core" {
				continue
			}
			if _, ok := enabled[dep]; !ok {
				return nil, fmt.Errorf(
					"%w: module %q requires %q",
					ErrMissingDependency,
					moduleID,
					dep,
				)
			}
			indegree[moduleID]++
			dependents[dep] = append(dependents[dep], moduleID)
		}
	}

	var ready []string
	for moduleID, count := range indegree {
		if count == 0 {
			ready = append(ready, moduleID)
		}
	}
	slices.Sort(ready)

	order := make([]string, 0, len(enabled))
	for len(ready) > 0 {
		current := ready[0]
		ready = ready[1:]
		order = append(order, current)

		nextModules := dependents[current]
		slices.Sort(nextModules)
		for _, next := range nextModules {
			indegree[next]--
			if indegree[next] == 0 {
				ready = append(ready, next)
			}
		}
		slices.Sort(ready)
	}

	if len(order) != len(enabled) {
		return nil, fmt.Errorf("failed to resolve module dependency order: cyclic dependency detected")
	}

	return order, nil
}

func isEnabledByVersion(version string) bool {
	normalized := strings.ToLower(strings.TrimSpace(version))
	switch normalized {
	case "", "disabled", "disable", "off", "false", "0", "none":
		return false
	default:
		return true
	}
}

// LoadModuleConfig loads configuration for a specific module.
func (l *ConfigLoader) LoadModuleConfig(moduleID string) (*ModuleConfig, error) {
	if l.config == nil {
		if _, err := l.Load(); err != nil {
			return nil, err
		}
	}

	// Look for module-specific config file
	modulePath := fmt.Sprintf("%s/modules/%s.yaml", l.configPath[:len(l.configPath)-len("/aegion.yaml")], moduleID)
	data, err := os.ReadFile(modulePath)
	if err != nil {
		if os.IsNotExist(err) {
			// Build config from main aegion.yaml
			return l.buildModuleConfig(moduleID)
		}
		return nil, fmt.Errorf("reading module config: %w", err)
	}

	var cfg ModuleConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing module config: %w", err)
	}

	// Apply defaults
	applyModuleDefaults(&cfg, l.config)

	if err := ValidateModuleConfig(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// buildModuleConfig creates a module configuration from aegion.yaml.
func (l *ConfigLoader) buildModuleConfig(moduleID string) (*ModuleConfig, error) {
	version, ok := l.config.ModuleVersions[moduleID]
	if !ok {
		version = "latest"
	}

	image := moduleID
	if l.config != nil && strings.TrimSpace(l.config.ModuleRegistry.BaseURL) != "" {
		base := strings.TrimRight(strings.TrimSpace(l.config.ModuleRegistry.BaseURL), "/")
		image = fmt.Sprintf("%s/%s", base, moduleID)
	}

	cfg := &ModuleConfig{
		ID:      moduleID,
		Name:    moduleID,
		Image:   image,
		Version: version,
		Env:     make(map[string]string),
		Labels:  make(map[string]string),
	}

	applyModuleDefaults(cfg, l.config)

	if err := ValidateModuleConfig(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// applyModuleDefaults applies default values from main config.
func applyModuleDefaults(cfg *ModuleConfig, mainCfg *AegionConfig) {
	if cfg.Network == "" {
		if mainCfg != nil && strings.TrimSpace(mainCfg.Server.InternalNetwork.Name) != "" {
			cfg.Network = strings.TrimSpace(mainCfg.Server.InternalNetwork.Name)
		} else {
			cfg.Network = DefaultNetworkName
		}
	}

	if cfg.HealthCheck.Endpoint == "" {
		cfg.HealthCheck.Endpoint = "/health"
	}
	if cfg.HealthCheck.Interval == 0 {
		if mainCfg != nil && mainCfg.Server.InternalNetwork.HealthCheckInterval > 0 {
			cfg.HealthCheck.Interval = mainCfg.Server.InternalNetwork.HealthCheckInterval
		} else {
			cfg.HealthCheck.Interval = 5 * time.Second
		}
	}
	if cfg.HealthCheck.Timeout == 0 {
		if mainCfg != nil && mainCfg.Server.InternalNetwork.HealthCheckTimeout > 0 {
			cfg.HealthCheck.Timeout = mainCfg.Server.InternalNetwork.HealthCheckTimeout
		} else {
			cfg.HealthCheck.Timeout = 2 * time.Second
		}
	}
	if cfg.HealthCheck.Retries == 0 {
		if mainCfg != nil && mainCfg.Server.InternalNetwork.HealthCheckFailures > 0 {
			cfg.HealthCheck.Retries = mainCfg.Server.InternalNetwork.HealthCheckFailures
		} else {
			cfg.HealthCheck.Retries = 3
		}
	}
	if cfg.HealthCheck.StartPeriod == 0 {
		if mainCfg != nil && mainCfg.Server.InternalNetwork.StartupTimeout > 0 {
			cfg.HealthCheck.StartPeriod = mainCfg.Server.InternalNetwork.StartupTimeout
		} else {
			cfg.HealthCheck.StartPeriod = 30 * time.Second
		}
	}

	if cfg.RestartPolicy == "" {
		cfg.RestartPolicy = "unless-stopped"
	}

	if cfg.Labels == nil {
		cfg.Labels = make(map[string]string)
	}
	cfg.Labels["aegion.module"] = "true"
	cfg.Labels["aegion.module.id"] = cfg.ID
}

// ValidateModuleConfig validates a module configuration.
func ValidateModuleConfig(cfg *ModuleConfig) error {
	if cfg.ID == "" {
		return ErrMissingModuleID
	}
	if cfg.Name == "" {
		return ErrMissingModuleName
	}
	if cfg.Image == "" {
		return ErrMissingImage
	}
	return nil
}

// GetInternalSecret returns the internal auth secret.
func (l *ConfigLoader) GetInternalSecret() (string, error) {
	if l.config == nil {
		if _, err := l.Load(); err != nil {
			return "", err
		}
	}

	if len(l.config.Secrets.Internal) == 0 {
		return "", errors.New("no internal secret configured")
	}

	return l.config.Secrets.Internal[0], nil
}

// GetNetworkConfig returns the internal network configuration.
func (l *ConfigLoader) GetNetworkConfig() (*NetworkConfig, error) {
	if l.config == nil {
		if _, err := l.Load(); err != nil {
			return nil, err
		}
	}

	return &l.config.Server.InternalNetwork, nil
}
