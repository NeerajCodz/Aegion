package router

import (
	"encoding/json"
	"net/http"
	"time"
)

// HealthStatus represents the health check response.
type HealthStatus struct {
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
}

// ReadinessStatus represents the readiness check response.
type ReadinessStatus struct {
	Status    string                     `json:"status"`
	Checks    map[string]ComponentStatus `json:"checks"`
	Timestamp string                     `json:"timestamp"`
}

// ComponentStatus represents the health of a component.
type ComponentStatus struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	Latency string `json:"latency,omitempty"`
}

// handleHealth handles the /health endpoint for basic liveness checks.
func (r *Router) handleHealth(w http.ResponseWriter, req *http.Request) {
	status := HealthStatus{
		Status:    "healthy",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(status)
}

// handleReady handles the /ready endpoint for readiness checks.
// This checks if the service is ready to accept traffic.
func (r *Router) handleReady(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	checks := make(map[string]ComponentStatus)
	allHealthy := true

	// Check registered modules if registry is available
	if r.registry != nil {
		moduleCount := r.registry.ModuleCount()
		healthyCount := r.registry.HealthyCount()

		status := "healthy"
		message := ""
		if moduleCount > 0 && healthyCount == 0 {
			status = "unhealthy"
			message = "no healthy modules"
			allHealthy = false
		} else if healthyCount < moduleCount {
			status = "degraded"
			message = "some modules unhealthy"
		}

		checks["modules"] = ComponentStatus{
			Status:  status,
			Message: message,
		}
	}

	// Database check placeholder
	// TODO: Inject database connection and check
	checks["database"] = ComponentStatus{
		Status: "healthy",
	}

	// Cache check placeholder
	// TODO: Inject cache connection and check
	checks["cache"] = ComponentStatus{
		Status: "healthy",
	}

	// Determine overall status
	overallStatus := "healthy"
	httpStatus := http.StatusOK
	if !allHealthy {
		overallStatus = "unhealthy"
		httpStatus = http.StatusServiceUnavailable
	}

	status := ReadinessStatus{
		Status:    overallStatus,
		Checks:    checks,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	// Check if context was cancelled
	if ctx.Err() != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.WriteHeader(httpStatus)
	json.NewEncoder(w).Encode(status)
}

// handleMetrics handles the /metrics endpoint.
// This is a placeholder for Prometheus metrics integration.
func (r *Router) handleMetrics(w http.ResponseWriter, req *http.Request) {
	// Placeholder for Prometheus metrics
	// TODO: Integrate with prometheus/client_golang
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	// Basic metrics placeholder
	w.Write([]byte("# HELP aegion_up Aegion server is up\n"))
	w.Write([]byte("# TYPE aegion_up gauge\n"))
	w.Write([]byte("aegion_up 1\n"))

	if r.registry != nil {
		moduleCount := r.registry.ModuleCount()
		healthyCount := r.registry.HealthyCount()

		w.Write([]byte("\n# HELP aegion_modules_total Total number of registered modules\n"))
		w.Write([]byte("# TYPE aegion_modules_total gauge\n"))
		w.Write([]byte("aegion_modules_total " + itoa(moduleCount) + "\n"))

		w.Write([]byte("\n# HELP aegion_modules_healthy Number of healthy modules\n"))
		w.Write([]byte("# TYPE aegion_modules_healthy gauge\n"))
		w.Write([]byte("aegion_modules_healthy " + itoa(healthyCount) + "\n"))
	}
}

// itoa converts an int to a string without importing strconv.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}

	negative := i < 0
	if negative {
		i = -i
	}

	var buf [20]byte
	pos := len(buf)

	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}

	if negative {
		pos--
		buf[pos] = '-'
	}

	return string(buf[pos:])
}

// HealthChecker provides health check functionality for dependencies.
type HealthChecker interface {
	Check() error
}

// DatabaseHealthChecker checks database connectivity.
type DatabaseHealthChecker struct {
	check func() error
}

// NewDatabaseHealthChecker creates a new database health checker.
func NewDatabaseHealthChecker(checkFn func() error) *DatabaseHealthChecker {
	return &DatabaseHealthChecker{check: checkFn}
}

// Check performs the database health check.
func (c *DatabaseHealthChecker) Check() error {
	if c.check == nil {
		return nil
	}
	return c.check()
}

// CacheHealthChecker checks cache connectivity.
type CacheHealthChecker struct {
	check func() error
}

// NewCacheHealthChecker creates a new cache health checker.
func NewCacheHealthChecker(checkFn func() error) *CacheHealthChecker {
	return &CacheHealthChecker{check: checkFn}
}

// Check performs the cache health check.
func (c *CacheHealthChecker) Check() error {
	if c.check == nil {
		return nil
	}
	return c.check()
}
