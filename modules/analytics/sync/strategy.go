package sync

import (
	"context"

	"github.com/aegion/aegion/modules/analytics"
)

// Strategy defines the interface for sync strategies.
type Strategy interface {
	// Name returns the strategy identifier (e.g., "real_time", "batch", "async")
	Name() string

	// Start initializes the strategy and begins syncing
	Start(ctx context.Context) error

	// Stop gracefully shuts down the strategy
	Stop(ctx context.Context) error

	// PublishEvent publishes an event for syncing
	PublishEvent(ctx context.Context, event *analytics.SyncEvent) error

	// Health returns the current health status of the strategy
	Health(ctx context.Context) (*analytics.StrategyHealthStatus, error)

	// GetPosition returns the current sync position for a given table
	GetPosition(ctx context.Context, table string) (*analytics.SyncPosition, error)

	// SetPosition updates the sync position for a given table
	SetPosition(ctx context.Context, position *analytics.SyncPosition) error

	// IsEnabled returns whether this strategy is currently enabled
	IsEnabled() bool
}

// Config contains common configuration for sync strategies.
type Config struct {
	Enabled     bool
	MaxRetries  int
	RetryBackoffMs int
	Logger      Logger
	DB          DB
	DuckDB      DuckDB
}

// Logger defines a minimal logging interface.
type Logger interface {
	Debug(msg string, keysAndValues ...interface{})
	Info(msg string, keysAndValues ...interface{})
	Warn(msg string, keysAndValues ...interface{})
	Error(msg string, keysAndValues ...interface{})
}

// DB defines the PostgreSQL database interface.
type DB interface {
	Exec(ctx context.Context, sql string, args ...interface{}) error
	Query(ctx context.Context, sql string, args ...interface{}) ([]map[string]interface{}, error)
	QueryRow(ctx context.Context, sql string, args ...interface{}) (map[string]interface{}, error)
}

// DuckDB defines the DuckDB database interface.
type DuckDB interface {
	Exec(ctx context.Context, sql string, args ...interface{}) error
	Query(ctx context.Context, sql string, args ...interface{}) ([]map[string]interface{}, error)
	QueryRow(ctx context.Context, sql string, args ...interface{}) (map[string]interface{}, error)
}
