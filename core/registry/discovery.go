package registry

import (
	"errors"
	"sync"
	"sync/atomic"
)

var (
	ErrNoHealthyInstances = errors.New("no healthy instances available")
	ErrEndpointNotFound   = errors.New("endpoint not found")
)

// Discovery provides service discovery functionality.
type Discovery struct {
	registry *Registry

	// Round-robin state per module+endpoint type
	rrIndex map[string]*uint64
	rrMu    sync.RWMutex
}

// NewDiscovery creates a new Discovery instance.
func NewDiscovery(registry *Registry) *Discovery {
	return &Discovery{
		registry: registry,
		rrIndex:  make(map[string]*uint64),
	}
}

// GetEndpoint returns an endpoint URL for the specified module and endpoint type.
// If multiple instances exist, it uses round-robin selection.
func (d *Discovery) GetEndpoint(moduleID string, endpointType EndpointType) (string, error) {
	module, err := d.registry.GetModule(moduleID)
	if err != nil {
		return "", err
	}

	if module.Status != StatusHealthy && module.Status != StatusStarting {
		return "", ErrNoHealthyInstances
	}

	// Find matching endpoints
	var matchingEndpoints []Endpoint
	for _, ep := range module.Endpoints {
		if ep.Type == endpointType {
			matchingEndpoints = append(matchingEndpoints, ep)
		}
	}

	if len(matchingEndpoints) == 0 {
		return "", ErrEndpointNotFound
	}

	// Single endpoint - return directly
	if len(matchingEndpoints) == 1 {
		return matchingEndpoints[0].URL, nil
	}

	// Multiple endpoints - use round-robin
	key := moduleID + ":" + string(endpointType)
	idx := d.nextIndex(key)
	selected := matchingEndpoints[idx%uint64(len(matchingEndpoints))]

	return selected.URL, nil
}

// GetEndpointByName returns endpoints for all modules with the given name.
// Useful when multiple instances of the same service are registered.
func (d *Discovery) GetEndpointByName(moduleName string, endpointType EndpointType) ([]string, error) {
	modules := d.registry.ListModules(&ModuleQuery{Name: moduleName})
	if len(modules) == 0 {
		return nil, ErrModuleNotFound
	}

	var endpoints []string
	for _, module := range modules {
		if module.Status != StatusHealthy && module.Status != StatusStarting {
			continue
		}
		for _, ep := range module.Endpoints {
			if ep.Type == endpointType {
				endpoints = append(endpoints, ep.URL)
			}
		}
	}

	if len(endpoints) == 0 {
		return nil, ErrNoHealthyInstances
	}

	return endpoints, nil
}

// GetHealthyEndpoint returns an endpoint from any healthy instance of the named module.
// Uses round-robin across all healthy instances.
func (d *Discovery) GetHealthyEndpoint(moduleName string, endpointType EndpointType) (string, error) {
	endpoints, err := d.GetEndpointByName(moduleName, endpointType)
	if err != nil {
		return "", err
	}

	if len(endpoints) == 1 {
		return endpoints[0], nil
	}

	// Round-robin across healthy endpoints
	key := "name:" + moduleName + ":" + string(endpointType)
	idx := d.nextIndex(key)
	return endpoints[idx%uint64(len(endpoints))], nil
}

// GetAllEndpoints returns all endpoints of a specific type across all healthy modules.
func (d *Discovery) GetAllEndpoints(endpointType EndpointType) []string {
	status := StatusHealthy
	modules := d.registry.ListModules(&ModuleQuery{
		Status:       &status,
		EndpointType: &endpointType,
	})

	var endpoints []string
	for _, module := range modules {
		for _, ep := range module.Endpoints {
			if ep.Type == endpointType {
				endpoints = append(endpoints, ep.URL)
			}
		}
	}

	return endpoints
}

// ResolveModule finds and returns the best available module instance by name.
func (d *Discovery) ResolveModule(moduleName string) (*Module, error) {
	modules := d.registry.ListModules(&ModuleQuery{Name: moduleName})
	if len(modules) == 0 {
		return nil, ErrModuleNotFound
	}

	// Prefer healthy modules
	for _, module := range modules {
		if module.Status == StatusHealthy {
			return module, nil
		}
	}

	// Fall back to starting modules
	for _, module := range modules {
		if module.Status == StatusStarting {
			return module, nil
		}
	}

	return nil, ErrNoHealthyInstances
}

// nextIndex returns the next round-robin index for a key.
func (d *Discovery) nextIndex(key string) uint64 {
	d.rrMu.RLock()
	idx, exists := d.rrIndex[key]
	d.rrMu.RUnlock()

	if !exists {
		d.rrMu.Lock()
		// Double-check after acquiring write lock
		if idx, exists = d.rrIndex[key]; !exists {
			var zero uint64
			d.rrIndex[key] = &zero
			idx = &zero
		}
		d.rrMu.Unlock()
	}

	return atomic.AddUint64(idx, 1)
}

// ResetRoundRobin resets the round-robin index for a specific key.
func (d *Discovery) ResetRoundRobin(moduleID string, endpointType EndpointType) {
	key := moduleID + ":" + string(endpointType)
	d.rrMu.Lock()
	delete(d.rrIndex, key)
	d.rrMu.Unlock()
}

// ResetAllRoundRobin resets all round-robin indexes.
func (d *Discovery) ResetAllRoundRobin() {
	d.rrMu.Lock()
	d.rrIndex = make(map[string]*uint64)
	d.rrMu.Unlock()
}
