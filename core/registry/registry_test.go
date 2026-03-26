package registry

import (
	"fmt"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name   string
		config Config
	}{
		{
			"default config",
			DefaultConfig(),
		},
		{
			"custom config",
			Config{
				HealthCheckInterval: 60 * time.Second,
				HealthCheckTimeout:  10 * time.Second,
			},
		},
		{
			"zero config gets defaults",
			Config{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := New(tt.config)
			if registry == nil {
				t.Fatal("New() returned nil registry")
			}
			if registry.modules == nil {
				t.Error("modules map is nil")
			}
			if registry.healthChecker == nil {
				t.Error("health checker is nil")
			}
			if registry.discovery == nil {
				t.Error("discovery is nil")
			}
		})
	}
}

func TestRegister(t *testing.T) {
	registry := New(DefaultConfig())

	tests := []struct {
		name    string
		req     RegistrationRequest
		wantErr error
	}{
		{
			name: "valid registration",
			req: RegistrationRequest{
				ID:        "test-module",
				Name:      "Test Module",
				Version:   "1.0.0",
				Endpoints: []Endpoint{{Type: EndpointHTTP, URL: "http://localhost:8080"}},
				HealthURL: "http://localhost:8080/health",
			},
			wantErr: nil,
		},
		{
			name: "missing ID",
			req: RegistrationRequest{
				Name:      "Test Module",
				Endpoints: []Endpoint{{Type: EndpointHTTP, URL: "http://localhost:8080"}},
			},
			wantErr: ErrInvalidModule,
		},
		{
			name: "missing name",
			req: RegistrationRequest{
				ID:        "test-module",
				Endpoints: []Endpoint{{Type: EndpointHTTP, URL: "http://localhost:8080"}},
			},
			wantErr: ErrInvalidModule,
		},
		{
			name: "no endpoints",
			req: RegistrationRequest{
				ID:   "test-module",
				Name: "Test Module",
			},
			wantErr: ErrInvalidModule,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := registry.Register(tt.req)
			
			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Errorf("Register() error = %v, want %v", err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Errorf("Register() unexpected error = %v", err)
				return
			}

			if resp == nil {
				t.Error("Register() returned nil response")
				return
			}

			if !resp.Success {
				t.Error("Register() response success = false")
			}
			if resp.ModuleID != tt.req.ID {
				t.Errorf("Register() ModuleID = %s, want %s", resp.ModuleID, tt.req.ID)
			}
		})
	}
}

func TestRegisterDuplicate(t *testing.T) {
	registry := New(DefaultConfig())

	req := RegistrationRequest{
		ID:        "test-module",
		Name:      "Test Module",
		Endpoints: []Endpoint{{Type: EndpointHTTP, URL: "http://localhost:8080"}},
	}

	// Register once
	_, err := registry.Register(req)
	if err != nil {
		t.Fatalf("First registration failed: %v", err)
	}

	// Try to register again
	_, err = registry.Register(req)
	if err != ErrModuleAlreadyExists {
		t.Errorf("Duplicate registration error = %v, want %v", err, ErrModuleAlreadyExists)
	}
}

func TestDeregister(t *testing.T) {
	registry := New(DefaultConfig())

	// Register a module first
	req := RegistrationRequest{
		ID:        "test-module",
		Name:      "Test Module",
		Endpoints: []Endpoint{{Type: EndpointHTTP, URL: "http://localhost:8080"}},
	}
	_, err := registry.Register(req)
	if err != nil {
		t.Fatalf("Registration failed: %v", err)
	}

	tests := []struct {
		name     string
		moduleID string
		wantErr  error
	}{
		{
			name:     "valid deregistration",
			moduleID: "test-module",
			wantErr:  nil,
		},
		{
			name:     "non-existent module",
			moduleID: "non-existent",
			wantErr:  ErrModuleNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := registry.Deregister(tt.moduleID)

			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Errorf("Deregister() error = %v, want %v", err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Errorf("Deregister() unexpected error = %v", err)
				return
			}

			if resp == nil {
				t.Error("Deregister() returned nil response")
				return
			}

			if !resp.Success {
				t.Error("Deregister() response success = false")
			}
			if resp.ModuleID != tt.moduleID {
				t.Errorf("Deregister() ModuleID = %s, want %s", resp.ModuleID, tt.moduleID)
			}
		})
	}
}

func TestGetModule(t *testing.T) {
	registry := New(DefaultConfig())

	// Register a module first
	req := RegistrationRequest{
		ID:        "test-module",
		Name:      "Test Module",
		Version:   "1.0.0",
		Endpoints: []Endpoint{{Type: EndpointHTTP, URL: "http://localhost:8080"}},
		HealthURL: "http://localhost:8080/health",
		Metadata:  map[string]string{"env": "test"},
	}
	_, err := registry.Register(req)
	if err != nil {
		t.Fatalf("Registration failed: %v", err)
	}

	tests := []struct {
		name     string
		moduleID string
		wantErr  error
	}{
		{
			name:     "existing module",
			moduleID: "test-module",
			wantErr:  nil,
		},
		{
			name:     "non-existent module",
			moduleID: "non-existent",
			wantErr:  ErrModuleNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			module, err := registry.GetModule(tt.moduleID)

			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Errorf("GetModule() error = %v, want %v", err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Errorf("GetModule() unexpected error = %v", err)
				return
			}

			if module == nil {
				t.Error("GetModule() returned nil module")
				return
			}

			if module.ID != req.ID {
				t.Errorf("GetModule() ID = %s, want %s", module.ID, req.ID)
			}
			if module.Name != req.Name {
				t.Errorf("GetModule() Name = %s, want %s", module.Name, req.Name)
			}
			if module.Status != StatusStarting {
				t.Errorf("GetModule() Status = %s, want %s", module.Status, StatusStarting)
			}
		})
	}
}

func TestListModules(t *testing.T) {
	registry := New(DefaultConfig())

	// Register multiple modules
	modules := []RegistrationRequest{
		{
			ID:        "module1",
			Name:      "Module 1",
			Endpoints: []Endpoint{{Type: EndpointHTTP, URL: "http://localhost:8081"}},
		},
		{
			ID:        "module2",
			Name:      "Module 2",
			Endpoints: []Endpoint{{Type: EndpointGRPC, URL: "grpc://localhost:9001"}},
		},
		{
			ID:        "module3",
			Name:      "Different Name",
			Endpoints: []Endpoint{{Type: EndpointHTTP, URL: "http://localhost:8083"}},
		},
	}

	for _, req := range modules {
		_, err := registry.Register(req)
		if err != nil {
			t.Fatalf("Registration failed: %v", err)
		}
	}

	// Update status for filtering tests
	registry.UpdateStatus("module1", StatusHealthy)
	registry.UpdateStatus("module2", StatusUnhealthy)

	tests := []struct {
		name        string
		query       *ModuleQuery
		wantCount   int
		wantModules []string
	}{
		{
			name:        "no filter",
			query:       nil,
			wantCount:   3,
			wantModules: []string{"module1", "module2", "module3"},
		},
		{
			name:        "filter by healthy status",
			query:       &ModuleQuery{Status: &[]ModuleStatus{StatusHealthy}[0]},
			wantCount:   1,
			wantModules: []string{"module1"},
		},
		{
			name:        "filter by name",
			query:       &ModuleQuery{Name: "Module 1"},
			wantCount:   1,
			wantModules: []string{"module1"},
		},
		{
			name:        "filter by endpoint type",
			query:       &ModuleQuery{EndpointType: &[]EndpointType{EndpointHTTP}[0]},
			wantCount:   2,
			wantModules: []string{"module1", "module3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := registry.ListModules(tt.query)

			if len(result) != tt.wantCount {
				t.Errorf("ListModules() count = %d, want %d", len(result), tt.wantCount)
			}

			// Check if expected modules are present
			foundModules := make(map[string]bool)
			for _, module := range result {
				foundModules[module.ID] = true
			}

			for _, expectedID := range tt.wantModules {
				if !foundModules[expectedID] {
					t.Errorf("ListModules() missing expected module %s", expectedID)
				}
			}
		})
	}
}

func TestUpdateStatus(t *testing.T) {
	registry := New(DefaultConfig())

	// Register a module first
	req := RegistrationRequest{
		ID:        "test-module",
		Name:      "Test Module",
		Endpoints: []Endpoint{{Type: EndpointHTTP, URL: "http://localhost:8080"}},
	}
	_, err := registry.Register(req)
	if err != nil {
		t.Fatalf("Registration failed: %v", err)
	}

	tests := []struct {
		name     string
		moduleID string
		status   ModuleStatus
		wantErr  error
	}{
		{
			name:     "update to healthy",
			moduleID: "test-module",
			status:   StatusHealthy,
			wantErr:  nil,
		},
		{
			name:     "update to unhealthy",
			moduleID: "test-module",
			status:   StatusUnhealthy,
			wantErr:  nil,
		},
		{
			name:     "non-existent module",
			moduleID: "non-existent",
			status:   StatusHealthy,
			wantErr:  ErrModuleNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := registry.UpdateStatus(tt.moduleID, tt.status)

			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Errorf("UpdateStatus() error = %v, want %v", err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Errorf("UpdateStatus() unexpected error = %v", err)
				return
			}

			// Verify the status was updated
			module, err := registry.GetModule(tt.moduleID)
			if err != nil {
				t.Fatalf("GetModule() after UpdateStatus() failed: %v", err)
			}
			if module.Status != tt.status {
				t.Errorf("Module status = %s, want %s", module.Status, tt.status)
			}
		})
	}
}

func TestModuleCount(t *testing.T) {
	registry := New(DefaultConfig())

	if count := registry.ModuleCount(); count != 0 {
		t.Errorf("Empty registry ModuleCount() = %d, want 0", count)
	}

	// Register modules
	for i := 0; i < 3; i++ {
		req := RegistrationRequest{
			ID:        fmt.Sprintf("module%d", i),
			Name:      fmt.Sprintf("Module %d", i),
			Endpoints: []Endpoint{{Type: EndpointHTTP, URL: fmt.Sprintf("http://localhost:808%d", i)}},
		}
		_, err := registry.Register(req)
		if err != nil {
			t.Fatalf("Registration failed: %v", err)
		}
	}

	if count := registry.ModuleCount(); count != 3 {
		t.Errorf("Registry with 3 modules ModuleCount() = %d, want 3", count)
	}
}

func TestHealthyCount(t *testing.T) {
	registry := New(DefaultConfig())

	// Register modules with different statuses
	modules := []string{"module1", "module2", "module3"}
	for _, id := range modules {
		req := RegistrationRequest{
			ID:        id,
			Name:      id,
			Endpoints: []Endpoint{{Type: EndpointHTTP, URL: "http://localhost:8080"}},
		}
		_, err := registry.Register(req)
		if err != nil {
			t.Fatalf("Registration failed: %v", err)
		}
	}

	// Update statuses
	registry.UpdateStatus("module1", StatusHealthy)
	registry.UpdateStatus("module2", StatusHealthy)
	registry.UpdateStatus("module3", StatusUnhealthy)

	if count := registry.HealthyCount(); count != 2 {
		t.Errorf("HealthyCount() = %d, want 2", count)
	}
}

func TestRegistryClosed(t *testing.T) {
	registry := New(DefaultConfig())

	req := RegistrationRequest{
		ID:        "test-module",
		Name:      "Test Module",
		Endpoints: []Endpoint{{Type: EndpointHTTP, URL: "http://localhost:8080"}},
	}

	// Stop the registry
	registry.Stop()

	// Try to register after stop
	_, err := registry.Register(req)
	if err != ErrRegistryClosed {
		t.Errorf("Register() after Stop() error = %v, want %v", err, ErrRegistryClosed)
	}

	// Try to deregister after stop
	_, err = registry.Deregister("test")
	if err != ErrRegistryClosed {
		t.Errorf("Deregister() after Stop() error = %v, want %v", err, ErrRegistryClosed)
	}
}

func TestGetAccessors(t *testing.T) {
	registry := New(DefaultConfig())

	if discovery := registry.Discovery(); discovery == nil {
		t.Error("Discovery() returned nil")
	}

	if healthChecker := registry.HealthChecker(); healthChecker == nil {
		t.Error("HealthChecker() returned nil")
	}
}