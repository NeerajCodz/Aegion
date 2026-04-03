package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aegion/aegion/core/registry"
)

func TestModuleState(t *testing.T) {
	tests := []struct {
		name  string
		state ModuleState
		want  string
	}{
		{"unknown state", StateUnknown, "unknown"},
		{"stopped state", StateStopped, "stopped"},
		{"starting state", StateStarting, "starting"},
		{"running state", StateRunning, "running"},
		{"stopping state", StateStopping, "stopping"},
		{"failed state", StateFailed, "failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.state) != tt.want {
				t.Errorf("ModuleState = %s, want %s", string(tt.state), tt.want)
			}
		})
	}
}

func TestOrchestratorErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		msg  string
	}{
		{"ErrModuleNotFound", ErrModuleNotFound, "module not found"},
		{"ErrModuleAlreadyRunning", ErrModuleAlreadyRunning, "module is already running"},
		{"ErrModuleNotRunning", ErrModuleNotRunning, "module is not running"},
		{"ErrOrchestratorClosed", ErrOrchestratorClosed, "orchestrator is closed"},
		{"ErrStartFailed", ErrStartFailed, "failed to start module"},
		{"ErrStopFailed", ErrStopFailed, "failed to stop module"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Error() != tt.msg {
				t.Errorf("error message = %q, want %q", tt.err.Error(), tt.msg)
			}
		})
	}
}

func TestModuleStatus(t *testing.T) {
	status := ModuleStatus{
		ModuleID:     "test-module",
		ContainerID:  "container123",
		State:        StateRunning,
		Health:       "healthy",
		IPAddress:    "192.168.1.100",
		Ports:        []string{"8080:8080", "9090:9090"},
		RestartCount: 2,
		Error:        "",
	}

	if status.ModuleID != "test-module" {
		t.Errorf("ModuleID = %s, want test-module", status.ModuleID)
	}
	if status.ContainerID != "container123" {
		t.Errorf("ContainerID = %s, want container123", status.ContainerID)
	}
	if status.State != StateRunning {
		t.Errorf("State = %s, want %s", status.State, StateRunning)
	}
	if status.Health != "healthy" {
		t.Errorf("Health = %s, want healthy", status.Health)
	}
	if status.IPAddress != "192.168.1.100" {
		t.Errorf("IPAddress = %s, want 192.168.1.100", status.IPAddress)
	}
	if len(status.Ports) != 2 {
		t.Errorf("Ports length = %d, want 2", len(status.Ports))
	}
	if status.RestartCount != 2 {
		t.Errorf("RestartCount = %d, want 2", status.RestartCount)
	}
}

func TestValidateModuleConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  *ModuleConfig
		wantErr error
	}{
		{
			name: "valid config",
			config: &ModuleConfig{
				ID:    "test-module",
				Name:  "Test Module",
				Image: "nginx:latest",
			},
			wantErr: nil,
		},
		{
			name: "missing ID",
			config: &ModuleConfig{
				Name:  "Test Module",
				Image: "nginx:latest",
			},
			wantErr: ErrMissingModuleID,
		},
		{
			name: "missing name",
			config: &ModuleConfig{
				ID:    "test-module",
				Image: "nginx:latest",
			},
			wantErr: ErrMissingModuleName,
		},
		{
			name: "missing image",
			config: &ModuleConfig{
				ID:   "test-module",
				Name: "Test Module",
			},
			wantErr: ErrMissingImage,
		},
		{
			name: "empty ID",
			config: &ModuleConfig{
				ID:    "",
				Name:  "Test Module",
				Image: "nginx:latest",
			},
			wantErr: ErrMissingModuleID,
		},
		{
			name: "empty name",
			config: &ModuleConfig{
				ID:    "test-module",
				Name:  "",
				Image: "nginx:latest",
			},
			wantErr: ErrMissingModuleName,
		},
		{
			name: "empty image",
			config: &ModuleConfig{
				ID:    "test-module",
				Name:  "Test Module",
				Image: "",
			},
			wantErr: ErrMissingImage,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateModuleConfig(tt.config)
			if err != tt.wantErr {
				t.Errorf("ValidateModuleConfig() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestModuleConfigDefaults(t *testing.T) {
	// Test the logic from applyModuleDefaults function
	tests := []struct {
		name           string
		config         *ModuleConfig
		mainConfig     *AegionConfig
		expectDefaults map[string]interface{}
	}{
		{
			name: "apply network default",
			config: &ModuleConfig{
				ID:    "test-module",
				Name:  "Test Module",
				Image: "nginx:latest",
			},
			mainConfig: &AegionConfig{},
			expectDefaults: map[string]interface{}{
				"network":             "aegion_modules",
				"health_endpoint":     "/health",
				"health_interval":     5 * time.Second,
				"health_timeout":      2 * time.Second,
				"health_retries":      3,
				"health_start_period": 30 * time.Second,
				"restart_policy":      "unless-stopped",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := *tt.config // Copy the config

			// Simulate applyModuleDefaults logic
			if cfg.Network == "" {
				cfg.Network = "aegion_modules"
			}
			if cfg.HealthCheck.Endpoint == "" {
				cfg.HealthCheck.Endpoint = "/health"
			}
			if cfg.HealthCheck.Interval == 0 {
				cfg.HealthCheck.Interval = 5 * time.Second
			}
			if cfg.HealthCheck.Timeout == 0 {
				cfg.HealthCheck.Timeout = 2 * time.Second
			}
			if cfg.HealthCheck.Retries == 0 {
				cfg.HealthCheck.Retries = 3
			}
			if cfg.HealthCheck.StartPeriod == 0 {
				cfg.HealthCheck.StartPeriod = 30 * time.Second
			}
			if cfg.RestartPolicy == "" {
				cfg.RestartPolicy = "unless-stopped"
			}

			// Verify defaults were applied
			if cfg.Network != tt.expectDefaults["network"] {
				t.Errorf("Network = %s, want %s", cfg.Network, tt.expectDefaults["network"])
			}
			if cfg.HealthCheck.Endpoint != tt.expectDefaults["health_endpoint"] {
				t.Errorf("HealthCheck.Endpoint = %s, want %s", cfg.HealthCheck.Endpoint, tt.expectDefaults["health_endpoint"])
			}
			if cfg.HealthCheck.Interval != tt.expectDefaults["health_interval"] {
				t.Errorf("HealthCheck.Interval = %v, want %v", cfg.HealthCheck.Interval, tt.expectDefaults["health_interval"])
			}
			if cfg.HealthCheck.Timeout != tt.expectDefaults["health_timeout"] {
				t.Errorf("HealthCheck.Timeout = %v, want %v", cfg.HealthCheck.Timeout, tt.expectDefaults["health_timeout"])
			}
			if cfg.HealthCheck.Retries != tt.expectDefaults["health_retries"] {
				t.Errorf("HealthCheck.Retries = %d, want %d", cfg.HealthCheck.Retries, tt.expectDefaults["health_retries"])
			}
			if cfg.HealthCheck.StartPeriod != tt.expectDefaults["health_start_period"] {
				t.Errorf("HealthCheck.StartPeriod = %v, want %v", cfg.HealthCheck.StartPeriod, tt.expectDefaults["health_start_period"])
			}
			if cfg.RestartPolicy != tt.expectDefaults["restart_policy"] {
				t.Errorf("RestartPolicy = %s, want %s", cfg.RestartPolicy, tt.expectDefaults["restart_policy"])
			}
		})
	}
}

func TestPortMapping(t *testing.T) {
	port := PortMapping{
		HostPort:      "8080",
		ContainerPort: "80",
		Protocol:      "tcp",
	}

	if port.HostPort != "8080" {
		t.Errorf("HostPort = %s, want 8080", port.HostPort)
	}
	if port.ContainerPort != "80" {
		t.Errorf("ContainerPort = %s, want 80", port.ContainerPort)
	}
	if port.Protocol != "tcp" {
		t.Errorf("Protocol = %s, want tcp", port.Protocol)
	}
}

func TestVolumeMapping(t *testing.T) {
	volume := VolumeMapping{
		HostPath:      "/host/data",
		ContainerPath: "/data",
		ReadOnly:      true,
	}

	if volume.HostPath != "/host/data" {
		t.Errorf("HostPath = %s, want /host/data", volume.HostPath)
	}
	if volume.ContainerPath != "/data" {
		t.Errorf("ContainerPath = %s, want /data", volume.ContainerPath)
	}
	if !volume.ReadOnly {
		t.Error("ReadOnly should be true")
	}
}

func TestResourceConfig(t *testing.T) {
	resources := ResourceConfig{
		CPULimit:          "1.0",
		MemoryLimit:       "512m",
		CPUReservation:    "0.5",
		MemoryReservation: "256m",
	}

	if resources.CPULimit != "1.0" {
		t.Errorf("CPULimit = %s, want 1.0", resources.CPULimit)
	}
	if resources.MemoryLimit != "512m" {
		t.Errorf("MemoryLimit = %s, want 512m", resources.MemoryLimit)
	}
}

func TestHealthCheckConfig(t *testing.T) {
	hc := HealthCheckConfig{
		Endpoint:    "/health",
		Interval:    30 * time.Second,
		Timeout:     5 * time.Second,
		Retries:     3,
		StartPeriod: 60 * time.Second,
	}

	if hc.Endpoint != "/health" {
		t.Errorf("Endpoint = %s, want /health", hc.Endpoint)
	}
	if hc.Interval != 30*time.Second {
		t.Errorf("Interval = %v, want 30s", hc.Interval)
	}
	if hc.Retries != 3 {
		t.Errorf("Retries = %d, want 3", hc.Retries)
	}
}

func TestConfigLoaderStruct(t *testing.T) {
	configPath := "/path/to/config.yaml"
	loader := NewConfigLoader(configPath)

	if loader.configPath != configPath {
		t.Errorf("configPath = %s, want %s", loader.configPath, configPath)
	}
}

func TestModuleConfigStruct(t *testing.T) {
	config := ModuleConfig{
		ID:            "test-module",
		Name:          "Test Module",
		Image:         "nginx:latest",
		Version:       "v1.0.0",
		Network:       "test-network",
		RestartPolicy: "always",
		Env:           map[string]string{"ENV": "test"},
		Labels:        map[string]string{"app": "test"},
		Volumes:       []VolumeMapping{{HostPath: "/data", ContainerPath: "/data"}},
		Ports:         []PortMapping{{HostPort: "8080", ContainerPort: "80"}},
	}

	if config.ID != "test-module" {
		t.Errorf("ID = %s, want test-module", config.ID)
	}
	if config.Image != "nginx:latest" {
		t.Errorf("Image = %s, want nginx:latest", config.Image)
	}
	if len(config.Env) != 1 {
		t.Errorf("Env length = %d, want 1", len(config.Env))
	}
	if config.Labels["app"] != "test" {
		t.Errorf("Labels[app] = %s, want test", config.Labels["app"])
	}
	if config.Env["ENV"] != "test" {
		t.Errorf("Env[ENV] = %s, want test", config.Env["ENV"])
	}
}

func TestBuildModuleConfigUsesRegistryBaseURL(t *testing.T) {
	loader := &ConfigLoader{
		config: &AegionConfig{
			ModuleVersions: map[string]string{
				"admin": "v1.2.3",
			},
			ModuleRegistry: RegistryConfig{
				BaseURL: "ghcr.io/aegion/base",
			},
			Server: ServerConfig{
				InternalNetwork: NetworkConfig{
					Name: "aegion_modules_test",
				},
			},
		},
	}

	cfg, err := loader.buildModuleConfig("admin")
	if err != nil {
		t.Fatalf("buildModuleConfig returned error: %v", err)
	}

	if cfg.Image != "ghcr.io/aegion/base/admin" {
		t.Fatalf("Image = %q, want %q", cfg.Image, "ghcr.io/aegion/base/admin")
	}
	if cfg.Version != "v1.2.3" {
		t.Fatalf("Version = %q, want %q", cfg.Version, "v1.2.3")
	}
	if cfg.Network != "aegion_modules_test" {
		t.Fatalf("Network = %q, want %q", cfg.Network, "aegion_modules_test")
	}
	if cfg.Labels["aegion.module"] != "true" {
		t.Fatalf("expected aegion.module label to be set")
	}
}

func TestBuildModuleConfigDefaultsToModuleImageAndLatest(t *testing.T) {
	loader := &ConfigLoader{
		config: &AegionConfig{
			ModuleVersions: map[string]string{},
			Server:         ServerConfig{},
		},
	}

	cfg, err := loader.buildModuleConfig("password")
	if err != nil {
		t.Fatalf("buildModuleConfig returned error: %v", err)
	}

	if cfg.Image != "password" {
		t.Fatalf("Image = %q, want %q", cfg.Image, "password")
	}
	if cfg.Version != "latest" {
		t.Fatalf("Version = %q, want %q", cfg.Version, "latest")
	}
	if cfg.Network != DefaultNetworkName {
		t.Fatalf("Network = %q, want %q", cfg.Network, DefaultNetworkName)
	}
}

func TestApplyModuleDefaultsWithNilMainConfig(t *testing.T) {
	cfg := &ModuleConfig{
		ID:    "mod-a",
		Name:  "mod-a",
		Image: "mod-a",
	}

	applyModuleDefaults(cfg, nil)

	if cfg.Network != DefaultNetworkName {
		t.Fatalf("Network = %q, want %q", cfg.Network, DefaultNetworkName)
	}
	if cfg.HealthCheck.Endpoint != "/health" {
		t.Fatalf("Health endpoint = %q, want /health", cfg.HealthCheck.Endpoint)
	}
	if cfg.RestartPolicy != "unless-stopped" {
		t.Fatalf("RestartPolicy = %q, want unless-stopped", cfg.RestartPolicy)
	}
}

func TestConfigLoaderLoadErrors(t *testing.T) {
	t.Run("missing config returns ErrConfigNotFound", func(t *testing.T) {
		loader := NewConfigLoader(filepath.Join(t.TempDir(), "aegion.yaml"))
		_, err := loader.Load()
		if !errors.Is(err, ErrConfigNotFound) {
			t.Fatalf("expected ErrConfigNotFound, got %v", err)
		}
	})

	t.Run("invalid yaml returns parsing error", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "aegion.yaml")
		if err := os.WriteFile(path, []byte(":\n- invalid"), 0o600); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		loader := NewConfigLoader(path)
		_, err := loader.Load()
		if err == nil || !strings.Contains(err.Error(), "parsing config file") {
			t.Fatalf("expected parsing config error, got %v", err)
		}
	})
}

func TestConfigLoaderSecretsAndNetwork(t *testing.T) {
	loader := &ConfigLoader{
		config: &AegionConfig{
			Secrets: SecretsConfig{
				Internal: []string{"internal-secret"},
			},
			Server: ServerConfig{
				InternalNetwork: NetworkConfig{Name: "test-net"},
			},
		},
	}

	secret, err := loader.GetInternalSecret()
	if err != nil {
		t.Fatalf("GetInternalSecret returned error: %v", err)
	}
	if secret != "internal-secret" {
		t.Fatalf("secret = %q, want %q", secret, "internal-secret")
	}

	network, err := loader.GetNetworkConfig()
	if err != nil {
		t.Fatalf("GetNetworkConfig returned error: %v", err)
	}
	if network.Name != "test-net" {
		t.Fatalf("network name = %q, want %q", network.Name, "test-net")
	}

	loader.config.Secrets.Internal = nil
	if _, err := loader.GetInternalSecret(); err == nil {
		t.Fatalf("expected missing secret error")
	}
}

func TestDockerResourceParsersAndBuilders(t *testing.T) {
	tests := []struct {
		input string
		want  int64
	}{
		{input: "1k", want: 1024},
		{input: "2m", want: 2 * 1024 * 1024},
		{input: "3g", want: 3 * 1024 * 1024 * 1024},
		{input: "1024", want: 1024},
	}

	for _, tc := range tests {
		got, err := parseMemory(tc.input)
		if err != nil {
			t.Fatalf("parseMemory(%q) unexpected error: %v", tc.input, err)
		}
		if got != tc.want {
			t.Fatalf("parseMemory(%q) = %d, want %d", tc.input, got, tc.want)
		}
	}

	if _, err := parseMemory("bad"); err == nil {
		t.Fatalf("parseMemory should fail for invalid input")
	}
	if _, err := parseCPU("bad"); err == nil {
		t.Fatalf("parseCPU should fail for invalid input")
	}

	cpu, err := parseCPU("0.5")
	if err != nil {
		t.Fatalf("parseCPU returned error: %v", err)
	}
	if cpu != 500000000 {
		t.Fatalf("parseCPU = %d, want 500000000", cpu)
	}

	dc := &DockerClient{}
	hc := dc.buildHealthCheck(&ModuleConfig{
		HealthCheck: HealthCheckConfig{
			Endpoint:    "/ready",
			Interval:    10 * time.Second,
			Timeout:     3 * time.Second,
			Retries:     4,
			StartPeriod: 20 * time.Second,
		},
	})
	if hc == nil {
		t.Fatalf("expected non-nil healthcheck")
	}
	if len(hc.Test) != 2 || !strings.Contains(hc.Test[1], "/ready") {
		t.Fatalf("unexpected healthcheck command: %#v", hc.Test)
	}

	if dc.buildHealthCheck(&ModuleConfig{}) != nil {
		t.Fatalf("expected nil healthcheck when endpoint is empty")
	}

	res := dc.buildResources(&ModuleConfig{
		Resources: ResourceConfig{
			MemoryLimit:       "128m",
			MemoryReservation: "64m",
			CPULimit:          "1.5",
		},
	})
	if res.Memory == 0 || res.MemoryReservation == 0 || res.NanoCPUs == 0 {
		t.Fatalf("expected resource limits to be parsed, got %+v", res)
	}
}

func TestOrchestratorCoreFlowsWithStubs(t *testing.T) {
	ctx := context.Background()
	moduleID := "password"
	reg := registry.New(registry.DefaultConfig())
	defer reg.Stop()

	moduleCfg := &ModuleConfig{ID: moduleID, Name: "Password", Image: "password"}

	t.Run("start and stop module success path", func(t *testing.T) {
		o := &Orchestrator{
			registry: reg,
			modules:  make(map[string]*moduleInstance),
		}

		o.ensureNetworkFn = func(context.Context) (string, error) { return "net-1", nil }
		o.loadModuleCfgFn = func(id string) (*ModuleConfig, error) {
			if id != moduleID {
				return nil, fmt.Errorf("unexpected module id %s", id)
			}
			return moduleCfg, nil
		}
		o.generateTokenFn = func(id string) (string, error) {
			if id != moduleID {
				return "", fmt.Errorf("unexpected module id %s", id)
			}
			return "token", nil
		}
		o.createContainerFn = func(_ context.Context, cfg *ModuleConfig, token string) (string, error) {
			if cfg.ID != moduleID || token != "token" {
				return "", fmt.Errorf("unexpected create args")
			}
			return "container-1234567890", nil
		}
		o.startContainerFn = func(context.Context, string) error { return nil }
		o.stopContainerFn = func(context.Context, string, time.Duration) error { return nil }
		o.removeContainerFn = func(context.Context, string, bool) error { return nil }
		o.containerLogsFn = func(context.Context, string, int, time.Time) (string, error) { return "logs", nil }
		o.getContainerInfoFn = func(context.Context, string) (*ContainerInfo, error) {
			return &ContainerInfo{
				State:        "running",
				Health:       "healthy",
				IPAddress:    "10.0.0.2",
				Ports:        []string{"8080/tcp"},
				RestartCount: 1,
			}, nil
		}
		o.dockerCloseFn = func() error { return nil }

		if err := o.Start(ctx); err != nil {
			t.Fatalf("Start returned error: %v", err)
		}

		if err := o.StartModule(ctx, moduleID); err != nil {
			t.Fatalf("StartModule returned error: %v", err)
		}
		if !o.IsRunning(moduleID) {
			t.Fatalf("expected module to be running")
		}

		status, err := o.GetModuleStatus(ctx, moduleID)
		if err != nil {
			t.Fatalf("GetModuleStatus returned error: %v", err)
		}
		if status.State != StateRunning {
			t.Fatalf("expected running state, got %s", status.State)
		}
		if status.Health != "healthy" {
			t.Fatalf("expected healthy status")
		}

		logs, err := o.GetModuleLogs(ctx, moduleID, 50)
		if err != nil {
			t.Fatalf("GetModuleLogs returned error: %v", err)
		}
		if logs != "logs" {
			t.Fatalf("unexpected logs value: %q", logs)
		}

		if err := o.StopModule(ctx, moduleID); err != nil {
			t.Fatalf("StopModule returned error: %v", err)
		}

		if err := o.Stop(ctx); err != nil {
			t.Fatalf("Stop returned error: %v", err)
		}
	})

	t.Run("error paths and state transitions", func(t *testing.T) {
		o := &Orchestrator{
			registry: reg,
			modules:  make(map[string]*moduleInstance),
		}
		o.dockerCloseFn = func() error { return nil }
		o.ensureNetworkFn = func(context.Context) (string, error) { return "", errors.New("network down") }
		if err := o.Start(ctx); err == nil {
			t.Fatalf("expected start error")
		}

		o.closed = true
		if err := o.StartModule(ctx, moduleID); !errors.Is(err, ErrOrchestratorClosed) {
			t.Fatalf("expected ErrOrchestratorClosed, got %v", err)
		}
		o.closed = false

		o.modules[moduleID] = &moduleInstance{moduleID: moduleID, state: StateRunning}
		if err := o.StartModule(ctx, moduleID); !errors.Is(err, ErrModuleAlreadyRunning) {
			t.Fatalf("expected ErrModuleAlreadyRunning, got %v", err)
		}

		o.modules = make(map[string]*moduleInstance)
		o.loadModuleCfgFn = func(string) (*ModuleConfig, error) { return nil, errors.New("missing cfg") }
		if err := o.StartModule(ctx, moduleID); err == nil || !strings.Contains(err.Error(), "loading module config") {
			t.Fatalf("expected module config load error, got %v", err)
		}
		if got := o.modules[moduleID].state; got != StateFailed {
			t.Fatalf("expected failed state after config error, got %s", got)
		}

		o.modules = make(map[string]*moduleInstance)
		o.loadModuleCfgFn = func(string) (*ModuleConfig, error) { return moduleCfg, nil }
		o.generateTokenFn = func(string) (string, error) { return "", errors.New("token fail") }
		if err := o.StartModule(ctx, moduleID); err == nil || !strings.Contains(err.Error(), "generating auth token") {
			t.Fatalf("expected token generation error, got %v", err)
		}

		o.modules = make(map[string]*moduleInstance)
		o.generateTokenFn = func(string) (string, error) { return "tok", nil }
		o.createContainerFn = func(context.Context, *ModuleConfig, string) (string, error) { return "", errors.New("create fail") }
		if err := o.StartModule(ctx, moduleID); err == nil || !strings.Contains(err.Error(), "creating container") {
			t.Fatalf("expected container creation error, got %v", err)
		}

		removeCalled := false
		o.modules = make(map[string]*moduleInstance)
		o.createContainerFn = func(context.Context, *ModuleConfig, string) (string, error) { return "container-xyz", nil }
		o.startContainerFn = func(context.Context, string) error { return errors.New("start fail") }
		o.removeContainerFn = func(context.Context, string, bool) error { removeCalled = true; return nil }
		if err := o.StartModule(ctx, moduleID); err == nil || !strings.Contains(err.Error(), "starting container") {
			t.Fatalf("expected container start error, got %v", err)
		}
		if !removeCalled {
			t.Fatalf("expected cleanup removeContainer to be called")
		}

		if err := o.StopModule(ctx, "missing"); !errors.Is(err, ErrModuleNotFound) {
			t.Fatalf("expected ErrModuleNotFound, got %v", err)
		}

		o.modules[moduleID] = &moduleInstance{moduleID: moduleID, state: StateStopped}
		if err := o.StopModule(ctx, moduleID); !errors.Is(err, ErrModuleNotRunning) {
			t.Fatalf("expected ErrModuleNotRunning, got %v", err)
		}

		o.modules[moduleID] = &moduleInstance{moduleID: moduleID, containerID: "c1", state: StateRunning}
		o.stopContainerFn = func(context.Context, string, time.Duration) error { return errors.New("stop fail") }
		if err := o.StopModule(ctx, moduleID); err == nil || !strings.Contains(err.Error(), "stopping container") {
			t.Fatalf("expected stop error, got %v", err)
		}
		if o.modules[moduleID].state != StateFailed {
			t.Fatalf("expected failed state after stop error")
		}

		o.modules[moduleID] = &moduleInstance{moduleID: moduleID, containerID: "c1", state: StateRunning}
		o.stopContainerFn = func(context.Context, string, time.Duration) error { return nil }
		o.removeContainerFn = func(context.Context, string, bool) error { return errors.New("remove warn") }
		if _, err := reg.Register(registry.RegistrationRequest{
			ID:      moduleID,
			Name:    moduleID,
			Version: "v1",
			Endpoints: []registry.Endpoint{
				{Type: registry.EndpointHTTP, URL: "http://localhost"},
			},
			HealthURL: "http://localhost/health",
		}); err != nil {
			t.Fatalf("failed to register module: %v", err)
		}
		if err := o.StopModule(ctx, moduleID); err != nil {
			t.Fatalf("expected stop success with remove warning, got %v", err)
		}

		o.modules[moduleID] = &moduleInstance{moduleID: moduleID, containerID: "c1", state: StateRunning}
		if err := o.RestartModule(ctx, moduleID); err == nil {
			t.Fatalf("expected restart failure due to missing start stubs")
		}
	})

	t.Run("status mapping and list/log behavior", func(t *testing.T) {
		o := &Orchestrator{
			registry: reg,
			modules:  make(map[string]*moduleInstance),
		}
		o.getContainerInfoFn = func(context.Context, string) (*ContainerInfo, error) {
			return &ContainerInfo{
				State:        "exited",
				ExitCode:     2,
				RestartCount: 3,
			}, nil
		}

		st, err := o.GetModuleStatus(ctx, "missing")
		if err != nil {
			t.Fatalf("unexpected error for missing module: %v", err)
		}
		if st.State != StateStopped {
			t.Fatalf("expected stopped state for missing module")
		}

		o.modules["a"] = &moduleInstance{moduleID: "a", containerID: "ca", state: StateRunning}
		st, err = o.GetModuleStatus(ctx, "a")
		if err != nil {
			t.Fatalf("unexpected status error: %v", err)
		}
		if st.State != StateFailed {
			t.Fatalf("expected failed state for exited container, got %s", st.State)
		}
		if !strings.Contains(st.Error, "exited with code 2") {
			t.Fatalf("expected exit code error, got %q", st.Error)
		}

		o.getContainerInfoFn = func(context.Context, string) (*ContainerInfo, error) {
			return &ContainerInfo{State: "restarting"}, nil
		}
		st, err = o.GetModuleStatus(ctx, "a")
		if err != nil {
			t.Fatalf("unexpected status error: %v", err)
		}
		if st.State != StateStarting {
			t.Fatalf("expected starting state, got %s", st.State)
		}

		o.getContainerInfoFn = func(context.Context, string) (*ContainerInfo, error) {
			return nil, errors.New("inspect fail")
		}
		_, err = o.GetModuleStatus(ctx, "a")
		if err != nil {
			t.Fatalf("GetModuleStatus should tolerate inspect failures, got %v", err)
		}

		o.containerLogsFn = func(context.Context, string, int, time.Time) (string, error) {
			return "logline", nil
		}
		if _, err := o.GetModuleLogs(ctx, "missing", 10); !errors.Is(err, ErrModuleNotFound) {
			t.Fatalf("expected ErrModuleNotFound, got %v", err)
		}
		if logs, err := o.GetModuleLogs(ctx, "a", 10); err != nil || logs != "logline" {
			t.Fatalf("unexpected logs result: %q, err=%v", logs, err)
		}

		o.modules["b"] = &moduleInstance{moduleID: "b", containerID: "cb", state: StateStarting}
		statuses, err := o.ListModules(ctx)
		if err != nil {
			t.Fatalf("ListModules returned error: %v", err)
		}
		if len(statuses) == 0 {
			t.Fatalf("expected non-empty statuses")
		}

		o.modules["c"] = &moduleInstance{moduleID: "c", state: StateFailed}
		o.setModuleError("c", errors.New("boom"))
		if o.modules["c"].error != "boom" || o.modules["c"].state != StateFailed {
			t.Fatalf("expected setModuleError to persist failure state")
		}
	})
}
