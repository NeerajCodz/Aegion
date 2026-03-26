package registry

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// HealthChecker performs periodic health checks on registered modules.
type HealthChecker struct {
	registry *Registry
	interval time.Duration
	timeout  time.Duration
	client   *http.Client

	stopCh chan struct{}
	wg     sync.WaitGroup
	mu     sync.Mutex
	running bool
}

// NewHealthChecker creates a new health checker.
func NewHealthChecker(registry *Registry, interval, timeout time.Duration) *HealthChecker {
	return &HealthChecker{
		registry: registry,
		interval: interval,
		timeout:  timeout,
		client: &http.Client{
			Timeout: timeout,
		},
		stopCh: make(chan struct{}),
	}
}

// Start begins the periodic health checking goroutine.
func (h *HealthChecker) Start() {
	h.mu.Lock()
	if h.running {
		h.mu.Unlock()
		return
	}
	h.running = true
	h.mu.Unlock()

	h.wg.Add(1)
	go h.run()

	log.Info().
		Dur("interval", h.interval).
		Dur("timeout", h.timeout).
		Msg("health checker started")
}

// Stop stops the health checking goroutine.
func (h *HealthChecker) Stop() {
	h.mu.Lock()
	if !h.running {
		h.mu.Unlock()
		return
	}
	h.running = false
	h.mu.Unlock()

	close(h.stopCh)
	h.wg.Wait()

	log.Info().Msg("health checker stopped")
}

// run is the main health checking loop.
func (h *HealthChecker) run() {
	defer h.wg.Done()

	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()

	// Run initial check after a short delay
	time.Sleep(5 * time.Second)
	h.checkAll()

	for {
		select {
		case <-ticker.C:
			h.checkAll()
		case <-h.stopCh:
			return
		}
	}
}

// checkAll performs health checks on all registered modules.
func (h *HealthChecker) checkAll() {
	modules := h.registry.getAllModules()
	if len(modules) == 0 {
		return
	}

	log.Debug().Int("module_count", len(modules)).Msg("starting health checks")

	var wg sync.WaitGroup
	results := make(chan HealthCheckResult, len(modules))

	for _, module := range modules {
		wg.Add(1)
		go func(m *Module) {
			defer wg.Done()
			result := h.checkModule(m)
			results <- result
		}(module)
	}

	// Wait for all checks to complete
	go func() {
		wg.Wait()
		close(results)
	}()

	// Process results
	healthyCount := 0
	unhealthyCount := 0
	for result := range results {
		h.registry.UpdateStatus(result.ModuleID, result.Status)
		if result.Status == StatusHealthy {
			healthyCount++
		} else {
			unhealthyCount++
		}
	}

	log.Debug().
		Int("healthy", healthyCount).
		Int("unhealthy", unhealthyCount).
		Msg("health checks completed")
}

// checkModule performs a health check on a single module.
func (h *HealthChecker) checkModule(module *Module) HealthCheckResult {
	result := HealthCheckResult{
		ModuleID:  module.ID,
		CheckedAt: time.Now().UTC(),
	}

	if module.HealthURL == "" {
		result.Status = StatusUnknown
		result.Error = "no health URL configured"
		return result
	}

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), h.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, module.HealthURL, nil)
	if err != nil {
		result.Status = StatusUnhealthy
		result.Error = err.Error()
		result.Latency = time.Since(start)
		logHealthCheckFailure(module, result)
		return result
	}

	resp, err := h.client.Do(req)
	result.Latency = time.Since(start)

	if err != nil {
		result.Status = StatusUnhealthy
		result.Error = err.Error()
		logHealthCheckFailure(module, result)
		return result
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		result.Status = StatusHealthy
		// Only log if status changed from unhealthy
		if module.Status != StatusHealthy && module.Status != StatusStarting {
			log.Info().
				Str("module_id", module.ID).
				Str("name", module.Name).
				Dur("latency", result.Latency).
				Msg("module recovered")
		}
	} else {
		result.Status = StatusUnhealthy
		result.Error = "health check returned non-2xx status"
		logHealthCheckFailure(module, result)
	}

	return result
}

// logHealthCheckFailure logs a health check failure.
func logHealthCheckFailure(module *Module, result HealthCheckResult) {
	// Only log if module was previously healthy
	if module.Status == StatusHealthy {
		log.Warn().
			Str("module_id", module.ID).
			Str("name", module.Name).
			Str("error", result.Error).
			Dur("latency", result.Latency).
			Msg("module became unhealthy")
	}
}

// CheckNow performs an immediate health check on a specific module.
func (h *HealthChecker) CheckNow(moduleID string) (*HealthCheckResult, error) {
	module, err := h.registry.GetModule(moduleID)
	if err != nil {
		return nil, err
	}

	result := h.checkModule(module)
	h.registry.UpdateStatus(moduleID, result.Status)

	return &result, nil
}

// SetInterval updates the health check interval.
func (h *HealthChecker) SetInterval(interval time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.interval = interval
	log.Info().Dur("interval", interval).Msg("health check interval updated")
}

// SetTimeout updates the health check timeout.
func (h *HealthChecker) SetTimeout(timeout time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.timeout = timeout
	h.client.Timeout = timeout
	log.Info().Dur("timeout", timeout).Msg("health check timeout updated")
}

// GetInterval returns the current health check interval.
func (h *HealthChecker) GetInterval() time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.interval
}

// GetTimeout returns the current health check timeout.
func (h *HealthChecker) GetTimeout() time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.timeout
}
