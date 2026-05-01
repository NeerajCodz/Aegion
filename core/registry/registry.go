package registry

import (
	"errors"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

var (
	ErrModuleNotFound      = errors.New("module not found")
	ErrModuleAlreadyExists = errors.New("module already registered")
	ErrInvalidModule       = errors.New("invalid module configuration")
	ErrRegistryClosed      = errors.New("registry is closed")
)

// Registry manages service module registration and discovery.
type Registry struct {
	modules map[string]*Module
	mu      sync.RWMutex
	closed  bool

	// Health checker
	healthChecker *HealthChecker

	// Discovery
	discovery *Discovery
}

// Config holds registry configuration.
type Config struct {
	HealthCheckInterval time.Duration
	HealthCheckTimeout  time.Duration
}

// DefaultConfig returns the default registry configuration.
func DefaultConfig() Config {
	return Config{
		HealthCheckInterval: 30 * time.Second,
		HealthCheckTimeout:  5 * time.Second,
	}
}

// New creates a new service registry.
func New(cfg Config) *Registry {
	if cfg.HealthCheckInterval == 0 {
		cfg.HealthCheckInterval = 30 * time.Second
	}
	if cfg.HealthCheckTimeout == 0 {
		cfg.HealthCheckTimeout = 5 * time.Second
	}

	r := &Registry{
		modules: make(map[string]*Module),
	}

	r.healthChecker = NewHealthChecker(r, cfg.HealthCheckInterval, cfg.HealthCheckTimeout)
	r.discovery = NewDiscovery(r)

	return r
}

// Start starts the registry background tasks.
func (r *Registry) Start() {
	r.healthChecker.Start()
	log.Info().Msg("service registry started")
}

// Stop stops the registry and its background tasks.
func (r *Registry) Stop() {
	r.mu.Lock()
	r.closed = true
	r.mu.Unlock()

	r.healthChecker.Stop()
	log.Info().Msg("service registry stopped")
}

// Register registers a new module with the registry.
func (r *Registry) Register(req RegistrationRequest) (*RegistrationResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil, ErrRegistryClosed
	}

	if req.ID == "" || req.Name == "" || len(req.Endpoints) == 0 {
		return nil, ErrInvalidModule
	}

	if _, exists := r.modules[req.ID]; exists {
		return nil, ErrModuleAlreadyExists
	}

	now := time.Now().UTC()
	module := &Module{
		ID:           req.ID,
		Name:         req.Name,
		Version:      req.Version,
		Endpoints:    req.Endpoints,
		HealthURL:    req.HealthURL,
		Status:       StatusStarting,
		RegisteredAt: now,
		LastHealthAt: now,
		Metadata:     req.Metadata,
	}

	r.modules[req.ID] = module

	log.Info().
		Str("module_id", module.ID).
		Str("name", module.Name).
		Str("version", module.Version).
		Int("endpoints", len(module.Endpoints)).
		Msg("module registered")

	return &RegistrationResponse{
		Success:      true,
		ModuleID:     module.ID,
		RegisteredAt: now,
		Message:      "module registered successfully",
	}, nil
}

// Deregister removes a module from the registry.
func (r *Registry) Deregister(moduleID string) (*DeregistrationResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil, ErrRegistryClosed
	}

	module, exists := r.modules[moduleID]
	if !exists {
		return nil, ErrModuleNotFound
	}

	delete(r.modules, moduleID)

	log.Info().
		Str("module_id", module.ID).
		Str("name", module.Name).
		Msg("module deregistered")

	return &DeregistrationResponse{
		Success:  true,
		ModuleID: moduleID,
		Message:  "module deregistered successfully",
	}, nil
}

// GetModule returns a registered module by ID.
func (r *Registry) GetModule(moduleID string) (*Module, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	module, exists := r.modules[moduleID]
	if !exists {
		return nil, ErrModuleNotFound
	}

	// Return a copy to prevent concurrent modification
	moduleCopy := *module
	moduleCopy.Endpoints = make([]Endpoint, len(module.Endpoints))
	copy(moduleCopy.Endpoints, module.Endpoints)

	return &moduleCopy, nil
}

// ListModules returns all registered modules, optionally filtered.
func (r *Registry) ListModules(query *ModuleQuery) []*Module {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*Module, 0, len(r.modules))

	for _, module := range r.modules {
		if query != nil {
			if query.Status != nil && module.Status != *query.Status {
				continue
			}
			if query.Name != "" && module.Name != query.Name {
				continue
			}
			if query.EndpointType != nil {
				hasType := false
				for _, ep := range module.Endpoints {
					if ep.Type == *query.EndpointType {
						hasType = true
						break
					}
				}
				if !hasType {
					continue
				}
			}
		}

		// Return a copy
		moduleCopy := *module
		moduleCopy.Endpoints = make([]Endpoint, len(module.Endpoints))
		copy(moduleCopy.Endpoints, module.Endpoints)
		result = append(result, &moduleCopy)
	}

	return result
}

// UpdateStatus updates the status of a registered module.
func (r *Registry) UpdateStatus(moduleID string, status ModuleStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	module, exists := r.modules[moduleID]
	if !exists {
		return ErrModuleNotFound
	}

	oldStatus := module.Status
	module.Status = status
	module.LastHealthAt = time.Now().UTC()

	if oldStatus != status {
		log.Info().
			Str("module_id", moduleID).
			Str("old_status", string(oldStatus)).
			Str("new_status", string(status)).
			Msg("module status changed")
	}

	return nil
}

// GetHealthyModules returns all modules with healthy status.
func (r *Registry) GetHealthyModules() []*Module {
	status := StatusHealthy
	return r.ListModules(&ModuleQuery{Status: &status})
}

// ModuleCount returns the total number of registered modules.
func (r *Registry) ModuleCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.modules)
}

// HealthyCount returns the number of healthy modules.
func (r *Registry) HealthyCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	count := 0
	for _, module := range r.modules {
		if module.Status == StatusHealthy {
			count++
		}
	}
	return count
}

// Discovery returns the service discovery instance.
func (r *Registry) Discovery() *Discovery {
	return r.discovery
}

// HealthChecker returns the health checker instance.
func (r *Registry) HealthChecker() *HealthChecker {
	return r.healthChecker
}

// getAllModules returns all modules (internal use for health checking).
func (r *Registry) getAllModules() []*Module {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*Module, 0, len(r.modules))
	for _, module := range r.modules {
		moduleCopy := *module
		result = append(result, &moduleCopy)
	}
	return result
}
