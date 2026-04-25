package dashboards

import (
	"time"
)

// Dashboard represents a pre-built or custom analytics dashboard.
type Dashboard struct {
	ID                string                 `json:"id"`
	Name              string                 `json:"name"`
	Description       string                 `json:"description,omitempty"`
	Category          string                 `json:"category"`
	IsDefault         bool                   `json:"is_default"`
	Layout            string                 `json:"layout"` // "grid-3col", "grid-4col", "flex"
	RefreshInterval   int                    `json:"refresh_interval_seconds"`
	Components        []Component            `json:"components"`
	Config            map[string]interface{} `json:"config,omitempty"`
	OwnerID           *string                `json:"owner_id,omitempty"`
	Public            bool                   `json:"public"`
	ShareToken        *string                `json:"share_token,omitempty"`
	Pinned            bool                   `json:"pinned"`
	CreatedAt         time.Time              `json:"created_at"`
	UpdatedAt         time.Time              `json:"updated_at"`
}

// Component represents a widget/chart in a dashboard.
type Component struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"` // "time_series", "pie_chart", "gauge", etc.
	Title       string                 `json:"title"`
	Description string                 `json:"description,omitempty"`
	QueryID     string                 `json:"query_id"`
	TimeRange   string                 `json:"time_range"` // "1h", "1d", "7d", "30d"
	Metrics     []string               `json:"metrics"`
	Config      map[string]interface{} `json:"config,omitempty"`
	GridCol     int                    `json:"grid_col"`
	GridRow     int                    `json:"grid_row"`
	GridWidth   int                    `json:"grid_width"`
	GridHeight  int                    `json:"grid_height"`
}

// DashboardQuery represents a pre-defined query for dashboard use.
type DashboardQuery struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Category    string                 `json:"category"`
	SQL         string                 `json:"sql"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
	CacheTTL    int                    `json:"cache_ttl_seconds"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

// DashboardMetadata stores computed metrics for dashboards.
type DashboardMetadata struct {
	ID            string    `json:"id"`
	DashboardID   string    `json:"dashboard_id"`
	MetricName    string    `json:"metric_name"`
	LastComputed  time.Time `json:"last_computed"`
	NextCompute   time.Time `json:"next_compute"`
	ComputeStatus string    `json:"compute_status"` // "pending", "running", "completed", "failed"
	ErrorMessage  *string   `json:"error_message,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// DashboardConfig holds dashboard-specific configuration.
type DashboardConfig struct {
	AutoRefreshInterval int    `json:"auto_refresh_interval_seconds"`
	DefaultTimeRange    string `json:"default_time_range_days"`
	MaxCustomDashboards int    `json:"max_custom_dashboards"`
	EnableSharing       bool   `json:"enable_sharing"`
	EnableScheduledReports bool `json:"enable_scheduled_reports"`
}

// QueryResult represents the result of a dashboard query.
type QueryResult struct {
	QueryID       string                   `json:"query_id"`
	Data          []map[string]interface{} `json:"data"`
	Columns       []string                 `json:"columns"`
	RowCount      int                      `json:"row_count"`
	ExecutionTime int                      `json:"execution_time_ms"`
	FromCache     bool                     `json:"from_cache"`
	CachedAt      *time.Time               `json:"cached_at,omitempty"`
}

// ExportRequest represents a request to export dashboard data.
type ExportRequest struct {
	DashboardID string `json:"dashboard_id"`
	Format      string `json:"format"` // "csv", "json", "pdf"
	TimeRange   string `json:"time_range"`
	IncludeCharts bool `json:"include_charts"`
}

// AlertThreshold represents an alert configuration for a dashboard metric.
type AlertThreshold struct {
	ID            string      `json:"id"`
	DashboardID   string      `json:"dashboard_id"`
	MetricName    string      `json:"metric_name"`
	Operator      string      `json:"operator"` // "gt", "lt", "eq", "gte", "lte"
	Threshold     float64     `json:"threshold"`
	SeverityLevel string      `json:"severity_level"` // "info", "warning", "critical"
	Enabled       bool        `json:"enabled"`
	CreatedAt     time.Time   `json:"created_at"`
	UpdatedAt     time.Time   `json:"updated_at"`
}

// DashboardShare represents a shared dashboard link.
type DashboardShare struct {
	ID          string     `json:"id"`
	DashboardID string     `json:"dashboard_id"`
	Token       string     `json:"token"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	ReadOnly    bool       `json:"read_only"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}
