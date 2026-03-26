// Package registry provides service registration and discovery for Aegion modules.
package registry

import (
	"time"
)

// ModuleStatus represents the health status of a registered module.
type ModuleStatus string

const (
	StatusHealthy   ModuleStatus = "healthy"
	StatusUnhealthy ModuleStatus = "unhealthy"
	StatusUnknown   ModuleStatus = "unknown"
	StatusStarting  ModuleStatus = "starting"
)

// EndpointType defines the type of endpoint a module exposes.
type EndpointType string

const (
	EndpointHTTP EndpointType = "http"
	EndpointGRPC EndpointType = "grpc"
	EndpointWS   EndpointType = "websocket"
)

// Endpoint represents a service endpoint exposed by a module.
type Endpoint struct {
	Type    EndpointType `json:"type"`
	URL     string       `json:"url"`
	Methods []string     `json:"methods,omitempty"`
}

// Module represents a registered service module.
type Module struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	Version      string       `json:"version"`
	Endpoints    []Endpoint   `json:"endpoints"`
	HealthURL    string       `json:"health_url"`
	Status       ModuleStatus `json:"status"`
	RegisteredAt time.Time    `json:"registered_at"`
	LastHealthAt time.Time    `json:"last_health_at"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// RegistrationRequest is the request to register a module.
type RegistrationRequest struct {
	ID        string            `json:"id" validate:"required"`
	Name      string            `json:"name" validate:"required"`
	Version   string            `json:"version" validate:"required"`
	Endpoints []Endpoint        `json:"endpoints" validate:"required,min=1"`
	HealthURL string            `json:"health_url" validate:"required,url"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// RegistrationResponse is the response after registering a module.
type RegistrationResponse struct {
	Success      bool      `json:"success"`
	ModuleID     string    `json:"module_id"`
	RegisteredAt time.Time `json:"registered_at"`
	Message      string    `json:"message,omitempty"`
}

// DeregistrationRequest is the request to deregister a module.
type DeregistrationRequest struct {
	ModuleID string `json:"module_id" validate:"required"`
}

// DeregistrationResponse is the response after deregistering a module.
type DeregistrationResponse struct {
	Success  bool   `json:"success"`
	ModuleID string `json:"module_id"`
	Message  string `json:"message,omitempty"`
}

// HealthCheckResult represents the result of a health check.
type HealthCheckResult struct {
	ModuleID  string       `json:"module_id"`
	Status    ModuleStatus `json:"status"`
	CheckedAt time.Time    `json:"checked_at"`
	Latency   time.Duration `json:"latency"`
	Error     string       `json:"error,omitempty"`
}

// ModuleQuery is used to filter modules when listing.
type ModuleQuery struct {
	Status       *ModuleStatus `json:"status,omitempty"`
	Name         string        `json:"name,omitempty"`
	EndpointType *EndpointType `json:"endpoint_type,omitempty"`
}
