// Package analytics provides OLAP analytics capabilities using DuckDB.
package analytics

import "time"

// Event represents an analytics event.
type Event struct {
	ID        string                 `json:"id"`
	Category  string                 `json:"category"`
	EventType string                 `json:"event_type"`
	Data      map[string]interface{} `json:"data"`
	UserID    *string                `json:"user_id,omitempty"`
	SessionID *string                `json:"session_id,omitempty"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
}

// Metric represents an aggregated metric.
type Metric struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Category  string    `json:"category"`
	Value     float64   `json:"value"`
	Unit      string    `json:"unit,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Dashboard represents a saved analytics dashboard.
type Dashboard struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Config      map[string]interface{} `json:"config"`
	OwnerID     string                 `json:"owner_id"`
	Public      bool                   `json:"public"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

// Query represents a saved analytics query.
type Query struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	SQL         string    `json:"sql"`
	OwnerID     string    `json:"owner_id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Webhook represents a webhook subscription for analytics events.
type Webhook struct {
	ID           string                 `json:"id"`
	UserID       string                 `json:"user_id"`
	URL          string                 `json:"url"`
	EventTypes   []string               `json:"event_types"`             // Support for multiple event types
	Categories   []string               `json:"categories,omitempty"`    // Support for category filters
	CustomFilter map[string]interface{} `json:"custom_filter,omitempty"` // Advanced JSON filters
	Secret       string                 `json:"secret"`
	Active       bool                   `json:"active"`
	FailureCount int                    `json:"failure_count"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
}

// WebhookEventPayload is the payload sent to webhook endpoints.
type WebhookEventPayload struct {
	ID         string                 `json:"id"`
	Timestamp  time.Time              `json:"timestamp"`
	EventType  string                 `json:"event_type"`
	Category   string                 `json:"category"`
	Data       map[string]interface{} `json:"data"`
	Attempts   int                    `json:"attempts"`
	Signatures map[string]string      `json:"signatures"`
}

// WebhookDelivery tracks a webhook delivery attempt.
type WebhookDelivery struct {
	ID            string     `json:"id"`
	WebhookID     string     `json:"webhook_id"`
	EventID       string     `json:"event_id"`
	Status        string     `json:"status"` // "pending", "success", "failed", "retrying"
	StatusCode    int        `json:"status_code,omitempty"`
	ResponseBody  string     `json:"response_body,omitempty"`
	Error         string     `json:"error,omitempty"`
	Attempts      int        `json:"attempts"`
	MaxRetries    int        `json:"max_retries"`
	NextRetryAt   *time.Time `json:"next_retry_at,omitempty"`
	LastAttemptAt time.Time  `json:"last_attempt_at"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// DLQWebhookEvent represents a webhook event that failed and was moved to DLQ.
type DLQWebhookEvent struct {
	ID          string                 `json:"id"`
	WebhookID   string                 `json:"webhook_id"`
	EventID     string                 `json:"event_id"`
	EventData   map[string]interface{} `json:"event_data"`
	ErrorMsg    string                 `json:"error_msg"`
	RetryCount  int                    `json:"retry_count"`
	LastErrorAt time.Time              `json:"last_error_at"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

// HealthStatus represents the health check status.
type HealthStatus struct {
	DuckDB        bool      `json:"duckdb"`
	Storage       bool      `json:"storage"`
	Migrations    bool      `json:"migrations"`
	LastCheckTime time.Time `json:"last_check_time"`
	Status        string    `json:"status"` // "healthy" or "degraded"
}

// ==================== Sync Models ====================

// SyncEvent represents a data event ready for syncing.
type SyncEvent struct {
	ID           string                 `json:"id"`
	SourceTable  string                 `json:"source_table"`
	SourceRecord map[string]interface{} `json:"source_record"`
	EventType    string                 `json:"event_type"` // "insert", "update", "delete"
	Timestamp    time.Time              `json:"timestamp"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// SyncPosition tracks the progress of a sync operation.
type SyncPosition struct {
	ID             string                 `json:"id"`
	Strategy       string                 `json:"strategy"` // "real_time", "batch", "async"
	SourceTable    string                 `json:"source_table"`
	LastSyncedID   *string                `json:"last_synced_id,omitempty"`
	LastSyncedAt   *time.Time             `json:"last_synced_at,omitempty"`
	CheckpointData map[string]interface{} `json:"checkpoint_data,omitempty"`
	CreatedAt      time.Time              `json:"created_at"`
	UpdatedAt      time.Time              `json:"updated_at"`
}

// SyncEventRecord represents a logged sync operation.
type SyncEventRecord struct {
	ID            string                 `json:"id"`
	Strategy      string                 `json:"strategy"`
	EventType     string                 `json:"event_type"` // "sync_start", "sync_complete", "sync_error"
	SourceTable   *string                `json:"source_table,omitempty"`
	RecordsSynced *int                   `json:"records_synced,omitempty"`
	ErrorMessage  *string                `json:"error_message,omitempty"`
	DurationMs    *int                   `json:"duration_ms,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt     time.Time              `json:"created_at"`
}

// DLQEvent represents a dead-lettered event that failed to sync.
type DLQEvent struct {
	ID           string                 `json:"id"`
	EventData    map[string]interface{} `json:"event_data"`
	ErrorMessage string                 `json:"error_message"`
	RetryCount   int                    `json:"retry_count"`
	LastErrorAt  time.Time              `json:"last_error_at"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
}

// SyncHealthStatus represents health information for sync operations.
type SyncHealthStatus struct {
	Overall         string                 `json:"overall"` // "healthy", "degraded", "unhealthy"
	RealTimeSync    StrategyHealthStatus   `json:"real_time_sync"`
	BatchSync       StrategyHealthStatus   `json:"batch_sync"`
	AsyncSync       StrategyHealthStatus   `json:"async_sync"`
	LastSyncEventAt *time.Time             `json:"last_sync_event_at,omitempty"`
	SyncPositions   []SyncPosition         `json:"sync_positions,omitempty"`
	ErrorMetrics    map[string]interface{} `json:"error_metrics,omitempty"`
	LastCheckTime   time.Time              `json:"last_check_time"`
}

// StrategyHealthStatus represents health status of a single sync strategy.
type StrategyHealthStatus struct {
	Enabled       bool       `json:"enabled"`
	Healthy       bool       `json:"healthy"`
	LastSyncAt    *time.Time `json:"last_sync_at,omitempty"`
	SyncLagMs     int64      `json:"sync_lag_ms"`
	ErrorCount    int        `json:"error_count"`
	WarningCount  int        `json:"warning_count"`
	LastErrorMsg  *string    `json:"last_error_msg,omitempty"`
	PositionCount int        `json:"position_count"` // number of tracked positions
}
