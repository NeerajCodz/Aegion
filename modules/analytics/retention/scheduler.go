package retention

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ScheduleConfig defines scheduling configuration.
type ScheduleConfig struct {
	// ArchivalInterval is how often to run archival jobs
	ArchivalInterval time.Duration `yaml:"archival_interval"`

	// CleanupInterval is how often to run cleanup jobs
	CleanupInterval time.Duration `yaml:"cleanup_interval"`

	// TieringInterval is how often to run tiering transitions
	TieringInterval time.Duration `yaml:"tiering_interval"`

	// StartTime is when to start scheduled jobs (e.g., "02:00" for 2 AM)
	StartTime string `yaml:"start_time"`

	// Categories to process (empty means all)
	Categories []string `yaml:"categories"`
}

// JobScheduler orchestrates scheduled retention jobs.
type JobScheduler struct {
	config         *ScheduleConfig
	executor       *ArchivalExecutor
	tieringEngine  *TieringEngine
	cleanupManager *CleanupManager
	auditLog       AuditLog
	mu             sync.RWMutex
	running        bool
	cancel         context.CancelFunc
	jobHistory     map[string][]interface{}
	lastRunTimes   map[string]time.Time
}

// NewJobScheduler creates a new job scheduler.
func NewJobScheduler(
	config *ScheduleConfig,
	executor *ArchivalExecutor,
	tieringEngine *TieringEngine,
	cleanupManager *CleanupManager,
	auditLog AuditLog,
) *JobScheduler {
	return &JobScheduler{
		config:         config,
		executor:       executor,
		tieringEngine:  tieringEngine,
		cleanupManager: cleanupManager,
		auditLog:       auditLog,
		jobHistory:     make(map[string][]interface{}),
		lastRunTimes:   make(map[string]time.Time),
	}
}

// Start begins the scheduled job runner.
func (js *JobScheduler) Start(ctx context.Context) error {
	js.mu.Lock()
	if js.running {
		js.mu.Unlock()
		return fmt.Errorf("scheduler already running")
	}
	js.running = true
	js.mu.Unlock()

	ctx, cancel := context.WithCancel(ctx)
	js.cancel = cancel

	go js.runScheduler(ctx)

	_ = js.auditLog.LogMessage(ctx, "JobScheduler", "Retention job scheduler started")

	return nil
}

// Stop stops the scheduler.
func (js *JobScheduler) Stop() error {
	js.mu.Lock()
	if !js.running {
		js.mu.Unlock()
		return fmt.Errorf("scheduler not running")
	}
	js.running = false
	js.mu.Unlock()

	if js.cancel != nil {
		js.cancel()
	}

	return nil
}

// runScheduler is the main scheduler loop.
func (js *JobScheduler) runScheduler(ctx context.Context) {
	archivalTicker := time.NewTicker(js.config.ArchivalInterval)
	tieringTicker := time.NewTicker(js.config.TieringInterval)
	cleanupTicker := time.NewTicker(js.config.CleanupInterval)

	defer archivalTicker.Stop()
	defer tieringTicker.Stop()
	defer cleanupTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-archivalTicker.C:
			js.runArchivalJobs(ctx)

		case <-tieringTicker.C:
			js.runTieringJobs(ctx)

		case <-cleanupTicker.C:
			js.runCleanupJobs(ctx)
		}
	}
}

// runArchivalJobs runs archival jobs for all configured categories.
func (js *JobScheduler) runArchivalJobs(ctx context.Context) {
	categories := js.config.Categories
	if len(categories) == 0 {
		// If no categories specified, would need to query all categories
		// For now, use a default set
		categories = []string{"authentication", "audit_events", "sessions"}
	}

	for _, category := range categories {
		job := &ArchivalJob{
			ID:         fmt.Sprintf("arch_%s_%d", category, time.Now().UnixNano()),
			Category:   category,
			SourceTier: TierHot,
			TargetTier: TierWarm,
		}

		if err := js.executor.ArchiveData(ctx, job); err != nil {
			_ = js.auditLog.LogMessage(ctx, "JobScheduler",
				fmt.Sprintf("Archival failed for category %s: %v", category, err))
		} else {
			js.recordJobHistory("archival", job)
		}

		// Then archive warm to cold
		job2 := &ArchivalJob{
			ID:         fmt.Sprintf("arch_%s_c_%d", category, time.Now().UnixNano()),
			Category:   category,
			SourceTier: TierWarm,
			TargetTier: TierCold,
		}

		if err := js.executor.ArchiveData(ctx, job2); err != nil {
			_ = js.auditLog.LogMessage(ctx, "JobScheduler",
				fmt.Sprintf("Warm->Cold archival failed for category %s: %v", category, err))
		} else {
			js.recordJobHistory("archival", job2)
		}
	}

	js.lastRunTimes["archival"] = time.Now()
}

// runTieringJobs runs tier transition jobs.
func (js *JobScheduler) runTieringJobs(ctx context.Context) {
	categories := js.config.Categories
	if len(categories) == 0 {
		categories = []string{"authentication", "audit_events", "sessions"}
	}

	for _, category := range categories {
		transition, err := js.tieringEngine.TransitionStaleData(ctx, category)
		if err != nil {
			_ = js.auditLog.LogMessage(ctx, "JobScheduler",
				fmt.Sprintf("Tiering failed for category %s: %v", category, err))
		} else {
			js.recordJobHistory("tiering", transition)
		}
	}

	js.lastRunTimes["tiering"] = time.Now()
}

// runCleanupJobs runs cleanup and garbage collection.
func (js *JobScheduler) runCleanupJobs(ctx context.Context) {
	categories := js.config.Categories
	if len(categories) == 0 {
		categories = []string{"authentication", "audit_events", "sessions"}
	}

	for _, category := range categories {
		// Cleanup expired data
		job, err := js.cleanupManager.CleanupExpiredData(ctx, category)
		if err != nil {
			js.auditLog.LogMessage(ctx, "JobScheduler",
				fmt.Sprintf("Cleanup expired data failed for category %s: %v", category, err))
		} else {
			js.recordJobHistory("cleanup_expired", job)
		}

		// Cleanup soft-deleted data (older than 30 days)
		softDeleteJob, err := js.cleanupManager.CleanupSoftDeletedData(ctx, category, 30)
		if err != nil {
			js.auditLog.LogMessage(ctx, "JobScheduler",
				fmt.Sprintf("Cleanup soft-deleted failed for category %s: %v", category, err))
		} else {
			js.recordJobHistory("cleanup_soft", softDeleteJob)
		}

		// Find and report orphans
		orphans, err := js.cleanupManager.FindOrphanRecords(ctx, category)
		if err != nil {
			js.auditLog.LogMessage(ctx, "JobScheduler",
				fmt.Sprintf("Orphan detection failed for category %s: %v", category, err))
		} else if len(orphans) > 0 {
			js.auditLog.LogMessage(ctx, "JobScheduler",
				fmt.Sprintf("Found %d orphan records in category %s", len(orphans), category))
			// Attempt repair
			if err := js.cleanupManager.RepairOrphanRecords(ctx, orphans); err != nil {
				js.auditLog.LogMessage(ctx, "JobScheduler",
					fmt.Sprintf("Orphan repair failed for category %s: %v", category, err))
			}
		}
	}

	js.lastRunTimes["cleanup"] = time.Now()
}

// recordJobHistory records a job in history.
func (js *JobScheduler) recordJobHistory(jobType string, job interface{}) {
	js.mu.Lock()
	defer js.mu.Unlock()

	if _, exists := js.jobHistory[jobType]; !exists {
		js.jobHistory[jobType] = []interface{}{}
	}

	// Keep only last 100 jobs per type
	history := js.jobHistory[jobType]
	if len(history) >= 100 {
		history = history[1:]
	}

	history = append(history, job)
	js.jobHistory[jobType] = history
}

// GetJobHistory returns recorded job history.
func (js *JobScheduler) GetJobHistory(jobType string, limit int) []interface{} {
	js.mu.RLock()
	defer js.mu.RUnlock()

	history := js.jobHistory[jobType]
	if len(history) <= limit {
		return history
	}

	return history[len(history)-limit:]
}

// GetLastRunTime returns the last time a job type ran.
func (js *JobScheduler) GetLastRunTime(jobType string) *time.Time {
	js.mu.RLock()
	defer js.mu.RUnlock()

	if t, exists := js.lastRunTimes[jobType]; exists {
		return &t
	}

	return nil
}

// IsRunning returns whether the scheduler is currently running.
func (js *JobScheduler) IsRunning() bool {
	js.mu.RLock()
	defer js.mu.RUnlock()
	return js.running
}

// DefaultScheduleConfig returns sensible defaults for scheduling.
func DefaultScheduleConfig() *ScheduleConfig {
	return &ScheduleConfig{
		ArchivalInterval: 24 * time.Hour,     // Daily
		CleanupInterval:  7 * 24 * time.Hour, // Weekly
		TieringInterval:  6 * time.Hour,      // Every 6 hours
		StartTime:        "02:00",            // 2 AM
		Categories:       []string{},         // All categories
	}
}
