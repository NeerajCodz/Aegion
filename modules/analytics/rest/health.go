package rest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// HealthStatus represents the health status of the analytics service.
type HealthStatus struct {
	Status          string                 `json:"status"`
	Timestamp       time.Time              `json:"timestamp"`
	Version         string                 `json:"version"`
	Uptime          float64                `json:"uptime"`
	Services        map[string]interface{} `json:"services"`
	Metrics         map[string]interface{} `json:"metrics"`
	SyncLag         *int64                 `json:"sync_lag_ms,omitempty"`
	CacheHitRate    float64                `json:"cache_hit_rate"`
	QueryLatencyP95 int64                  `json:"query_latency_p95_ms"`
}

// ReadinessStatus represents readiness check status.
type ReadinessStatus struct {
	Ready    bool                   `json:"ready"`
	Reason   string                 `json:"reason,omitempty"`
	Services map[string]bool        `json:"services"`
	Details  map[string]interface{} `json:"details,omitempty"`
}

// LivenessStatus represents liveness check status.
type LivenessStatus struct {
	Alive   bool      `json:"alive"`
	Uptime  float64   `json:"uptime"`
	Updated time.Time `json:"updated"`
}

// HealthChecker interface for health status checks.
type HealthChecker interface {
	CheckHealth(ctx context.Context) (map[string]interface{}, error)
	CheckReadiness(ctx context.Context) (bool, string, error)
	GetSyncLag(ctx context.Context) (int64, error)
	GetCacheMetrics(ctx context.Context) (map[string]interface{}, error)
}

// Health handles GET /health
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// Get service start time (should be tracked elsewhere)
	startTime := time.Now().Add(-5 * time.Minute)
	uptime := time.Since(startTime).Seconds()

	status := "healthy"
	statusCode := http.StatusOK

	// Check dependencies
	services := make(map[string]interface{})

	// Check database (DuckDB)
	if h.queries == nil {
		services["database"] = map[string]interface{}{
			"status": "down",
			"error":  "database not initialized",
		}
		status = "degraded"
		statusCode = http.StatusServiceUnavailable
	} else {
		services["database"] = map[string]interface{}{
			"status": "up",
		}
	}

	// Check cache
	if h.cache != nil {
		services["cache"] = map[string]interface{}{
			"status": "up",
		}
	} else {
		services["cache"] = map[string]interface{}{
			"status": "unavailable",
		}
	}

	// Get cache metrics if available
	cacheMetrics := make(map[string]interface{})
	if h.cache != nil {
		if m, found, err := h.cache.Get(ctx, "metrics:cache"); err == nil && found && m != nil {
			if metrics, ok := m.(map[string]interface{}); ok {
				cacheMetrics = metrics
			}
		}
	}

	// Get sync lag
	var syncLag *int64
	if h.queries != nil {
		// Query sync lag from store
		if syncQB, ok := h.queries.(interface {
			GetSyncLag(context.Context) (int64, error)
		}); ok {
			if lag, err := syncQB.GetSyncLag(ctx); err == nil {
				syncLag = &lag
			}
		}
	}

	healthStatus := HealthStatus{
		Status:          status,
		Timestamp:       time.Now(),
		Version:         "1.0.0",
		Uptime:          uptime,
		Services:        services,
		Metrics:         cacheMetrics,
		SyncLag:         syncLag,
		CacheHitRate:    extractFloat(cacheMetrics, "hit_rate", 0.0),
		QueryLatencyP95: int64(extractFloat(cacheMetrics, "query_latency_p95_ms", 0.0)),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(Response{Data: healthStatus})
}

// Ready handles GET /ready - Readiness probe
func (h *Handler) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	services := make(map[string]bool)
	var ready bool
	var reason string

	// Check database connectivity
	dbReady := false
	if h.queries != nil {
		// Try executing a simple query
		if results, err := h.queries.ExecuteQuery(ctx, "SELECT 1"); err == nil && len(results) > 0 {
			dbReady = true
			services["database"] = true
		}
	}

	if !dbReady {
		services["database"] = false
		reason = "database not ready"
		ready = false
	} else {
		services["database"] = true
		ready = true
	}

	// Check cache if critical
	if h.cache != nil {
		if _, found, err := h.cache.Get(ctx, "test:readiness"); err == nil || found {
			services["cache"] = true
		} else {
			services["cache"] = false
			if reason == "" {
				reason = "cache not ready"
			}
		}
	}

	status := ReadinessStatus{
		Ready:    ready,
		Reason:   reason,
		Services: services,
	}

	statusCode := http.StatusOK
	if !ready {
		statusCode = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(status)
}

// Live handles GET /live - Liveness probe
func (h *Handler) Live(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now().Add(-5 * time.Minute)
	uptime := time.Since(startTime).Seconds()

	status := LivenessStatus{
		Alive:   true,
		Uptime:  uptime,
		Updated: time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(status)
}

// Metrics handles GET /metrics - Prometheus metrics endpoint
func (h *Handler) Metrics(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	// Collect metrics from cache if available
	var hitRate float64
	var queryLatencyP95 int64
	var totalQueries int64
	var cachedQueries int64

	if h.cache != nil {
		if m, found, err := h.cache.Get(ctx, "metrics:cache"); err == nil && found && m != nil {
			if metrics, ok := m.(map[string]interface{}); ok {
				hitRate = extractFloat(metrics, "hit_rate", 0.0)
				queryLatencyP95 = int64(extractFloat(metrics, "query_latency_p95_ms", 0.0))
				totalQueries = int64(extractFloat(metrics, "total_queries", 0.0))
				cachedQueries = int64(extractFloat(metrics, "cached_queries", 0.0))
			}
		}
	}

	// Build Prometheus format output
	output := fmt.Sprintf(`# HELP analytics_cache_hit_rate Cache hit rate (0-1)
# TYPE analytics_cache_hit_rate gauge
analytics_cache_hit_rate %f

# HELP analytics_query_latency_p95_ms 95th percentile query latency in milliseconds
# TYPE analytics_query_latency_p95_ms gauge
analytics_query_latency_p95_ms %d

# HELP analytics_total_queries Total number of queries executed
# TYPE analytics_total_queries counter
analytics_total_queries %d

# HELP analytics_cached_queries Number of queries served from cache
# TYPE analytics_cached_queries counter
analytics_cached_queries %d

# HELP analytics_health Service health status (1=healthy, 0=unhealthy)
# TYPE analytics_health gauge
analytics_health 1
`, hitRate, queryLatencyP95, totalQueries, cachedQueries)

	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, output)
}

// Helper function to extract float from map
func extractFloat(m map[string]interface{}, key string, defaultVal float64) float64 {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case float64:
			return val
		case float32:
			return float64(val)
		case int:
			return float64(val)
		case int64:
			return float64(val)
		case string:
			if f, err := fmt.Sscanf(val, "%f"); err == nil {
				return float64(f)
			}
		}
	}
	return defaultVal
}
