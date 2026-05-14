package registry

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/aegion/aegion/internal/xlog"
)

// HealthChecker performs periodic health checks on registered modules.
type HealthChecker struct {
	registry     *Registry
	interval     time.Duration
	timeout      time.Duration
	initialDelay time.Duration
	client       *http.Client
	logger       *xlog.Logger

	stopCh  chan struct{}
	wg      sync.WaitGroup
	mu      sync.Mutex
	running bool
}

// NewHealthChecker creates a new health checker.
func NewHealthChecker(registry *Registry, interval, timeout time.Duration, logger any) *HealthChecker {
	initialDelay := 5 * time.Second
	if interval > 0 && interval < 10*time.Second {
		initialDelay = interval / 2
	}
	log := xlog.Adapt(logger)
	return &HealthChecker{
		registry:     registry,
		interval:     interval,
		timeout:      timeout,
		initialDelay: initialDelay,
		client: &http.Client{
			Timeout: timeout,
		},
		logger: log,
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

	h.logger.Info("health checker started",
		"interval", h.interval,
		"timeout", h.timeout,
	)
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

	h.logger.Info("health checker stopped")
}

// run is the main health checking loop.
func (h *HealthChecker) run() {
	defer h.wg.Done()

	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()

	// Run initial check after a short delay
	time.Sleep(h.initialDelay)
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

	h.logger.Debug("starting health checks", "module_count", len(modules))

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
		if err := h.registry.UpdateStatus(result.ModuleID, result.Status); err != nil {
			h.logger.Warn("failed to update module status", "error", err, "module_id", result.ModuleID)
		}
		if result.Status == StatusHealthy {
			healthyCount++
		} else {
			unhealthyCount++
		}
	}

	h.logger.Debug("health checks completed",
		"healthy", healthyCount,
		"unhealthy", unhealthyCount,
	)
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
		h.logHealthCheckFailure(module, result)
		return result
	}

	resp, err := h.client.Do(req)
	result.Latency = time.Since(start)

	if err != nil {
		result.Status = StatusUnhealthy
		result.Error = err.Error()
		h.logHealthCheckFailure(module, result)
		return result
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		result.Status = StatusHealthy
		// Only log if status changed from unhealthy
		if module.Status != StatusHealthy && module.Status != StatusStarting {
			h.logger.Info("module recovered",
				"module_id", module.ID,
				"name", module.Name,
				"latency", result.Latency,
			)
		}
	} else {
		result.Status = StatusUnhealthy
		result.Error = "health check returned non-2xx status"
		h.logHealthCheckFailure(module, result)
	}

	return result
}

// logHealthCheckFailure logs a health check failure.
func (h *HealthChecker) logHealthCheckFailure(module *Module, result HealthCheckResult) {
	// Only log if module was previously healthy
	if module.Status == StatusHealthy {
		h.logger.Warn("module became unhealthy",
			"module_id", module.ID,
			"name", module.Name,
			"error", result.Error,
			"latency", result.Latency,
		)
	}
}

// CheckNow performs an immediate health check on a specific module.
func (h *HealthChecker) CheckNow(moduleID string) (*HealthCheckResult, error) {
	module, err := h.registry.GetModule(moduleID)
	if err != nil {
		return nil, err
	}

	result := h.checkModule(module)
	if err := h.registry.UpdateStatus(moduleID, result.Status); err != nil {
		return nil, err
	}

	return &result, nil
}

// SetInterval updates the health check interval.
func (h *HealthChecker) SetInterval(interval time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.interval = interval
	h.logger.Info("health check interval updated", "interval", interval)
}

// SetTimeout updates the health check timeout.
func (h *HealthChecker) SetTimeout(timeout time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.timeout = timeout
	h.client.Timeout = timeout
	h.logger.Info("health check timeout updated", "timeout", timeout)
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
