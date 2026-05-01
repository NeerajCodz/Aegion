package router

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
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
	if err := json.NewEncoder(w).Encode(status); err != nil {
		r.logger.Error("Failed to encode health response", "error", err)
	}
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

	checks["database"] = r.runDependencyCheck("database", r.databaseChecker)
	if checks["database"].Status == "unhealthy" {
		allHealthy = false
	}

	checks["cache"] = r.runDependencyCheck("cache", r.cacheChecker)
	if checks["cache"].Status == "unhealthy" {
		allHealthy = false
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
	if err := json.NewEncoder(w).Encode(status); err != nil {
		r.logger.Error("Failed to encode readiness response", "error", err)
	}
}

// handleMetrics handles the /metrics endpoint.
func (r *Router) handleMetrics(w http.ResponseWriter, req *http.Request) {
	now := time.Now().UTC()
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	var builder strings.Builder
	writeMetricHelp(&builder, "aegion_up", "Aegion server is up", "gauge")
	writeMetricSample(&builder, "aegion_up", nil, "1")
	writeMetricHelp(&builder, "aegion_router_uptime_seconds", "Router uptime in seconds", "gauge")
	writeMetricSample(&builder, "aegion_router_uptime_seconds", nil, formatFloat(now.Sub(r.startedAt).Seconds()))
	writeMetricHelp(&builder, "aegion_router_request_timeout_seconds", "Configured router request timeout in seconds", "gauge")
	writeMetricSample(&builder, "aegion_router_request_timeout_seconds", nil, formatFloat(r.config.RequestTimeout.Seconds()))
	writeMetricHelp(&builder, "aegion_router_module_timeout_seconds", "Configured module proxy timeout in seconds", "gauge")
	writeMetricSample(&builder, "aegion_router_module_timeout_seconds", nil, formatFloat(r.config.ModuleTimeout.Seconds()))
	writeMetricHelp(&builder, "aegion_router_rate_limit_enabled", "Router rate limiting enabled flag", "gauge")
	writeMetricSample(&builder, "aegion_router_rate_limit_enabled", nil, boolGauge(r.config.RateLimit.Enabled))
	writeMetricHelp(&builder, "aegion_router_rate_limit_rps", "Configured router requests per second limit", "gauge")
	writeMetricSample(&builder, "aegion_router_rate_limit_rps", nil, formatFloat(r.config.RateLimit.RequestsPerSecond))
	writeMetricHelp(&builder, "aegion_router_rate_limit_burst", "Configured router burst limit", "gauge")
	writeMetricSample(&builder, "aegion_router_rate_limit_burst", nil, itoa(r.config.RateLimit.Burst))

	if r.registry != nil {
		moduleCount := r.registry.ModuleCount()
		healthyCount := r.registry.HealthyCount()

		writeMetricHelp(&builder, "aegion_modules_total", "Total number of registered modules", "gauge")
		writeMetricSample(&builder, "aegion_modules_total", nil, itoa(moduleCount))
		writeMetricHelp(&builder, "aegion_modules_healthy", "Number of healthy modules", "gauge")
		writeMetricSample(&builder, "aegion_modules_healthy", nil, itoa(healthyCount))
	}

	r.writeDependencyMetric(&builder, "database", r.runDependencyCheck("database", r.databaseChecker))
	r.writeDependencyMetric(&builder, "cache", r.runDependencyCheck("cache", r.cacheChecker))

	_, _ = w.Write([]byte(builder.String()))
}

func (r *Router) writeDependencyMetric(builder *strings.Builder, name string, status ComponentStatus) {
	writeMetricHelp(builder, "aegion_dependency_status", "Dependency status by component and state", "gauge")
	writeMetricSample(builder, "aegion_dependency_status", map[string]string{
		"component": name,
		"status":    status.Status,
	}, "1")

	if latencyMs := durationMillis(status.Latency); latencyMs >= 0 {
		writeMetricHelp(builder, "aegion_dependency_latency_milliseconds", "Dependency check latency in milliseconds", "gauge")
		writeMetricSample(builder, "aegion_dependency_latency_milliseconds", map[string]string{
			"component": name,
		}, formatFloat(latencyMs))
	}
}

func writeMetricHelp(builder *strings.Builder, name, help, metricType string) {
	builder.WriteString("# HELP ")
	builder.WriteString(name)
	builder.WriteByte(' ')
	builder.WriteString(help)
	builder.WriteByte('\n')
	builder.WriteString("# TYPE ")
	builder.WriteString(name)
	builder.WriteByte(' ')
	builder.WriteString(metricType)
	builder.WriteByte('\n')
}

func writeMetricSample(builder *strings.Builder, name string, labels map[string]string, value string) {
	builder.WriteString(name)
	if len(labels) > 0 {
		keys := make([]string, 0, len(labels))
		for key := range labels {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		builder.WriteByte('{')
		for index, key := range keys {
			if index > 0 {
				builder.WriteByte(',')
			}
			builder.WriteString(key)
			builder.WriteString("=\"")
			builder.WriteString(escapePrometheusLabel(labels[key]))
			builder.WriteByte('"')
		}
		builder.WriteByte('}')
	}
	builder.WriteByte(' ')
	builder.WriteString(value)
	builder.WriteByte('\n')
}

func escapePrometheusLabel(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\n", "\\n")
	return strings.ReplaceAll(value, "\"", "\\\"")
}

func boolGauge(enabled bool) string {
	if enabled {
		return "1"
	}
	return "0"
}

func formatFloat(value float64) string {
	return fmt.Sprintf("%.6f", value)
}

func durationMillis(latency string) float64 {
	if latency == "" {
		return -1
	}
	parsed, err := time.ParseDuration(latency)
	if err != nil {
		return -1
	}
	return float64(parsed.Milliseconds())
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

func (r *Router) runDependencyCheck(name string, checker HealthChecker) ComponentStatus {
	if checker == nil {
		return ComponentStatus{
			Status:  "disabled",
			Message: name + " health check not configured",
		}
	}

	start := time.Now()
	if err := checker.Check(); err != nil {
		return ComponentStatus{
			Status:  "unhealthy",
			Message: err.Error(),
			Latency: time.Since(start).String(),
		}
	}

	return ComponentStatus{
		Status:  "healthy",
		Latency: time.Since(start).String(),
	}
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
		return errors.New("database health check is not configured")
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
		return errors.New("cache health check is not configured")
	}
	return c.check()
}
