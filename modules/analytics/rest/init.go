package rest

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/aegion/aegion/internal/platform/logger"
)

// InitParams holds parameters for initializing the REST API module
type InitParams struct {
	Config         Config
	Logger         *slog.Logger
	DB             Database
	Validator      *Validator
	WebhookManager WebhookManager
}

// Initialize sets up the REST API module
func Initialize(params InitParams) (*Handler, error) {
	// Create a default logger if not provided
	if params.Logger == nil {
		params.Logger = logger.New(logger.Config{Level: "info", Format: "json"}).Logger
	}

	// Validate config
	if params.Config.QueryTimeoutSeconds == 0 {
		params.Config.QueryTimeoutSeconds = 300
	}
	if params.Config.MaxPageSize == 0 {
		params.Config.MaxPageSize = 10000
	}
	if params.Config.DefaultPageSize == 0 {
		params.Config.DefaultPageSize = 100
	}
	if params.Config.RateLimit == 0 {
		params.Config.RateLimit = 1000
	}
	if params.Config.ResultCacheTTLMinutes == 0 {
		params.Config.ResultCacheTTLMinutes = 15
	}

	if params.DB == nil {
		return nil, fmt.Errorf("database interface is required")
	}

	if params.Validator == nil {
		params.Validator = NewValidator()
	}

	// Create query builder
	queryBuilder := NewQueryBuilder(params.DB)

	// Create export builder
	exportBuilder := NewExportBuilder(params.DB)

	// Create cache
	cache := NewCache()

	// Create handler
	handler := NewHandler(HandlerDeps{
		Logger:         params.Logger,
		Config:         params.Config,
		Queries:        queryBuilder,
		Exports:        exportBuilder,
		Cache:          cache,
		Validator:      params.Validator,
		WebhookManager: params.WebhookManager,
	})

	params.Logger.Info("REST API module initialized",
		"base_path", params.Config.BasePath,
		"rate_limit", params.Config.RateLimit,
		"query_timeout_seconds", params.Config.QueryTimeoutSeconds,
	)

	return handler, nil
}

// HealthCheck performs a health check of the REST API
func HealthCheck(ctx context.Context, handler *Handler) error {
	healthCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Check if handler is properly initialized
	if handler == nil {
		return fmt.Errorf("handler is not initialized")
	}

	// Check if context is valid
	if err := ValidateContext(healthCtx); err != nil {
		return err
	}

	return nil
}
