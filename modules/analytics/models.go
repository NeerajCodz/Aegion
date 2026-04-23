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

// Webhook represents a webhook for analytics events.
type Webhook struct {
	ID        string    `json:"id"`
	URL       string    `json:"url"`
	EventType string    `json:"event_type"`
	Secret    string    `json:"secret"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// HealthStatus represents the health check status.
type HealthStatus struct {
	DuckDB        bool      `json:"duckdb"`
	Storage       bool      `json:"storage"`
	Migrations    bool      `json:"migrations"`
	LastCheckTime time.Time `json:"last_check_time"`
	Status        string    `json:"status"` // "healthy" or "degraded"
}
