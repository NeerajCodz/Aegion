package retention

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"
)

// Manager orchestrates the entire retention system.
type Manager struct {
	db              *sql.DB
	policy          *RetentionPolicy
	executor        *ArchivalExecutor
	tieringEngine   *TieringEngine
	cleanupManager  *CleanupManager
	scheduler       *JobScheduler
	auditLog        AuditLog
	mu              sync.RWMutex
	initialized     bool
	storageBackends map[TierType]StorageBackendWriter
}

// NewManager creates a new retention manager.
func NewManager(db *sql.DB, policy *RetentionPolicy) *Manager {
	return &Manager{
		db:              db,
		policy:          policy,
		storageBackends: make(map[TierType]StorageBackendWriter),
	}
}

// RegisterStorageBackend registers a storage backend for a tier.
func (m *Manager) RegisterStorageBackend(tier TierType, backend StorageBackendWriter) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.storageBackends[tier] = backend
}

// Initialize sets up the retention system.
func (m *Manager) Initialize(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.initialized {
		return fmt.Errorf("manager already initialized")
	}

	// Validate policy
	if err := m.policy.Validate(); err != nil {
		return fmt.Errorf("policy validation failed: %w", err)
	}

	// Initialize audit log
	auditLog := NewDatabaseAuditLog(m.db)
	if err := auditLog.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize audit log: %w", err)
	}
	m.auditLog = auditLog

	// Create tables
	if err := m.createTables(ctx); err != nil {
		return fmt.Errorf("failed to create tables: %w", err)
	}

	// Initialize components
	m.executor = NewArchivalExecutor(m.db, m.policy, m.auditLog)
	m.tieringEngine = NewTieringEngine(m.db, m.policy, m.auditLog)
	m.cleanupManager = NewCleanupManager(m.db, m.policy, m.auditLog)

	// Register storage backends with executor
	for tier, backend := range m.storageBackends {
		m.executor.RegisterStorageBackend(tier, backend)
	}

	// Create scheduler
	scheduleConfig := DefaultScheduleConfig()
	m.scheduler = NewJobScheduler(scheduleConfig, m.executor, m.tieringEngine, m.cleanupManager, m.auditLog)

	m.initialized = true
	m.auditLog.LogMessage(ctx, "Manager", "Retention manager initialized successfully")

	return nil
}

// createTables creates the necessary database tables.
func (m *Manager) createTables(ctx context.Context) error {
	// Event categories table (referenced by analytics_events FK).
	categoriesQuery := `
		CREATE TABLE IF NOT EXISTS event_categories (
			name TEXT PRIMARY KEY,
			description TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`
	if _, err := m.db.ExecContext(ctx, categoriesQuery); err != nil {
		return fmt.Errorf("failed to create event_categories table: %w", err)
	}

	// Analytics events table (extended for retention)
	eventsQuery := `
		CREATE TABLE IF NOT EXISTS analytics_events (
			id TEXT PRIMARY KEY,
			category TEXT NOT NULL,
			event_type TEXT,
			tier TEXT DEFAULT 'hot',
			data TEXT,
			user_id TEXT,
			session_id TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			tier_updated_at TIMESTAMP,
			archived_at TIMESTAMP,
			archive_path TEXT,
			deleted_at TIMESTAMP,
			FOREIGN KEY (category) REFERENCES event_categories(name)
		)
	`

	if _, err := m.db.ExecContext(ctx, eventsQuery); err != nil {
		return fmt.Errorf("failed to create analytics_events table: %w", err)
	}

	// Tier transitions table
	tierTransitionsQuery := `
		CREATE TABLE IF NOT EXISTS tier_transitions (
			id TEXT PRIMARY KEY,
			category TEXT NOT NULL,
			from_tier TEXT NOT NULL,
			to_tier TEXT NOT NULL,
			rows_affected INTEGER,
			started_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			completed_at TIMESTAMP,
			status TEXT,
			error TEXT
		)
	`

	if _, err := m.db.ExecContext(ctx, tierTransitionsQuery); err != nil {
		return fmt.Errorf("failed to create tier_transitions table: %w", err)
	}

	// Archival jobs table
	archivalJobsQuery := `
		CREATE TABLE IF NOT EXISTS archival_jobs (
			id TEXT PRIMARY KEY,
			category TEXT NOT NULL,
			source_tier TEXT,
			target_tier TEXT,
			status TEXT,
			started_at TIMESTAMP,
			completed_at TIMESTAMP,
			row_count INTEGER,
			bytes_transferred INTEGER,
			error TEXT,
			checksum TEXT,
			retry_count INTEGER DEFAULT 0
		)
	`

	if _, err := m.db.ExecContext(ctx, archivalJobsQuery); err != nil {
		return fmt.Errorf("failed to create archival_jobs table: %w", err)
	}

	// Cleanup jobs table
	cleanupJobsQuery := `
		CREATE TABLE IF NOT EXISTS cleanup_jobs (
			id TEXT PRIMARY KEY,
			category TEXT NOT NULL,
			job_type TEXT,
			started_at TIMESTAMP,
			completed_at TIMESTAMP,
			rows_processed INTEGER,
			rows_deleted INTEGER,
			status TEXT,
			error TEXT
		)
	`

	if _, err := m.db.ExecContext(ctx, cleanupJobsQuery); err != nil {
		return fmt.Errorf("failed to create cleanup_jobs table: %w", err)
	}

	// Indices for performance
	indexQueries := []string{
		"CREATE INDEX IF NOT EXISTS idx_analytics_events_category_tier ON analytics_events(category, tier)",
		"CREATE INDEX IF NOT EXISTS idx_analytics_events_created_at ON analytics_events(created_at DESC)",
		"CREATE INDEX IF NOT EXISTS idx_tier_transitions_category ON tier_transitions(category)",
		"CREATE INDEX IF NOT EXISTS idx_archival_jobs_category ON archival_jobs(category)",
		"CREATE INDEX IF NOT EXISTS idx_cleanup_jobs_category ON cleanup_jobs(category)",
	}

	for _, indexQuery := range indexQueries {
		if _, err := m.db.ExecContext(ctx, indexQuery); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}

// StartScheduler starts the job scheduler.
func (m *Manager) StartScheduler(ctx context.Context) error {
	m.mu.RLock()
	if !m.initialized {
		m.mu.RUnlock()
		return fmt.Errorf("manager not initialized")
	}
	scheduler := m.scheduler
	m.mu.RUnlock()

	if err := scheduler.Start(ctx); err != nil {
		return fmt.Errorf("failed to start scheduler: %w", err)
	}

	return nil
}

// StopScheduler stops the job scheduler.
func (m *Manager) StopScheduler() error {
	m.mu.RLock()
	if !m.initialized {
		m.mu.RUnlock()
		return fmt.Errorf("manager not initialized")
	}
	scheduler := m.scheduler
	m.mu.RUnlock()

	return scheduler.Stop()
}

// ArchiveCategory manually triggers archival for a category.
func (m *Manager) ArchiveCategory(ctx context.Context, category string) (*ArchivalJob, error) {
	m.mu.RLock()
	if !m.initialized {
		m.mu.RUnlock()
		return nil, fmt.Errorf("manager not initialized")
	}
	executor := m.executor
	m.mu.RUnlock()

	job := &ArchivalJob{
		ID:         fmt.Sprintf("manual_arch_%s_%d", category, time.Now().UnixNano()),
		Category:   category,
		SourceTier: TierHot,
		TargetTier: TierWarm,
	}

	if err := executor.ArchiveData(ctx, job); err != nil {
		return job, fmt.Errorf("archival failed: %w", err)
	}

	return job, nil
}

// TransitionCategory manually triggers tier transitions for a category.
func (m *Manager) TransitionCategory(ctx context.Context, category string) (*TierTransition, error) {
	m.mu.RLock()
	if !m.initialized {
		m.mu.RUnlock()
		return nil, fmt.Errorf("manager not initialized")
	}
	tieringEngine := m.tieringEngine
	m.mu.RUnlock()

	return tieringEngine.TransitionStaleData(ctx, category)
}

// CleanupCategory manually triggers cleanup for a category.
func (m *Manager) CleanupCategory(ctx context.Context, category string) (*CleanupJob, error) {
	m.mu.RLock()
	if !m.initialized {
		m.mu.RUnlock()
		return nil, fmt.Errorf("manager not initialized")
	}
	cleanupManager := m.cleanupManager
	m.mu.RUnlock()

	return cleanupManager.CleanupExpiredData(ctx, category)
}

// GetPolicy returns the current retention policy.
func (m *Manager) GetPolicy() *RetentionPolicy {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.policy
}

// UpdatePolicy updates the retention policy.
func (m *Manager) UpdatePolicy(newPolicy *RetentionPolicy) error {
	if err := newPolicy.Validate(); err != nil {
		return fmt.Errorf("policy validation failed: %w", err)
	}

	m.mu.Lock()
	m.policy = newPolicy
	m.mu.Unlock()

	return nil
}

// GetTierMetrics returns metrics for all tiers in a category.
func (m *Manager) GetTierMetrics(ctx context.Context, category string) ([]TierMetrics, error) {
	m.mu.RLock()
	if !m.initialized {
		m.mu.RUnlock()
		return nil, fmt.Errorf("manager not initialized")
	}
	tieringEngine := m.tieringEngine
	m.mu.RUnlock()

	return tieringEngine.GetMetricsForCategory(ctx, category)
}

// GetCleanupStats returns cleanup statistics for a category.
func (m *Manager) GetCleanupStats(ctx context.Context, category string) (*CleanupStats, error) {
	m.mu.RLock()
	if !m.initialized {
		m.mu.RUnlock()
		return nil, fmt.Errorf("manager not initialized")
	}
	cleanupManager := m.cleanupManager
	m.mu.RUnlock()

	return cleanupManager.GetCleanupStats(ctx, category)
}

// GetAuditLogs returns audit log entries.
func (m *Manager) GetAuditLogs(ctx context.Context, component string, limit int) ([]AuditLogEntry, error) {
	m.mu.RLock()
	if !m.initialized {
		m.mu.RUnlock()
		return nil, fmt.Errorf("manager not initialized")
	}
	auditLog := m.auditLog
	m.mu.RUnlock()

	return auditLog.GetLogs(ctx, component, limit)
}

// Status returns the current status of the manager.
type Status struct {
	Initialized      bool       `json:"initialized"`
	SchedulerRunning bool       `json:"scheduler_running"`
	Policy           string     `json:"policy"`
	LastArchivalTime *time.Time `json:"last_archival_time,omitempty"`
	LastCleanupTime  *time.Time `json:"last_cleanup_time,omitempty"`
	LastTieringTime  *time.Time `json:"last_tiering_time,omitempty"`
}

// GetStatus returns the current manager status.
func (m *Manager) GetStatus() *Status {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status := &Status{
		Initialized: m.initialized,
		Policy:      m.policy.DefaultPolicy,
	}

	if m.scheduler != nil {
		status.SchedulerRunning = m.scheduler.IsRunning()
		status.LastArchivalTime = m.scheduler.GetLastRunTime("archival")
		status.LastCleanupTime = m.scheduler.GetLastRunTime("cleanup")
		status.LastTieringTime = m.scheduler.GetLastRunTime("tiering")
	}

	return status
}

// Close shuts down the retention manager.
func (m *Manager) Close(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.scheduler != nil && m.scheduler.IsRunning() {
		if err := m.scheduler.Stop(); err != nil {
			return fmt.Errorf("failed to stop scheduler: %w", err)
		}
	}

	if m.auditLog != nil {
		if err := m.auditLog.LogMessage(ctx, "Manager", "Retention manager shutting down"); err != nil {
			// Log error but don't fail shutdown
		}
	}

	m.initialized = false
	return nil
}
