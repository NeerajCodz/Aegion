package sync

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/aegion/aegion/internal/xlog"
	"github.com/aegion/aegion/modules/analytics"
)

// HybridSync implements a hybrid sync strategy with fallback mechanism.
type HybridSync struct {
	primaryStrategy   Strategy
	fallbackStrategy  Strategy
	logger            *xlog.Logger
	mu                sync.RWMutex
	primaryHealthy    bool
	lastPrimaryError  *string
	fallbackHealthy   bool
	lastFallbackError *string
}

// NewHybridSync creates a new hybrid sync strategy instance.
func NewHybridSync(
	primaryStrategy Strategy,
	fallbackStrategy Strategy,
	logger *xlog.Logger,
) *HybridSync {
	if logger == nil {
		logger = xlog.Default()
	}
	return &HybridSync{
		primaryStrategy:  primaryStrategy,
		fallbackStrategy: fallbackStrategy,
		logger:           logger,
		primaryHealthy:   true,
		fallbackHealthy:  true,
	}
}

// Name returns the strategy identifier.
func (h *HybridSync) Name() string {
	return "hybrid"
}

// Start initializes hybrid sync and begins processing.
func (h *HybridSync) Start(ctx context.Context) error {
	var primaryErr error
	var fallbackErr error

	h.logger.Info("starting hybrid sync strategy",
		"primary", h.primaryStrategy.Name(),
		"fallback", h.fallbackStrategy.Name(),
	)

	if h.primaryStrategy.IsEnabled() {
		if err := h.primaryStrategy.Start(ctx); err != nil {
			h.logger.Error("error starting primary strategy", "err", err)
			primaryErr = err
			h.mu.Lock()
			h.primaryHealthy = false
			msg := err.Error()
			h.lastPrimaryError = &msg
			h.mu.Unlock()
		}
	}

	if h.fallbackStrategy.IsEnabled() {
		if err := h.fallbackStrategy.Start(ctx); err != nil {
			h.logger.Error("error starting fallback strategy", "err", err)
			fallbackErr = err
			h.mu.Lock()
			h.fallbackHealthy = false
			msg := err.Error()
			h.lastFallbackError = &msg
			h.mu.Unlock()
		}
	}

	if primaryErr != nil && fallbackErr != nil {
		return fmt.Errorf("both primary and fallback strategies failed to start: primary=%v, fallback=%v", primaryErr, fallbackErr)
	}

	return nil
}

// Stop gracefully shuts down hybrid sync.
func (h *HybridSync) Stop(ctx context.Context) error {
	h.logger.Info("stopping hybrid sync strategy")

	var primaryErr, fallbackErr error

	if err := h.primaryStrategy.Stop(ctx); err != nil {
		h.logger.Error("error stopping primary strategy", "err", err)
		primaryErr = err
	}

	if err := h.fallbackStrategy.Stop(ctx); err != nil {
		h.logger.Error("error stopping fallback strategy", "err", err)
		fallbackErr = err
	}

	if primaryErr != nil && fallbackErr != nil {
		return fmt.Errorf("errors stopping strategies: primary=%v, fallback=%v", primaryErr, fallbackErr)
	}

	return nil
}

// PublishEvent publishes an event, trying primary first with fallback.
func (h *HybridSync) PublishEvent(ctx context.Context, event *analytics.SyncEvent) error {
	h.mu.RLock()
	primaryHealthy := h.primaryHealthy
	fallbackHealthy := h.fallbackHealthy
	h.mu.RUnlock()

	var primaryErr error
	var fallbackErr error

	// Try primary strategy first
	if primaryHealthy && h.primaryStrategy.IsEnabled() {
		if err := h.primaryStrategy.PublishEvent(ctx, event); err == nil {
			return nil
		} else {
			h.logger.Warn("primary strategy failed, falling back",
				"primary_strategy", h.primaryStrategy.Name(),
				"err", err,
			)
			primaryErr = err

			h.mu.Lock()
			h.primaryHealthy = false
			msg := err.Error()
			h.lastPrimaryError = &msg
			h.mu.Unlock()
		}
	}

	// Fallback to secondary strategy
	if fallbackHealthy && h.fallbackStrategy.IsEnabled() {
		if err := h.fallbackStrategy.PublishEvent(ctx, event); err == nil {
			h.logger.Info("event published via fallback strategy",
				"fallback_strategy", h.fallbackStrategy.Name(),
			)
			return nil
		} else {
			h.logger.Error("fallback strategy also failed",
				"fallback_strategy", h.fallbackStrategy.Name(),
				"err", err,
			)
			fallbackErr = err

			h.mu.Lock()
			h.fallbackHealthy = false
			msg := err.Error()
			h.lastFallbackError = &msg
			h.mu.Unlock()
		}
	}

	// Both strategies failed
	if primaryErr != nil && fallbackErr != nil {
		return fmt.Errorf("both primary and fallback strategies failed: primary=%v, fallback=%v", primaryErr, fallbackErr)
	}

	if primaryErr != nil {
		return primaryErr
	}

	return fallbackErr
}

// Health returns the current health status of hybrid sync.
func (h *HybridSync) Health(ctx context.Context) (*analytics.StrategyHealthStatus, error) {
	h.mu.RLock()
	primaryHealthy := h.primaryHealthy
	fallbackHealthy := h.fallbackHealthy
	lastPrimaryError := h.lastPrimaryError
	lastFallbackError := h.lastFallbackError
	h.mu.RUnlock()

	// Get health from both strategies
	var primaryHealth, fallbackHealth *analytics.StrategyHealthStatus

	if h.primaryStrategy.IsEnabled() {
		health, err := h.primaryStrategy.Health(ctx)
		if err != nil {
			h.logger.Error("error getting primary strategy health", "err", err)
		} else {
			primaryHealth = health
		}
	}

	if h.fallbackStrategy.IsEnabled() {
		health, err := h.fallbackStrategy.Health(ctx)
		if err != nil {
			h.logger.Error("error getting fallback strategy health", "err", err)
		} else {
			fallbackHealth = health
		}
	}

	// Combine health information
	isHealthy := primaryHealthy && fallbackHealthy
	var lastError *string
	if lastPrimaryError != nil {
		lastError = lastPrimaryError
	} else if lastFallbackError != nil {
		lastError = lastFallbackError
	}

	totalErrors := 0
	totalWarnings := 0
	totalPositions := 0
	var lastSyncAt *time.Time

	if primaryHealth != nil {
		totalErrors += primaryHealth.ErrorCount
		totalWarnings += primaryHealth.WarningCount
		totalPositions += primaryHealth.PositionCount
		if primaryHealth.LastSyncAt != nil && (lastSyncAt == nil || primaryHealth.LastSyncAt.After(*lastSyncAt)) {
			lastSyncAt = primaryHealth.LastSyncAt
		}
	}

	if fallbackHealth != nil {
		totalErrors += fallbackHealth.ErrorCount
		totalWarnings += fallbackHealth.WarningCount
		totalPositions += fallbackHealth.PositionCount
		if fallbackHealth.LastSyncAt != nil && (lastSyncAt == nil || fallbackHealth.LastSyncAt.After(*lastSyncAt)) {
			lastSyncAt = fallbackHealth.LastSyncAt
		}
	}

	status := &analytics.StrategyHealthStatus{
		Enabled:       true,
		Healthy:       isHealthy,
		LastSyncAt:    lastSyncAt,
		SyncLagMs:     0,
		ErrorCount:    totalErrors,
		WarningCount:  totalWarnings,
		LastErrorMsg:  lastError,
		PositionCount: totalPositions,
	}

	return status, nil
}

// GetPosition returns the current sync position for a table.
func (h *HybridSync) GetPosition(ctx context.Context, table string) (*analytics.SyncPosition, error) {
	// Try primary first
	if h.primaryStrategy.IsEnabled() {
		if pos, err := h.primaryStrategy.GetPosition(ctx, table); err == nil {
			return pos, nil
		}
	}

	// Fallback to secondary
	if h.fallbackStrategy.IsEnabled() {
		return h.fallbackStrategy.GetPosition(ctx, table)
	}

	return nil, fmt.Errorf("no strategy available to get position")
}

// SetPosition updates the sync position for a table.
func (h *HybridSync) SetPosition(ctx context.Context, position *analytics.SyncPosition) error {
	var primaryErr, fallbackErr error

	// Update in primary
	if h.primaryStrategy.IsEnabled() {
		if err := h.primaryStrategy.SetPosition(ctx, position); err != nil {
			h.logger.Warn("error setting position in primary strategy", "err", err)
			primaryErr = err
		}
	}

	// Update in fallback
	if h.fallbackStrategy.IsEnabled() {
		if err := h.fallbackStrategy.SetPosition(ctx, position); err != nil {
			h.logger.Warn("error setting position in fallback strategy", "err", err)
			fallbackErr = err
		}
	}

	// At least one should succeed
	if primaryErr != nil && fallbackErr != nil {
		return fmt.Errorf("failed to set position in both strategies")
	}

	return nil
}

// IsEnabled returns whether hybrid sync is enabled.
func (h *HybridSync) IsEnabled() bool {
	return (h.primaryStrategy.IsEnabled() || h.fallbackStrategy.IsEnabled())
}

// RecoverPrimary attempts to recover the primary strategy.
func (h *HybridSync) RecoverPrimary(ctx context.Context) error {
	h.logger.Info("attempting to recover primary strategy", "strategy", h.primaryStrategy.Name())

	if err := h.primaryStrategy.Start(ctx); err != nil {
		h.logger.Error("failed to recover primary strategy", "err", err)
		return err
	}

	h.mu.Lock()
	h.primaryHealthy = true
	h.lastPrimaryError = nil
	h.mu.Unlock()

	h.logger.Info("primary strategy recovered")
	return nil
}
