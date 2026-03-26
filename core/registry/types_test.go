package registry

import (
	"testing"
	"time"
)

func TestModuleStatus(t *testing.T) {
	tests := []struct {
		name   string
		status ModuleStatus
		want   string
	}{
		{"healthy status", StatusHealthy, "healthy"},
		{"unhealthy status", StatusUnhealthy, "unhealthy"},
		{"unknown status", StatusUnknown, "unknown"},
		{"starting status", StatusStarting, "starting"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.status) != tt.want {
				t.Errorf("ModuleStatus = %s, want %s", string(tt.status), tt.want)
			}
		})
	}
}

func TestEndpointType(t *testing.T) {
	tests := []struct {
		name     string
		endpoint EndpointType
		want     string
	}{
		{"http endpoint", EndpointHTTP, "http"},
		{"grpc endpoint", EndpointGRPC, "grpc"},
		{"websocket endpoint", EndpointWS, "websocket"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.endpoint) != tt.want {
				t.Errorf("EndpointType = %s, want %s", string(tt.endpoint), tt.want)
			}
		})
	}
}

func TestEndpointStruct(t *testing.T) {
	endpoint := Endpoint{
		Type:    EndpointHTTP,
		URL:     "http://localhost:8080",
		Methods: []string{"GET", "POST"},
	}

	if endpoint.Type != EndpointHTTP {
		t.Errorf("Type = %s, want %s", endpoint.Type, EndpointHTTP)
	}
	if endpoint.URL != "http://localhost:8080" {
		t.Errorf("URL = %s, want http://localhost:8080", endpoint.URL)
	}
	if len(endpoint.Methods) != 2 {
		t.Errorf("Methods length = %d, want 2", len(endpoint.Methods))
	}
	if endpoint.Methods[0] != "GET" || endpoint.Methods[1] != "POST" {
		t.Errorf("Methods = %v, want [GET POST]", endpoint.Methods)
	}
}

func TestModuleStruct(t *testing.T) {
	now := time.Now()
	module := Module{
		ID:        "test-module",
		Name:      "Test Module",
		Version:   "1.0.0",
		Endpoints: []Endpoint{{Type: EndpointHTTP, URL: "http://localhost:8080"}},
		HealthURL: "http://localhost:8080/health",
		Status:    StatusHealthy,
		RegisteredAt: now,
		LastHealthAt: now,
		Metadata: map[string]string{"env": "test"},
	}

	if module.ID != "test-module" {
		t.Errorf("ID = %s, want test-module", module.ID)
	}
	if module.Name != "Test Module" {
		t.Errorf("Name = %s, want Test Module", module.Name)
	}
	if module.Version != "1.0.0" {
		t.Errorf("Version = %s, want 1.0.0", module.Version)
	}
	if len(module.Endpoints) != 1 {
		t.Errorf("Endpoints length = %d, want 1", len(module.Endpoints))
	}
	if module.Status != StatusHealthy {
		t.Errorf("Status = %s, want %s", module.Status, StatusHealthy)
	}
	if module.Metadata["env"] != "test" {
		t.Errorf("Metadata[env] = %s, want test", module.Metadata["env"])
	}
}

func TestRegistrationRequestStruct(t *testing.T) {
	req := RegistrationRequest{
		ID:        "test-id",
		Name:      "test-name",
		Version:   "1.0.0",
		Endpoints: []Endpoint{{Type: EndpointHTTP, URL: "http://localhost:8080"}},
		HealthURL: "http://localhost:8080/health",
		Metadata:  map[string]string{"key": "value"},
	}

	if req.ID != "test-id" {
		t.Errorf("ID = %s, want test-id", req.ID)
	}
	if req.Name != "test-name" {
		t.Errorf("Name = %s, want test-name", req.Name)
	}
	if req.Version != "1.0.0" {
		t.Errorf("Version = %s, want 1.0.0", req.Version)
	}
	if len(req.Endpoints) != 1 {
		t.Errorf("Endpoints length = %d, want 1", len(req.Endpoints))
	}
	if req.HealthURL != "http://localhost:8080/health" {
		t.Errorf("HealthURL = %s, want http://localhost:8080/health", req.HealthURL)
	}
	if req.Metadata["key"] != "value" {
		t.Errorf("Metadata[key] = %s, want value", req.Metadata["key"])
	}
}

func TestRegistrationResponseStruct(t *testing.T) {
	now := time.Now()
	resp := RegistrationResponse{
		Success:      true,
		ModuleID:     "test-id",
		RegisteredAt: now,
		Message:      "success",
	}

	if !resp.Success {
		t.Errorf("Success = %t, want true", resp.Success)
	}
	if resp.ModuleID != "test-id" {
		t.Errorf("ModuleID = %s, want test-id", resp.ModuleID)
	}
	if resp.Message != "success" {
		t.Errorf("Message = %s, want success", resp.Message)
	}
	if resp.RegisteredAt != now {
		t.Errorf("RegisteredAt mismatch")
	}
}

func TestDeregistrationResponseStruct(t *testing.T) {
	resp := DeregistrationResponse{
		Success:  true,
		ModuleID: "test-id", 
		Message:  "deregistered",
	}

	if !resp.Success {
		t.Errorf("Success = %t, want true", resp.Success)
	}
	if resp.ModuleID != "test-id" {
		t.Errorf("ModuleID = %s, want test-id", resp.ModuleID)
	}
	if resp.Message != "deregistered" {
		t.Errorf("Message = %s, want deregistered", resp.Message)
	}
}

func TestModuleQueryStruct(t *testing.T) {
	status := StatusHealthy
	endpointType := EndpointHTTP
	query := ModuleQuery{
		Status:       &status,
		Name:         "test-module",
		EndpointType: &endpointType,
	}

	if *query.Status != StatusHealthy {
		t.Errorf("Status = %s, want %s", *query.Status, StatusHealthy)
	}
	if query.Name != "test-module" {
		t.Errorf("Name = %s, want test-module", query.Name)
	}
	if *query.EndpointType != EndpointHTTP {
		t.Errorf("EndpointType = %s, want %s", *query.EndpointType, EndpointHTTP)
	}
}

func TestHealthCheckResultStruct(t *testing.T) {
	now := time.Now()
	latency := 100 * time.Millisecond
	result := HealthCheckResult{
		ModuleID:  "test-module",
		Status:    StatusHealthy,
		CheckedAt: now,
		Latency:   latency,
		Error:     "",
	}

	if result.ModuleID != "test-module" {
		t.Errorf("ModuleID = %s, want test-module", result.ModuleID)
	}
	if result.Status != StatusHealthy {
		t.Errorf("Status = %s, want %s", result.Status, StatusHealthy)
	}
	if result.Latency != latency {
		t.Errorf("Latency = %v, want %v", result.Latency, latency)
	}
	if result.Error != "" {
		t.Errorf("Error = %s, want empty", result.Error)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	expectedInterval := 30 * time.Second
	expectedTimeout := 5 * time.Second

	if cfg.HealthCheckInterval != expectedInterval {
		t.Errorf("HealthCheckInterval = %v, want %v", cfg.HealthCheckInterval, expectedInterval)
	}
	if cfg.HealthCheckTimeout != expectedTimeout {
		t.Errorf("HealthCheckTimeout = %v, want %v", cfg.HealthCheckTimeout, expectedTimeout)
	}
}

func TestRegistryErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		msg  string
	}{
		{"ErrModuleNotFound", ErrModuleNotFound, "module not found"},
		{"ErrModuleAlreadyExists", ErrModuleAlreadyExists, "module already registered"},
		{"ErrInvalidModule", ErrInvalidModule, "invalid module configuration"},
		{"ErrRegistryClosed", ErrRegistryClosed, "registry is closed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Error() != tt.msg {
				t.Errorf("error message = %q, want %q", tt.err.Error(), tt.msg)
			}
		})
	}
}