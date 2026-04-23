package retention

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// CleanupJob represents a data cleanup operation.
type CleanupJob struct {
	ID              string    `json:"id"`
	Category        string    `json:"category"`
	JobType         string    `json:"job_type"` // expired_data, orphan_records, soft_deleted
	StartedAt       time.Time `json:"started_at"`
	CompletedAt     time.Time `json:"completed_at,omitempty"`
	RowsProcessed   int64     `json:"rows_processed"`
	RowsDeleted     int64     `json:"rows_deleted"`
	Status          string    `json:"status"` // pending, in_progress, completed, failed
	Error           string    `json:"error,omitempty"`
}

// CleanupManager handles garbage collection and data cleanup.
type CleanupManager struct {
	db       *sql.DB
	policy   *RetentionPolicy
	auditLog AuditLog
}

// NewCleanupManager creates a new cleanup manager.
func NewCleanupManager(db *sql.DB, policy *RetentionPolicy, auditLog AuditLog) *CleanupManager {
	return &CleanupManager{
		db:       db,
		policy:   policy,
		auditLog: auditLog,
	}
}

// CleanupExpiredData removes data that has exceeded the maximum retention period.
func (cm *CleanupManager) CleanupExpiredData(ctx context.Context, category string) (*CleanupJob, error) {
	job := &CleanupJob{
		ID:        fmt.Sprintf("cleanup_%d", time.Now().UnixNano()),
		Category:  category,
		JobType:   "expired_data",
		StartedAt: time.Now(),
		Status:    "in_progress",
	}

	coldConfig := cm.policy.GetTierConfig(TierCold)
	if coldConfig == nil || !coldConfig.Enabled {
		job.Status = "failed"
		job.Error = "cold tier not enabled"
		job.CompletedAt = time.Now()
		cm.auditLog.LogCleanup(ctx, job)
		return job, fmt.Errorf("cold tier not configured")
	}

	// Find expired records in cold storage
	countQuery := `
		SELECT COUNT(*) FROM analytics_events 
		WHERE category = ? 
		AND tier = ?
		AND deleted_at IS NULL
		AND created_at < datetime('now', '-' || ? || ' days')
	`

	var expiredCount int64
	if err := cm.db.QueryRowContext(ctx, countQuery, category, TierCold, coldConfig.TTLDays).Scan(&expiredCount); err != nil {
		job.Status = "failed"
		job.Error = fmt.Sprintf("count query failed: %v", err)
		job.CompletedAt = time.Now()
		cm.auditLog.LogCleanup(ctx, job)
		return job, err
	}

	if expiredCount == 0 {
		job.Status = "completed"
		job.RowsProcessed = 0
		job.RowsDeleted = 0
		job.CompletedAt = time.Now()
		cm.auditLog.LogCleanup(ctx, job)
		return job, nil
	}

	job.RowsProcessed = expiredCount

	// Hard delete expired records (in batches to avoid locking)
	batchSize := int64(5000)
	totalDeleted := int64(0)

	for offset := int64(0); offset < expiredCount; offset += batchSize {
		deleteQuery := `
			DELETE FROM analytics_events 
			WHERE id IN (
				SELECT id FROM analytics_events 
				WHERE category = ? 
				AND tier = ?
				AND deleted_at IS NULL
				AND created_at < datetime('now', '-' || ? || ' days')
				LIMIT ?
			)
		`

		result, err := cm.db.ExecContext(ctx, deleteQuery, category, TierCold, coldConfig.TTLDays, batchSize)
		if err != nil {
			job.Status = "failed"
			job.Error = fmt.Sprintf("delete failed at batch offset %d: %v", offset, err)
			job.CompletedAt = time.Now()
			cm.auditLog.LogCleanup(ctx, job)
			return job, err
		}

		deleted, err := result.RowsAffected()
		if err != nil {
			return job, err
		}

		totalDeleted += deleted

		// Give the database a moment to recover between batches
		select {
		case <-time.After(100 * time.Millisecond):
		case <-ctx.Done():
			job.Status = "failed"
			job.Error = "context cancelled"
			job.CompletedAt = time.Now()
			cm.auditLog.LogCleanup(ctx, job)
			return job, ctx.Err()
		}
	}

	job.RowsDeleted = totalDeleted
	job.Status = "completed"
	job.CompletedAt = time.Now()
	cm.auditLog.LogCleanup(ctx, job)

	return job, nil
}

// CleanupSoftDeletedData removes records marked as soft-deleted.
func (cm *CleanupManager) CleanupSoftDeletedData(ctx context.Context, category string, daysOld int) (*CleanupJob, error) {
	job := &CleanupJob{
		ID:        fmt.Sprintf("cleanup_soft_%d", time.Now().UnixNano()),
		Category:  category,
		JobType:   "soft_deleted",
		StartedAt: time.Now(),
		Status:    "in_progress",
	}

	// Count soft-deleted records to remove
	countQuery := `
		SELECT COUNT(*) FROM analytics_events 
		WHERE category = ? 
		AND deleted_at IS NOT NULL
		AND deleted_at < datetime('now', '-' || ? || ' days')
	`

	var count int64
	if err := cm.db.QueryRowContext(ctx, countQuery, category, daysOld).Scan(&count); err != nil {
		job.Status = "failed"
		job.Error = fmt.Sprintf("count query failed: %v", err)
		job.CompletedAt = time.Now()
		cm.auditLog.LogCleanup(ctx, job)
		return job, err
	}

	if count == 0 {
		job.Status = "completed"
		job.RowsProcessed = 0
		job.RowsDeleted = 0
		job.CompletedAt = time.Now()
		cm.auditLog.LogCleanup(ctx, job)
		return job, nil
	}

	job.RowsProcessed = count

	// Delete soft-deleted records
	deleteQuery := `
		DELETE FROM analytics_events 
		WHERE category = ? 
		AND deleted_at IS NOT NULL
		AND deleted_at < datetime('now', '-' || ? || ' days')
	`

	result, err := cm.db.ExecContext(ctx, deleteQuery, category, daysOld)
	if err != nil {
		job.Status = "failed"
		job.Error = fmt.Sprintf("delete failed: %v", err)
		job.CompletedAt = time.Now()
		cm.auditLog.LogCleanup(ctx, job)
		return job, err
	}

	deleted, err := result.RowsAffected()
	if err != nil {
		return job, err
	}

	job.RowsDeleted = deleted
	job.Status = "completed"
	job.CompletedAt = time.Now()
	cm.auditLog.LogCleanup(ctx, job)

	return job, nil
}

// FindOrphanRecords identifies records with no data in secondary storage.
type OrphanRecord struct {
	ID            string    `json:"id"`
	Category      string    `json:"category"`
	Tier          TierType  `json:"tier"`
	ArchivePath   *string   `json:"archive_path"`
	CreatedAt     time.Time `json:"created_at"`
	ArchivedAt    *time.Time `json:"archived_at"`
	VerifiedAt    *time.Time `json:"verified_at"`
}

// FindOrphanRecords finds records with missing archive data.
func (cm *CleanupManager) FindOrphanRecords(ctx context.Context, category string) ([]OrphanRecord, error) {
	query := `
		SELECT id, category, tier, archive_path, created_at, archived_at
		FROM analytics_events 
		WHERE category = ? 
		AND tier IN (?, ?)
		AND archived_at IS NOT NULL
		AND deleted_at IS NULL
	`

	rows, err := cm.db.QueryContext(ctx, query, category, TierWarm, TierCold)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	var orphans []OrphanRecord
	for rows.Next() {
		var record OrphanRecord
		if err := rows.Scan(&record.ID, &record.Category, &record.Tier, &record.ArchivePath,
			&record.CreatedAt, &record.ArchivedAt); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// In a real implementation, we would check if the data exists in the archive storage
		// For now, we'll flag records with no archive path as orphans
		if record.ArchivePath == nil {
			orphans = append(orphans, record)
		}
	}

	return orphans, rows.Err()
}

// RepairOrphanRecords attempts to fix orphan records by re-archiving them.
func (cm *CleanupManager) RepairOrphanRecords(ctx context.Context, orphans []OrphanRecord) error {
	if len(orphans) == 0 {
		return nil
	}

	for _, orphan := range orphans {
		// Move back to previous tier for re-archival
		var previousTier TierType
		switch orphan.Tier {
		case TierCold:
			previousTier = TierWarm
		case TierWarm:
			previousTier = TierHot
		default:
			// Can't repair orphaned hot tier data
			continue
		}

		query := `
			UPDATE analytics_events 
			SET tier = ?, archived_at = NULL, archive_path = NULL
			WHERE id = ?
		`

		if _, err := cm.db.ExecContext(ctx, query, previousTier, orphan.ID); err != nil {
			return fmt.Errorf("failed to repair orphan record %s: %w", orphan.ID, err)
		}
	}

	return nil
}

// GetCleanupStats returns cleanup statistics for a category.
type CleanupStats struct {
	Category             string    `json:"category"`
	TotalRecords         int64     `json:"total_records"`
	SoftDeletedRecords   int64     `json:"soft_deleted_records"`
	ExpiredRecords       int64     `json:"expired_records"`
	OrphanRecords        int64     `json:"orphan_records"`
	EstimatedBytesFreed  int64     `json:"estimated_bytes_freed"`
	LastCleanupTime      *time.Time `json:"last_cleanup_time,omitempty"`
	LastCleanupDuration  *time.Duration `json:"last_cleanup_duration,omitempty"`
}

// GetCleanupStats gathers cleanup statistics for a category.
func (cm *CleanupManager) GetCleanupStats(ctx context.Context, category string) (*CleanupStats, error) {
	stats := &CleanupStats{
		Category: category,
	}

	// Total records
	if err := cm.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM analytics_events WHERE category = ?`, category).Scan(&stats.TotalRecords); err != nil {
		return nil, err
	}

	// Soft-deleted records
	if err := cm.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM analytics_events WHERE category = ? AND deleted_at IS NOT NULL`, category).Scan(&stats.SoftDeletedRecords); err != nil {
		return nil, err
	}

	coldConfig := cm.policy.GetTierConfig(TierCold)
	if coldConfig != nil {
		// Expired records
		if err := cm.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM analytics_events WHERE category = ? AND tier = ? AND created_at < datetime('now', '-' || ? || ' days')`,
			category, TierCold, coldConfig.TTLDays).Scan(&stats.ExpiredRecords); err != nil {
			return nil, err
		}
	}

	// Orphan records
	if err := cm.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM analytics_events WHERE category = ? AND tier IN (?, ?) AND archived_at IS NOT NULL AND archive_path IS NULL AND deleted_at IS NULL`,
		category, TierWarm, TierCold).Scan(&stats.OrphanRecords); err != nil {
		return nil, err
	}

	// Estimated bytes
	var estimatedSize sql.NullFloat64
	if err := cm.db.QueryRowContext(ctx,
		`SELECT SUM(CAST(json_extract(data, '$.size') as REAL)) FROM analytics_events WHERE category = ? AND (deleted_at IS NOT NULL OR (tier = ? AND created_at < datetime('now', '-' || ? || ' days')))`,
		category, TierCold, coldConfig.TTLDays).Scan(&estimatedSize); err != nil {
		if err != sql.ErrNoRows {
			return nil, err
		}
	}

	if estimatedSize.Valid {
		stats.EstimatedBytesFreed = int64(estimatedSize.Float64)
	}

	return stats, nil
}
