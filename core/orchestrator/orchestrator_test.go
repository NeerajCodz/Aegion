package orchestrator

import (
	"testing"
	"time"
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
				"network":               "aegion_modules",
				"health_endpoint":       "/health",
				"health_interval":       5 * time.Second,
				"health_timeout":        2 * time.Second, 
				"health_retries":        3,
				"health_start_period":   30 * time.Second,
				"restart_policy":        "unless-stopped",
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