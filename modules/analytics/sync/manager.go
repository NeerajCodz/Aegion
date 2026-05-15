package sync

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/aegion/aegion/internal/xlog"
	"github.com/aegion/aegion/modules/analytics"
)

// Manager orchestrates all sync strategies and provides a unified interface.
type Manager struct {
	strategies    map[string]Strategy
	strategyOrder []string
	logger        *xlog.Logger
	mu            sync.RWMutex
	isRunning     bool
	rateLimiters  map[string]*RateLimiter
	deduplicator  *EventDeduplicator
}

// NewManager creates a new sync manager instance.
func NewManager(logger *xlog.Logger) *Manager {
	if logger == nil {
		logger = xlog.Default()
	}
	return &Manager{
		strategies:    make(map[string]Strategy),
		strategyOrder: make([]string, 0),
		logger:        logger,
		rateLimiters:  make(map[string]*RateLimiter),
		deduplicator:  NewEventDeduplicator(10 * time.Minute),
	}
}

// RegisterStrategy registers a sync strategy.
func (m *Manager) RegisterStrategy(strategy Strategy) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	name := strategy.Name()
	if _, exists := m.strategies[name]; exists {
		return fmt.Errorf("strategy %s already registered", name)
	}

	m.strategies[name] = strategy
	m.strategyOrder = append(m.strategyOrder, name)
	m.rateLimiters[name] = NewRateLimiter(100, 1*time.Second) // 100 events per second

	m.logger.Info("registered sync strategy", "strategy", name)
	return nil
}

// Start initializes all registered strategies.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.isRunning {
		m.mu.Unlock()
		return fmt.Errorf("sync manager already running")
	}
	m.isRunning = true
	m.mu.Unlock()

	m.logger.Info("starting sync manager", "strategies", len(m.strategies))

	var errors []error
	for _, name := range m.strategyOrder {
		m.mu.RLock()
		strategy := m.strategies[name]
		m.mu.RUnlock()

		if strategy.IsEnabled() {
			if err := strategy.Start(ctx); err != nil {
				m.logger.Error("error starting strategy", "strategy", name, "err", err)
				errors = append(errors, fmt.Errorf("%s: %w", name, err))
			}
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("errors starting strategies: %v", errors)
	}

	return nil
}

// Stop gracefully shuts down all strategies.
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	if !m.isRunning {
		m.mu.Unlock()
		return nil
	}
	m.isRunning = false
	m.mu.Unlock()

	m.logger.Info("stopping sync manager")

	var errors []error
	for _, name := range m.strategyOrder {
		m.mu.RLock()
		strategy := m.strategies[name]
		m.mu.RUnlock()

		if strategy.IsEnabled() {
			if err := strategy.Stop(ctx); err != nil {
				m.logger.Error("error stopping strategy", "strategy", name, "err", err)
				errors = append(errors, fmt.Errorf("%s: %w", name, err))
			}
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("errors stopping strategies: %v", errors)
	}

	return nil
}

// PublishEvent publishes an event to all appropriate strategies.
func (m *Manager) PublishEvent(ctx context.Context, event *analytics.SyncEvent) error {
	// Deduplication
	if m.deduplicator.IsDuplicate(event.ID) {
		m.logger.Warn("duplicate event detected and skipped", "event_id", event.ID)
		return nil
	}

	m.mu.RLock()
	strategies := make([]Strategy, 0, len(m.strategyOrder))
	for _, name := range m.strategyOrder {
		strategies = append(strategies, m.strategies[name])
	}
	m.mu.RUnlock()

	var errors []error
	for _, strategy := range strategies {
		if !strategy.IsEnabled() {
			continue
		}

		strategyName := strategy.Name()

		// Rate limiting
		if !m.rateLimiters[strategyName].Allow() {
			m.logger.Warn("rate limit exceeded for strategy", "strategy", strategyName)
			continue
		}

		if err := strategy.PublishEvent(ctx, event); err != nil {
			m.logger.Error("error publishing event to strategy", "strategy", strategyName, "err", err)
			errors = append(errors, fmt.Errorf("%s: %w", strategyName, err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("errors publishing to strategies: %v", errors)
	}

	return nil
}

// Health returns aggregated health status across all strategies.
func (m *Manager) Health(ctx context.Context) (*analytics.SyncHealthStatus, error) {
	m.mu.RLock()
	strategies := make([]Strategy, 0, len(m.strategyOrder))
	for _, name := range m.strategyOrder {
		strategies = append(strategies, m.strategies[name])
	}
	m.mu.RUnlock()

	status := &analytics.SyncHealthStatus{
		Overall:       "healthy",
		LastCheckTime: time.Now(),
		ErrorMetrics:  make(map[string]interface{}),
		SyncPositions: make([]analytics.SyncPosition, 0),
	}

	for _, strategy := range strategies {
		if !strategy.IsEnabled() {
			continue
		}

		strategyName := strategy.Name()
		health, err := strategy.Health(ctx)
		if err != nil {
			m.logger.Error("error getting health for strategy", "strategy", strategyName, "err", err)
			status.Overall = "degraded"
			continue
		}

		// Collect health info based on strategy type
		switch strategyName {
		case "real_time":
			status.RealTimeSync = *health
		case "batch":
			status.BatchSync = *health
		case "async":
			status.AsyncSync = *health
		case "hybrid":
			// Hybrid already aggregates its children
		}

		// Mark overall as unhealthy if any strategy is unhealthy
		if !health.Healthy && health.ErrorCount > 0 {
			status.Overall = "degraded"
		}

		// Track error metrics
		status.ErrorMetrics[strategyName] = map[string]interface{}{
			"error_count":   health.ErrorCount,
			"warning_count": health.WarningCount,
			"sync_lag_ms":   health.SyncLagMs,
		}
	}

	// Aggregate sync positions
	for _, strategy := range strategies {
		if !strategy.IsEnabled() {
			continue
		}

		for _, table := range []string{"audit_events", "sessions", "auth_logs"} {
			if pos, err := strategy.GetPosition(ctx, table); err == nil {
				status.SyncPositions = append(status.SyncPositions, *pos)
			}
		}
	}

	return status, nil
}

// GetPosition returns the current sync position for a given table.
func (m *Manager) GetPosition(ctx context.Context, table string) (*analytics.SyncPosition, error) {
	m.mu.RLock()
	strategies := make([]Strategy, 0, len(m.strategyOrder))
	for _, name := range m.strategyOrder {
		strategies = append(strategies, m.strategies[name])
	}
	m.mu.RUnlock()

	for _, strategy := range strategies {
		if !strategy.IsEnabled() {
			continue
		}

		if pos, err := strategy.GetPosition(ctx, table); err == nil {
			return pos, nil
		}
	}

	return nil, fmt.Errorf("no sync position found for table: %s", table)
}

// SetPosition updates the sync position for a given table across all strategies.
func (m *Manager) SetPosition(ctx context.Context, position *analytics.SyncPosition) error {
	m.mu.RLock()
	strategies := make([]Strategy, 0, len(m.strategyOrder))
	for _, name := range m.strategyOrder {
		strategies = append(strategies, m.strategies[name])
	}
	m.mu.RUnlock()

	for _, strategy := range strategies {
		if !strategy.IsEnabled() {
			continue
		}

		if err := strategy.SetPosition(ctx, position); err != nil {
			m.logger.Error("error setting position for strategy", "strategy", strategy.Name(), "err", err)
		}
	}

	return nil
}

// GetStrategy returns a registered strategy by name.
func (m *Manager) GetStrategy(name string) (Strategy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	strategy, exists := m.strategies[name]
	if !exists {
		return nil, fmt.Errorf("strategy not found: %s", name)
	}

	return strategy, nil
}

// RateLimiter implements a simple token bucket rate limiter.
type RateLimiter struct {
	capacity       int64
	refillRate     int64
	refillInterval time.Duration
	tokens         int64
	lastRefill     time.Time
	mu             sync.Mutex
}

// NewRateLimiter creates a new rate limiter.
func NewRateLimiter(capacity int64, interval time.Duration) *RateLimiter {
	return &RateLimiter{
		capacity:       capacity,
		refillRate:     capacity,
		refillInterval: interval,
		tokens:         capacity,
		lastRefill:     time.Now(),
	}
}

// Allow checks if an event is allowed based on rate limit.
func (rl *RateLimiter) Allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(rl.lastRefill)
	if elapsed >= rl.refillInterval {
		rl.tokens = rl.capacity
		rl.lastRefill = now
	}

	if rl.tokens > 0 {
		rl.tokens--
		return true
	}

	return false
}

// EventDeduplicator prevents duplicate events from being processed.
type EventDeduplicator struct {
	seenEvents map[string]time.Time
	ttl        time.Duration
	mu         sync.RWMutex
	ticker     *time.Ticker
}

// NewEventDeduplicator creates a new event deduplicator.
func NewEventDeduplicator(ttl time.Duration) *EventDeduplicator {
	ed := &EventDeduplicator{
		seenEvents: make(map[string]time.Time),
		ttl:        ttl,
		ticker:     time.NewTicker(1 * time.Minute),
	}

	go ed.cleanupLoop()
	return ed
}

// IsDuplicate checks if an event has been seen before.
func (ed *EventDeduplicator) IsDuplicate(eventID string) bool {
	ed.mu.Lock()
	defer ed.mu.Unlock()

	if lastSeen, exists := ed.seenEvents[eventID]; exists && time.Since(lastSeen) < ed.ttl {
		return true
	}

	ed.seenEvents[eventID] = time.Now()
	return false
}

// cleanupLoop removes expired events periodically.
func (ed *EventDeduplicator) cleanupLoop() {
	for range ed.ticker.C {
		ed.mu.Lock()
		now := time.Now()
		for eventID, lastSeen := range ed.seenEvents {
			if now.Sub(lastSeen) > ed.ttl {
				delete(ed.seenEvents, eventID)
			}
		}
		ed.mu.Unlock()
	}
}
