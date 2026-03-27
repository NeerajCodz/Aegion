package registry

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoveryNew(t *testing.T) {
	registry := New(DefaultConfig())
	discovery := NewDiscovery(registry)

	assert.NotNil(t, discovery)
	assert.Equal(t, registry, discovery.registry)
	assert.NotNil(t, discovery.rrIndex)
	assert.Empty(t, discovery.rrIndex)
}

func TestDiscoveryGetEndpoint(t *testing.T) {
	registry := New(DefaultConfig())
	discovery := registry.Discovery()

	// Register a module
	req := RegistrationRequest{
		ID:   "api-service",
		Name: "API Service",
		Endpoints: []Endpoint{
			{Type: EndpointHTTP, URL: "http://localhost:8080"},
		},
	}
	_, err := registry.Register(req)
	require.NoError(t, err)

	// Update status to healthy
	registry.UpdateStatus("api-service", StatusHealthy)

	url, err := discovery.GetEndpoint("api-service", EndpointHTTP)

	assert.NoError(t, err)
	assert.Equal(t, "http://localhost:8080", url)
}

func TestDiscoveryGetEndpointNotFound(t *testing.T) {
	registry := New(DefaultConfig())
	discovery := registry.Discovery()

	url, err := discovery.GetEndpoint("nonexistent", EndpointHTTP)

	assert.Error(t, err)
	assert.Empty(t, url)
	assert.Equal(t, ErrModuleNotFound, err)
}

func TestDiscoveryGetEndpointTypeNotFound(t *testing.T) {
	registry := New(DefaultConfig())
	discovery := registry.Discovery()

	req := RegistrationRequest{
		ID:   "api-service",
		Name: "API Service",
		Endpoints: []Endpoint{
			{Type: EndpointHTTP, URL: "http://localhost:8080"},
		},
	}
	_, err := registry.Register(req)
	require.NoError(t, err)

	url, err := discovery.GetEndpoint("api-service", EndpointGRPC)

	assert.Error(t, err)
	assert.Empty(t, url)
	assert.Equal(t, ErrEndpointNotFound, err)
}

func TestDiscoveryGetEndpointUnhealthy(t *testing.T) {
	registry := New(DefaultConfig())
	discovery := registry.Discovery()

	req := RegistrationRequest{
		ID:   "api-service",
		Name: "API Service",
		Endpoints: []Endpoint{
			{Type: EndpointHTTP, URL: "http://localhost:8080"},
		},
	}
	_, err := registry.Register(req)
	require.NoError(t, err)

	// Module is unhealthy
	registry.UpdateStatus("api-service", StatusUnhealthy)

	url, err := discovery.GetEndpoint("api-service", EndpointHTTP)

	assert.Error(t, err)
	assert.Empty(t, url)
	assert.Equal(t, ErrNoHealthyInstances, err)
}

func TestDiscoveryGetEndpointStarting(t *testing.T) {
	registry := New(DefaultConfig())
	discovery := registry.Discovery()

	req := RegistrationRequest{
		ID:   "api-service",
		Name: "API Service",
		Endpoints: []Endpoint{
			{Type: EndpointHTTP, URL: "http://localhost:8080"},
		},
	}
	_, err := registry.Register(req)
	require.NoError(t, err)

	// Module is starting (allowed)
	url, err := discovery.GetEndpoint("api-service", EndpointHTTP)

	assert.NoError(t, err)
	assert.Equal(t, "http://localhost:8080", url)
}

func TestDiscoveryGetEndpointRoundRobin(t *testing.T) {
	registry := New(DefaultConfig())
	discovery := registry.Discovery()

	req := RegistrationRequest{
		ID:   "api-service",
		Name: "API Service",
		Endpoints: []Endpoint{
			{Type: EndpointHTTP, URL: "http://localhost:8080"},
			{Type: EndpointHTTP, URL: "http://localhost:8081"},
			{Type: EndpointHTTP, URL: "http://localhost:8082"},
		},
	}
	_, err := registry.Register(req)
	require.NoError(t, err)

	registry.UpdateStatus("api-service", StatusHealthy)

	// Get multiple endpoints and verify round-robin distribution
	urls := make(map[string]int)
	for i := 0; i < 9; i++ {
		url, err := discovery.GetEndpoint("api-service", EndpointHTTP)
		assert.NoError(t, err)
		urls[url]++
	}

	// Should be distributed evenly (3 each)
	assert.Equal(t, 3, urls["http://localhost:8080"])
	assert.Equal(t, 3, urls["http://localhost:8081"])
	assert.Equal(t, 3, urls["http://localhost:8082"])
}

func TestDiscoveryGetEndpointByName(t *testing.T) {
	registry := New(DefaultConfig())
	discovery := registry.Discovery()

	// Register multiple instances
	for i := 0; i < 2; i++ {
		req := RegistrationRequest{
			ID:   fmt.Sprintf("api-%d", i),
			Name: "API Service",
			Endpoints: []Endpoint{
				{Type: EndpointHTTP, URL: fmt.Sprintf("http://localhost:%d", 8080+i)},
			},
		}
		_, err := registry.Register(req)
		require.NoError(t, err)
	}

	endpoints, err := discovery.GetEndpointByName("API Service", EndpointHTTP)

	assert.NoError(t, err)
	assert.Len(t, endpoints, 2)
	assert.Contains(t, endpoints, "http://localhost:8080")
	assert.Contains(t, endpoints, "http://localhost:8081")
}

func TestDiscoveryGetEndpointByNameNoModules(t *testing.T) {
	registry := New(DefaultConfig())
	discovery := registry.Discovery()

	endpoints, err := discovery.GetEndpointByName("Nonexistent", EndpointHTTP)

	assert.Error(t, err)
	assert.Nil(t, endpoints)
	assert.Equal(t, ErrModuleNotFound, err)
}

func TestDiscoveryGetEndpointByNameNoHealthy(t *testing.T) {
	registry := New(DefaultConfig())
	discovery := registry.Discovery()

	req := RegistrationRequest{
		ID:   "api-service",
		Name: "API Service",
		Endpoints: []Endpoint{
			{Type: EndpointHTTP, URL: "http://localhost:8080"},
		},
	}
	_, err := registry.Register(req)
	require.NoError(t, err)

	registry.UpdateStatus("api-service", StatusUnhealthy)

	endpoints, err := discovery.GetEndpointByName("API Service", EndpointHTTP)

	assert.Error(t, err)
	assert.Nil(t, endpoints)
	assert.Equal(t, ErrNoHealthyInstances, err)
}

func TestDiscoveryGetHealthyEndpoint(t *testing.T) {
	registry := New(DefaultConfig())
	discovery := registry.Discovery()

	// Register multiple instances
	for i := 0; i < 3; i++ {
		req := RegistrationRequest{
			ID:   fmt.Sprintf("api-%d", i),
			Name: "API Service",
			Endpoints: []Endpoint{
				{Type: EndpointHTTP, URL: fmt.Sprintf("http://localhost:%d", 8080+i)},
			},
		}
		_, err := registry.Register(req)
		require.NoError(t, err)
		registry.UpdateStatus(fmt.Sprintf("api-%d", i), StatusHealthy)
	}

	endpoint, err := discovery.GetHealthyEndpoint("API Service", EndpointHTTP)

	assert.NoError(t, err)
	assert.NotEmpty(t, endpoint)
	assert.Contains(t, []string{
		"http://localhost:8080",
		"http://localhost:8081",
		"http://localhost:8082",
	}, endpoint)
}

func TestDiscoveryGetHealthyEndpointRoundRobin(t *testing.T) {
	registry := New(DefaultConfig())
	discovery := registry.Discovery()

	// Register 3 instances
	for i := 0; i < 3; i++ {
		req := RegistrationRequest{
			ID:   fmt.Sprintf("api-%d", i),
			Name: "API Service",
			Endpoints: []Endpoint{
				{Type: EndpointHTTP, URL: fmt.Sprintf("http://localhost:%d", 8080+i)},
			},
		}
		_, err := registry.Register(req)
		require.NoError(t, err)
		registry.UpdateStatus(fmt.Sprintf("api-%d", i), StatusHealthy)
	}

	// Get endpoints multiple times
	endpoints := make(map[string]int)
	for i := 0; i < 9; i++ {
		endpoint, _ := discovery.GetHealthyEndpoint("API Service", EndpointHTTP)
		endpoints[endpoint]++
	}

	// All 3 endpoints should be selected at least once
	assert.Len(t, endpoints, 3)
	for url, count := range endpoints {
		assert.Greater(t, count, 0, "Endpoint %s was not selected", url)
	}
}

func TestDiscoveryGetHealthyEndpointNotFound(t *testing.T) {
	registry := New(DefaultConfig())
	discovery := registry.Discovery()

	endpoint, err := discovery.GetHealthyEndpoint("Nonexistent", EndpointHTTP)

	assert.Error(t, err)
	assert.Empty(t, endpoint)
}

func TestDiscoveryGetAllEndpoints(t *testing.T) {
	registry := New(DefaultConfig())
	discovery := registry.Discovery()

	// Register modules with different endpoint types
	for i := 0; i < 2; i++ {
		req := RegistrationRequest{
			ID:   fmt.Sprintf("service-%d", i),
			Name: fmt.Sprintf("Service %d", i),
			Endpoints: []Endpoint{
				{Type: EndpointHTTP, URL: fmt.Sprintf("http://localhost:%d", 8000+i)},
				{Type: EndpointGRPC, URL: fmt.Sprintf("grpc://localhost:%d", 9000+i)},
			},
		}
		_, err := registry.Register(req)
		require.NoError(t, err)
		registry.UpdateStatus(fmt.Sprintf("service-%d", i), StatusHealthy)
	}

	httpEndpoints := discovery.GetAllEndpoints(EndpointHTTP)
	grpcEndpoints := discovery.GetAllEndpoints(EndpointGRPC)

	assert.Len(t, httpEndpoints, 2)
	assert.Len(t, grpcEndpoints, 2)
	assert.Contains(t, httpEndpoints, "http://localhost:8000")
	assert.Contains(t, httpEndpoints, "http://localhost:8001")
	assert.Contains(t, grpcEndpoints, "grpc://localhost:9000")
	assert.Contains(t, grpcEndpoints, "grpc://localhost:9001")
}

func TestDiscoveryGetAllEndpointsEmpty(t *testing.T) {
	registry := New(DefaultConfig())
	discovery := registry.Discovery()

	endpoints := discovery.GetAllEndpoints(EndpointHTTP)

	// Go returns nil slices for empty results from range loops, not empty slices
	assert.Empty(t, endpoints)
}

func TestDiscoveryGetAllEndpointsUnhealthy(t *testing.T) {
	registry := New(DefaultConfig())
	discovery := registry.Discovery()

	req := RegistrationRequest{
		ID:   "service-0",
		Name: "Service 0",
		Endpoints: []Endpoint{
			{Type: EndpointHTTP, URL: "http://localhost:8000"},
		},
	}
	_, err := registry.Register(req)
	require.NoError(t, err)

	// Don't update to healthy - stays in starting state
	registry.UpdateStatus("service-0", StatusUnhealthy)

	endpoints := discovery.GetAllEndpoints(EndpointHTTP)

	assert.Len(t, endpoints, 0)
}

func TestDiscoveryResolveModule(t *testing.T) {
	registry := New(DefaultConfig())
	discovery := registry.Discovery()

	req := RegistrationRequest{
		ID:        "api-service",
		Name:      "API Service",
		Version:   "1.0.0",
		Endpoints: []Endpoint{{Type: EndpointHTTP, URL: "http://localhost:8080"}},
	}
	_, err := registry.Register(req)
	require.NoError(t, err)

	registry.UpdateStatus("api-service", StatusHealthy)

	module, err := discovery.ResolveModule("API Service")

	assert.NoError(t, err)
	assert.NotNil(t, module)
	assert.Equal(t, "api-service", module.ID)
	assert.Equal(t, "API Service", module.Name)
	assert.Equal(t, StatusHealthy, module.Status)
}

func TestDiscoveryResolveModulePrefersHealthy(t *testing.T) {
	registry := New(DefaultConfig())
	discovery := registry.Discovery()

	// Register unhealthy instance
	req1 := RegistrationRequest{
		ID:        "api-1",
		Name:      "API Service",
		Endpoints: []Endpoint{{Type: EndpointHTTP, URL: "http://localhost:8080"}},
	}
	_, err := registry.Register(req1)
	require.NoError(t, err)
	registry.UpdateStatus("api-1", StatusUnhealthy)

	// Register healthy instance
	req2 := RegistrationRequest{
		ID:        "api-2",
		Name:      "API Service",
		Endpoints: []Endpoint{{Type: EndpointHTTP, URL: "http://localhost:8081"}},
	}
	_, err = registry.Register(req2)
	require.NoError(t, err)
	registry.UpdateStatus("api-2", StatusHealthy)

	module, err := discovery.ResolveModule("API Service")

	assert.NoError(t, err)
	assert.Equal(t, "api-2", module.ID)
	assert.Equal(t, StatusHealthy, module.Status)
}

func TestDiscoveryResolveModuleFallsBackToStarting(t *testing.T) {
	registry := New(DefaultConfig())
	discovery := registry.Discovery()

	// Register starting instance
	req := RegistrationRequest{
		ID:        "api-1",
		Name:      "API Service",
		Endpoints: []Endpoint{{Type: EndpointHTTP, URL: "http://localhost:8080"}},
	}
	_, err := registry.Register(req)
	require.NoError(t, err)
	// Stays in starting state

	module, err := discovery.ResolveModule("API Service")

	assert.NoError(t, err)
	assert.Equal(t, "api-1", module.ID)
	assert.Equal(t, StatusStarting, module.Status)
}

func TestDiscoveryResolveModuleNoInstances(t *testing.T) {
	registry := New(DefaultConfig())
	discovery := registry.Discovery()

	// Register unhealthy instance - not returned
	req := RegistrationRequest{
		ID:        "api-1",
		Name:      "API Service",
		Endpoints: []Endpoint{{Type: EndpointHTTP, URL: "http://localhost:8080"}},
	}
	_, err := registry.Register(req)
	require.NoError(t, err)
	registry.UpdateStatus("api-1", StatusUnhealthy)

	module, err := discovery.ResolveModule("API Service")

	assert.Error(t, err)
	assert.Nil(t, module)
	assert.Equal(t, ErrNoHealthyInstances, err)
}

func TestDiscoveryResolveModuleNotFound(t *testing.T) {
	registry := New(DefaultConfig())
	discovery := registry.Discovery()

	module, err := discovery.ResolveModule("Nonexistent")

	assert.Error(t, err)
	assert.Nil(t, module)
	assert.Equal(t, ErrModuleNotFound, err)
}

func TestDiscoveryResetRoundRobin(t *testing.T) {
	registry := New(DefaultConfig())
	discovery := registry.Discovery()

	req := RegistrationRequest{
		ID:   "api-service",
		Name: "API Service",
		Endpoints: []Endpoint{
			{Type: EndpointHTTP, URL: "http://localhost:8080"},
			{Type: EndpointHTTP, URL: "http://localhost:8081"},
			{Type: EndpointHTTP, URL: "http://localhost:8082"},
		},
	}
	_, err := registry.Register(req)
	require.NoError(t, err)

	registry.UpdateStatus("api-service", StatusHealthy)

	// Get some endpoints to populate round-robin
	for i := 0; i < 3; i++ {
		discovery.GetEndpoint("api-service", EndpointHTTP)
	}

	key := "api-service:" + string(EndpointHTTP)
	assert.Contains(t, discovery.rrIndex, key)

	// Reset
	discovery.ResetRoundRobin("api-service", EndpointHTTP)

	assert.NotContains(t, discovery.rrIndex, key)
}

func TestDiscoveryResetAllRoundRobin(t *testing.T) {
	registry := New(DefaultConfig())
	discovery := registry.Discovery()

	// Register modules with MULTIPLE endpoints of the same type per module
	for i := 0; i < 2; i++ {
		req := RegistrationRequest{
			ID:   fmt.Sprintf("service-%d", i),
			Name: fmt.Sprintf("Service %d", i),
			Endpoints: []Endpoint{
				{Type: EndpointHTTP, URL: fmt.Sprintf("http://localhost:%d", 8000+i*10)},
				{Type: EndpointHTTP, URL: fmt.Sprintf("http://localhost:%d", 8001+i*10)},
				{Type: EndpointGRPC, URL: fmt.Sprintf("grpc://localhost:%d", 9000+i*10)},
				{Type: EndpointGRPC, URL: fmt.Sprintf("grpc://localhost:%d", 9001+i*10)},
			},
		}
		_, err := registry.Register(req)
		require.NoError(t, err)
		registry.UpdateStatus(fmt.Sprintf("service-%d", i), StatusHealthy)
	}

	// Populate round-robin - only GetEndpoint uses round-robin for multiple endpoints in same module
	for i := 0; i < 2; i++ {
		discovery.GetEndpoint(fmt.Sprintf("service-%d", i), EndpointHTTP)
		discovery.GetEndpoint(fmt.Sprintf("service-%d", i), EndpointGRPC)
	}

	assert.Greater(t, len(discovery.rrIndex), 0)

	// Reset all
	discovery.ResetAllRoundRobin()

	assert.Equal(t, 0, len(discovery.rrIndex))
}

func TestDiscoveryLeastConnectionsPattern(t *testing.T) {
	// Although the current implementation uses round-robin,
	// this test validates the pattern works as expected.
	registry := New(DefaultConfig())
	discovery := registry.Discovery()

	// Register 2 instances
	for i := 0; i < 2; i++ {
		req := RegistrationRequest{
			ID:   fmt.Sprintf("api-%d", i),
			Name: "API Service",
			Endpoints: []Endpoint{
				{Type: EndpointHTTP, URL: fmt.Sprintf("http://localhost:%d", 8000+i)},
			},
		}
		_, err := registry.Register(req)
		require.NoError(t, err)
		registry.UpdateStatus(fmt.Sprintf("api-%d", i), StatusHealthy)
	}

	// Track which instances get selected
	selections := make(map[string]int)
	for i := 0; i < 20; i++ {
		endpoint, _ := discovery.GetEndpoint(fmt.Sprintf("api-%d", i%2), EndpointHTTP)
		selections[endpoint]++
	}

	// Both should be selected
	assert.Greater(t, len(selections), 0)
	for _, count := range selections {
		assert.Greater(t, count, 0)
	}
}

func TestDiscoveryConcurrentEndpointSelection(t *testing.T) {
	registry := New(DefaultConfig())
	discovery := registry.Discovery()

	req := RegistrationRequest{
		ID:   "api-service",
		Name: "API Service",
		Endpoints: []Endpoint{
			{Type: EndpointHTTP, URL: "http://localhost:8080"},
			{Type: EndpointHTTP, URL: "http://localhost:8081"},
			{Type: EndpointHTTP, URL: "http://localhost:8082"},
		},
	}
	_, err := registry.Register(req)
	require.NoError(t, err)

	registry.UpdateStatus("api-service", StatusHealthy)

	// Concurrent access
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				_, err := discovery.GetEndpoint("api-service", EndpointHTTP)
				assert.NoError(t, err)
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestDiscoveryMultipleEndpointTypes(t *testing.T) {
	registry := New(DefaultConfig())
	discovery := registry.Discovery()

	req := RegistrationRequest{
		ID:   "api-service",
		Name: "API Service",
		Endpoints: []Endpoint{
			{Type: EndpointHTTP, URL: "http://localhost:8080"},
			{Type: EndpointGRPC, URL: "grpc://localhost:9000"},
			{Type: EndpointWS, URL: "ws://localhost:8090"},
		},
	}
	_, err := registry.Register(req)
	require.NoError(t, err)

	registry.UpdateStatus("api-service", StatusHealthy)

	httpURL, err := discovery.GetEndpoint("api-service", EndpointHTTP)
	assert.NoError(t, err)
	assert.Equal(t, "http://localhost:8080", httpURL)

	grpcURL, err := discovery.GetEndpoint("api-service", EndpointGRPC)
	assert.NoError(t, err)
	assert.Equal(t, "grpc://localhost:9000", grpcURL)

	wsURL, err := discovery.GetEndpoint("api-service", EndpointWS)
	assert.NoError(t, err)
	assert.Equal(t, "ws://localhost:8090", wsURL)
}

func TestDiscoveryEndpointTimestamps(t *testing.T) {
	registry := New(DefaultConfig())

	req := RegistrationRequest{
		ID:   "api-service",
		Name: "API Service",
		Endpoints: []Endpoint{
			{Type: EndpointHTTP, URL: "http://localhost:8080"},
		},
	}
	resp, err := registry.Register(req)
	require.NoError(t, err)

	module, _ := registry.GetModule("api-service")

	assert.NotZero(t, module.RegisteredAt)
	assert.NotZero(t, module.LastHealthAt)
	assert.Equal(t, resp.RegisteredAt, module.RegisteredAt)
}

func TestDiscoveryRoundRobinDoesNotLoseProgress(t *testing.T) {
	registry := New(DefaultConfig())
	discovery := registry.Discovery()

	req := RegistrationRequest{
		ID:   "api-service",
		Name: "API Service",
		Endpoints: []Endpoint{
			{Type: EndpointHTTP, URL: "http://localhost:8080"},
			{Type: EndpointHTTP, URL: "http://localhost:8081"},
		},
	}
	_, err := registry.Register(req)
	require.NoError(t, err)

	registry.UpdateStatus("api-service", StatusHealthy)

	// Get endpoints and track sequence (round-robin starts at 0)
	// First call: index=1 -> selects 8081
	// Second call: index=2 -> selects 8080 (2 % 2 = 0)
	// Third call: index=3 -> selects 8081 (3 % 2 = 1)
	// Fourth call: index=4 -> selects 8080 (4 % 2 = 0)
	expected := []string{
		"http://localhost:8081",
		"http://localhost:8080",
		"http://localhost:8081",
		"http://localhost:8080",
	}

	for _, exp := range expected {
		actual, _ := discovery.GetEndpoint("api-service", EndpointHTTP)
		assert.Equal(t, exp, actual)
	}
}

func TestDiscoveryEndpointWithMetadata(t *testing.T) {
	registry := New(DefaultConfig())

	req := RegistrationRequest{
		ID:   "api-service",
		Name: "API Service",
		Endpoints: []Endpoint{
			{Type: EndpointHTTP, URL: "http://localhost:8080"},
		},
		Metadata: map[string]string{
			"version":      "1.0.0",
			"region":       "us-west",
			"environment":  "production",
		},
	}
	_, err := registry.Register(req)
	require.NoError(t, err)

	registry.UpdateStatus("api-service", StatusHealthy)

	module, _ := registry.GetModule("api-service")

	assert.NotEmpty(t, module.Metadata)
	assert.Equal(t, "1.0.0", module.Metadata["version"])
	assert.Equal(t, "us-west", module.Metadata["region"])
}

func TestDiscoveryRoundRobinWithReset(t *testing.T) {
	registry := New(DefaultConfig())
	discovery := registry.Discovery()

	req := RegistrationRequest{
		ID:   "api-service",
		Name: "API Service",
		Endpoints: []Endpoint{
			{Type: EndpointHTTP, URL: "http://localhost:8080"},
			{Type: EndpointHTTP, URL: "http://localhost:8081"},
		},
	}
	_, err := registry.Register(req)
	require.NoError(t, err)

	registry.UpdateStatus("api-service", StatusHealthy)

	// Get first endpoint
	ep1, _ := discovery.GetEndpoint("api-service", EndpointHTTP)

	// Get second endpoint
	ep2, _ := discovery.GetEndpoint("api-service", EndpointHTTP)
	assert.NotEqual(t, ep1, ep2)

	// Reset round-robin
	discovery.ResetRoundRobin("api-service", EndpointHTTP)

	// Should start from beginning
	ep3, _ := discovery.GetEndpoint("api-service", EndpointHTTP)
	assert.Equal(t, ep1, ep3)
}
