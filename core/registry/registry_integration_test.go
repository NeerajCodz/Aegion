package registry

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRegistryStartStop(t *testing.T) {
	registry := New(DefaultConfig())

	// Start
	registry.Start()
	time.Sleep(100 * time.Millisecond)
	assert.True(t, registry.healthChecker.running)

	// Stop
	registry.Stop()
	time.Sleep(100 * time.Millisecond)
	assert.True(t, registry.closed)
	assert.False(t, registry.healthChecker.running)
}

func TestRegistryStartStopIdempotent(t *testing.T) {
	registry := New(DefaultConfig())

	registry.Start()
	registry.Start() // Should be safe
	assert.True(t, registry.healthChecker.running)

	registry.Stop()
	registry.Stop() // Should be safe
	assert.True(t, registry.closed)
}

func TestRegistryRegisterAfterClose(t *testing.T) {
	registry := New(DefaultConfig())
	registry.Stop()

	req := RegistrationRequest{
		ID:        "api-service",
		Name:      "API Service",
		Endpoints: []Endpoint{{Type: EndpointHTTP, URL: "http://localhost:8080"}},
	}

	resp, err := registry.Register(req)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, ErrRegistryClosed, err)
}

func TestRegistryDeregisterAfterClose(t *testing.T) {
	registry := New(DefaultConfig())

	// Register first
	req := RegistrationRequest{
		ID:        "api-service",
		Name:      "API Service",
		Endpoints: []Endpoint{{Type: EndpointHTTP, URL: "http://localhost:8080"}},
	}
	registry.Register(req)

	registry.Stop()

	resp, err := registry.Deregister("api-service")

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, ErrRegistryClosed, err)
}

func TestRegistryConcurrentRegisterDeregister(t *testing.T) {
	registry := New(DefaultConfig())
	var wg sync.WaitGroup
	errors := make(chan error, 100)

	// Concurrent registrations
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			req := RegistrationRequest{
				ID:   "module-" + string(rune(id/10)) + "-" + string(rune(id%10)),
				Name: "Module",
				Endpoints: []Endpoint{
					{Type: EndpointHTTP, URL: "http://localhost:8080"},
				},
			}
			_, err := registry.Register(req)
			if err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for duplicate errors
	duplicateCount := 0
	for err := range errors {
		if err == ErrModuleAlreadyExists {
			duplicateCount++
		}
	}

	// No duplicates should exist
	assert.Equal(t, 0, duplicateCount)

	// Should have 50 unique modules
	assert.Equal(t, 50, registry.ModuleCount())
}

func TestRegistryConcurrentGetModule(t *testing.T) {
	registry := New(DefaultConfig())

	// Register modules
	for i := 0; i < 10; i++ {
		req := RegistrationRequest{
			ID:   "module-" + string(rune('0'+i)),
			Name: "Module",
			Endpoints: []Endpoint{
				{Type: EndpointHTTP, URL: "http://localhost:8080"},
			},
		}
		registry.Register(req)
	}

	// Concurrent reads
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			moduleID := "module-" + string(rune('0'+(id%10)))
			module, _ := registry.GetModule(moduleID)
			assert.NotNil(t, module)
		}(i)
	}

	wg.Wait()
}

func TestRegistryConcurrentListModules(t *testing.T) {
	registry := New(DefaultConfig())

	// Register modules with different statuses
	for i := 0; i < 20; i++ {
		req := RegistrationRequest{
			ID:   "module-" + string(rune(i/10)) + "-" + string(rune(i%10)),
			Name: "Module",
			Endpoints: []Endpoint{
				{Type: EndpointHTTP, URL: "http://localhost:8080"},
			},
		}
		_, _ = registry.Register(req)

		if i%3 == 0 {
			registry.UpdateStatus("module-"+string(rune(i/10))+"-"+string(rune(i%10)), StatusHealthy)
		}
	}

	// Concurrent list operations
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			modules := registry.ListModules(nil)
			assert.Greater(t, len(modules), 0)
		}()
	}

	wg.Wait()
}

func TestRegistryConcurrentUpdateStatus(t *testing.T) {
	registry := New(DefaultConfig())

	// Register modules
	for i := 0; i < 10; i++ {
		req := RegistrationRequest{
			ID:   "module-" + string(rune('0'+i)),
			Name: "Module",
			Endpoints: []Endpoint{
				{Type: EndpointHTTP, URL: "http://localhost:8080"},
			},
		}
		registry.Register(req)
	}

	// Concurrent status updates
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			moduleID := "module-" + string(rune('0'+(id%10)))
			status := StatusHealthy
			if id%2 == 0 {
				status = StatusUnhealthy
			}
			registry.UpdateStatus(moduleID, status)
		}(i)
	}

	wg.Wait()

	// All modules should exist
	assert.Equal(t, 10, registry.ModuleCount())
}

func TestRegistryGetHealthyModulesConcurrent(t *testing.T) {
	registry := New(DefaultConfig())

	// Register and update modules
	for i := 0; i < 15; i++ {
		req := RegistrationRequest{
			ID:   "module-" + string(rune(i/5)) + "-" + string(rune(i%5)),
			Name: "Module",
			Endpoints: []Endpoint{
				{Type: EndpointHTTP, URL: "http://localhost:8080"},
			},
		}
		registry.Register(req)

		if i%2 == 0 {
			registry.UpdateStatus("module-"+string(rune(i/5))+"-"+string(rune(i%5)), StatusHealthy)
		}
	}

	// Concurrent reads
	var wg sync.WaitGroup
	results := make(chan []*Module, 50)
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- registry.GetHealthyModules()
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	for modules := range results {
		for _, module := range modules {
			assert.Equal(t, StatusHealthy, module.Status)
		}
	}
}

func TestRegistryModulesCopy(t *testing.T) {
	registry := New(DefaultConfig())

	req := RegistrationRequest{
		ID:        "api-service",
		Name:      "API Service",
		Version:   "1.0.0",
		Endpoints: []Endpoint{{Type: EndpointHTTP, URL: "http://localhost:8080"}},
		Metadata: map[string]string{
			"region": "us-east",
		},
	}
	registry.Register(req)

	module1, _ := registry.GetModule("api-service")

	// Modifying the copy should not affect original
	module1.Name = "Modified"
	module1.Status = StatusUnhealthy

	module3, _ := registry.GetModule("api-service")

	assert.Equal(t, "API Service", module3.Name)
	assert.Equal(t, StatusStarting, module3.Status)
}

func TestRegistryEndpointsCopy(t *testing.T) {
	registry := New(DefaultConfig())

	req := RegistrationRequest{
		ID:   "api-service",
		Name: "API Service",
		Endpoints: []Endpoint{
			{Type: EndpointHTTP, URL: "http://localhost:8080"},
			{Type: EndpointGRPC, URL: "grpc://localhost:9000"},
		},
	}
	registry.Register(req)

	module1, _ := registry.GetModule("api-service")
	module1.Endpoints[0].URL = "http://modified:8080"

	module2, _ := registry.GetModule("api-service")

	assert.Equal(t, "http://localhost:8080", module2.Endpoints[0].URL)
}

func TestRegistryListModulesFilter(t *testing.T) {
	registry := New(DefaultConfig())

	// Register various modules
	configs := []struct {
		id       string
		name     string
		endpoint EndpointType
		status   ModuleStatus
	}{
		{"api-1", "API", EndpointHTTP, StatusHealthy},
		{"api-2", "API", EndpointGRPC, StatusHealthy},
		{"db-1", "Database", EndpointGRPC, StatusUnhealthy},
		{"web-1", "Web", EndpointHTTP, StatusStarting},
	}

	for _, cfg := range configs {
		req := RegistrationRequest{
			ID:   cfg.id,
			Name: cfg.name,
			Endpoints: []Endpoint{
				{Type: cfg.endpoint, URL: "http://localhost:8080"},
			},
		}
		registry.Register(req)
		if cfg.status != StatusStarting {
			registry.UpdateStatus(cfg.id, cfg.status)
		}
	}

	t.Run("filter by status", func(t *testing.T) {
		status := StatusHealthy
		modules := registry.ListModules(&ModuleQuery{Status: &status})
		assert.Len(t, modules, 2)
		for _, m := range modules {
			assert.Equal(t, StatusHealthy, m.Status)
		}
	})

	t.Run("filter by name", func(t *testing.T) {
		modules := registry.ListModules(&ModuleQuery{Name: "API"})
		assert.Len(t, modules, 2)
		for _, m := range modules {
			assert.Equal(t, "API", m.Name)
		}
	})

	t.Run("filter by endpoint type", func(t *testing.T) {
		epType := EndpointGRPC
		modules := registry.ListModules(&ModuleQuery{EndpointType: &epType})
		assert.Len(t, modules, 2)
		for _, m := range modules {
			hasGRPC := false
			for _, ep := range m.Endpoints {
				if ep.Type == EndpointGRPC {
					hasGRPC = true
					break
				}
			}
			assert.True(t, hasGRPC)
		}
	})

	t.Run("multiple filters", func(t *testing.T) {
		status := StatusHealthy
		epType := EndpointHTTP
		modules := registry.ListModules(&ModuleQuery{
			Status:       &status,
			EndpointType: &epType,
		})
		assert.Len(t, modules, 1)
		assert.Equal(t, "api-1", modules[0].ID)
	})
}

func TestRegistryResponseStructures(t *testing.T) {
	registry := New(DefaultConfig())

	req := RegistrationRequest{
		ID:        "api-service",
		Name:      "API Service",
		Version:   "1.0.0",
		Endpoints: []Endpoint{{Type: EndpointHTTP, URL: "http://localhost:8080"}},
		HealthURL: "http://localhost:8080/health",
		Metadata: map[string]string{"env": "prod"},
	}

	resp, err := registry.Register(req)

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Equal(t, "api-service", resp.ModuleID)
	assert.NotZero(t, resp.RegisteredAt)
	assert.NotEmpty(t, resp.Message)
}

func TestRegistryDeregistrationResponse(t *testing.T) {
	registry := New(DefaultConfig())

	req := RegistrationRequest{
		ID:        "api-service",
		Name:      "API Service",
		Endpoints: []Endpoint{{Type: EndpointHTTP, URL: "http://localhost:8080"}},
	}
	registry.Register(req)

	resp, err := registry.Deregister("api-service")

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Equal(t, "api-service", resp.ModuleID)
	assert.NotEmpty(t, resp.Message)
}

func TestRegistryMetadataPreservation(t *testing.T) {
	registry := New(DefaultConfig())

	metadata := map[string]string{
		"version":      "2.3.4",
		"team":         "backend",
		"region":       "us-west-2",
		"environment":  "staging",
	}

	req := RegistrationRequest{
		ID:        "api-service",
		Name:      "API Service",
		Endpoints: []Endpoint{{Type: EndpointHTTP, URL: "http://localhost:8080"}},
		Metadata:  metadata,
	}
	registry.Register(req)

	module, _ := registry.GetModule("api-service")

	for key, value := range metadata {
		assert.Equal(t, value, module.Metadata[key])
	}
}

func TestRegistryVersionTracking(t *testing.T) {
	registry := New(DefaultConfig())

	versions := []string{"1.0.0", "2.0.0", "1.5.3"}

	for i, version := range versions {
		req := RegistrationRequest{
			ID:        "service-v" + string(rune('0'+i)),
			Name:      "Service",
			Version:   version,
			Endpoints: []Endpoint{{Type: EndpointHTTP, URL: "http://localhost:8080"}},
		}
		registry.Register(req)
	}

	for i, expectedVersion := range versions {
		module, _ := registry.GetModule("service-v" + string(rune('0'+i)))
		assert.Equal(t, expectedVersion, module.Version)
	}
}

func TestRegistryTimestampAccuracy(t *testing.T) {
	registry := New(DefaultConfig())

	req := RegistrationRequest{
		ID:        "api-service",
		Name:      "API Service",
		Endpoints: []Endpoint{{Type: EndpointHTTP, URL: "http://localhost:8080"}},
	}

	before := time.Now().UTC()
	resp, _ := registry.Register(req)
	after := time.Now().UTC()

	assert.True(t, resp.RegisteredAt.After(before) || resp.RegisteredAt.Equal(before))
	assert.True(t, resp.RegisteredAt.Before(after) || resp.RegisteredAt.Equal(after))

	module, _ := registry.GetModule("api-service")
	assert.Equal(t, resp.RegisteredAt, module.RegisteredAt)
	assert.Equal(t, resp.RegisteredAt, module.LastHealthAt)
}

func TestRegistryHealthStatusUpdate(t *testing.T) {
	registry := New(DefaultConfig())

	req := RegistrationRequest{
		ID:        "api-service",
		Name:      "API Service",
		Endpoints: []Endpoint{{Type: EndpointHTTP, URL: "http://localhost:8080"}},
	}
	registry.Register(req)

	module, _ := registry.GetModule("api-service")
	assert.Equal(t, StatusStarting, module.Status)

	registry.UpdateStatus("api-service", StatusHealthy)
	module, _ = registry.GetModule("api-service")
	assert.Equal(t, StatusHealthy, module.Status)

	registry.UpdateStatus("api-service", StatusUnhealthy)
	module, _ = registry.GetModule("api-service")
	assert.Equal(t, StatusUnhealthy, module.Status)
}

func TestRegistryLastHealthAtUpdate(t *testing.T) {
	registry := New(DefaultConfig())

	req := RegistrationRequest{
		ID:        "api-service",
		Name:      "API Service",
		Endpoints: []Endpoint{{Type: EndpointHTTP, URL: "http://localhost:8080"}},
	}
	registry.Register(req)

	module1, _ := registry.GetModule("api-service")
	firstTime := module1.LastHealthAt

	time.Sleep(100 * time.Millisecond)
	registry.UpdateStatus("api-service", StatusHealthy)

	module2, _ := registry.GetModule("api-service")
	secondTime := module2.LastHealthAt

	assert.True(t, secondTime.After(firstTime))
}

func TestRegistrySingleEndpointModule(t *testing.T) {
	registry := New(DefaultConfig())

	req := RegistrationRequest{
		ID:        "single-endpoint",
		Name:      "Single Endpoint",
		Endpoints: []Endpoint{{Type: EndpointHTTP, URL: "http://localhost:8080"}},
	}
	resp, err := registry.Register(req)

	assert.NoError(t, err)
	assert.True(t, resp.Success)

	module, _ := registry.GetModule("single-endpoint")
	assert.Len(t, module.Endpoints, 1)
}

func TestRegistryMultipleEndpoints(t *testing.T) {
	registry := New(DefaultConfig())

	endpoints := []Endpoint{
		{Type: EndpointHTTP, URL: "http://localhost:8080"},
		{Type: EndpointGRPC, URL: "grpc://localhost:9000"},
		{Type: EndpointWS, URL: "ws://localhost:8090"},
	}

	req := RegistrationRequest{
		ID:        "multi-endpoint",
		Name:      "Multi Endpoint",
		Endpoints: endpoints,
	}
	resp, err := registry.Register(req)

	assert.NoError(t, err)
	assert.True(t, resp.Success)

	module, _ := registry.GetModule("multi-endpoint")
	assert.Len(t, module.Endpoints, 3)

	for i, ep := range module.Endpoints {
		assert.Equal(t, endpoints[i].Type, ep.Type)
		assert.Equal(t, endpoints[i].URL, ep.URL)
	}
}

func TestRegistryDeregistrationRemovesModule(t *testing.T) {
	registry := New(DefaultConfig())

	req := RegistrationRequest{
		ID:        "api-service",
		Name:      "API Service",
		Endpoints: []Endpoint{{Type: EndpointHTTP, URL: "http://localhost:8080"}},
	}
	registry.Register(req)

	assert.Equal(t, 1, registry.ModuleCount())

	registry.Deregister("api-service")

	assert.Equal(t, 0, registry.ModuleCount())

	_, err := registry.GetModule("api-service")
	assert.Equal(t, ErrModuleNotFound, err)
}

func TestRegistryAccessorsReturnCorrectInstances(t *testing.T) {
	registry := New(DefaultConfig())

	discovery := registry.Discovery()
	healthChecker := registry.HealthChecker()

	assert.NotNil(t, discovery)
	assert.NotNil(t, healthChecker)
	assert.Equal(t, registry.discovery, discovery)
	assert.Equal(t, registry.healthChecker, healthChecker)
}

func TestRegistryEmptyListModules(t *testing.T) {
	registry := New(DefaultConfig())

	modules := registry.ListModules(nil)

	assert.Len(t, modules, 0)
}

func TestRegistryCountWithMultipleModules(t *testing.T) {
	registry := New(DefaultConfig())

	for i := 0; i < 15; i++ {
		req := RegistrationRequest{
			ID:   "module-" + string(rune('0'+(i/10))) + string(rune('0'+(i%10))),
			Name: "Module",
			Endpoints: []Endpoint{
				{Type: EndpointHTTP, URL: "http://localhost:8080"},
			},
		}
		registry.Register(req)

		if i%3 == 0 {
			registry.UpdateStatus("module-"+string(rune('0'+(i/10)))+string(rune('0'+(i%10))), StatusHealthy)
		}
	}

	assert.Equal(t, 15, registry.ModuleCount())
	assert.Equal(t, 5, registry.HealthyCount())
}

func TestRegistryDefaultConfigValues(t *testing.T) {
	config := DefaultConfig()

	assert.Equal(t, 30*time.Second, config.HealthCheckInterval)
	assert.Equal(t, 5*time.Second, config.HealthCheckTimeout)
}

func TestRegistryCustomConfigUsed(t *testing.T) {
	config := Config{
		HealthCheckInterval: 1 * time.Minute,
		HealthCheckTimeout:  10 * time.Second,
	}
	registry := New(config)

	assert.Equal(t, 1*time.Minute, registry.healthChecker.GetInterval())
	assert.Equal(t, 10*time.Second, registry.healthChecker.GetTimeout())
}

func TestRegistryGetAllModulesInternal(t *testing.T) {
	registry := New(DefaultConfig())

	for i := 0; i < 5; i++ {
		req := RegistrationRequest{
			ID:   "module-" + string(rune('0'+i)),
			Name: "Module",
			Endpoints: []Endpoint{
				{Type: EndpointHTTP, URL: "http://localhost:8080"},
			},
		}
		registry.Register(req)
	}

	modules := registry.getAllModules()

	assert.Len(t, modules, 5)
	for _, module := range modules {
		assert.NotNil(t, module)
	}
}
