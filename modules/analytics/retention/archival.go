package retention

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// ArchivalJob represents a data archival operation.
type ArchivalJob struct {
	ID            string    `json:"id"`
	Category      string    `json:"category"`
	SourceTier    TierType  `json:"source_tier"`
	TargetTier    TierType  `json:"target_tier"`
	Status        string    `json:"status"` // pending, in_progress, completed, failed
	StartedAt     time.Time `json:"started_at,omitempty"`
	CompletedAt   time.Time `json:"completed_at,omitempty"`
	RowCount      int64     `json:"row_count"`
	BytesTransferred int64  `json:"bytes_transferred"`
	Error         string    `json:"error,omitempty"`
	Checksum      string    `json:"checksum,omitempty"`
	RetryCount    int       `json:"retry_count"`
}

// ArchivalExecutor handles moving data between tiers.
type ArchivalExecutor struct {
	db            *sql.DB
	policy        *RetentionPolicy
	storageBackends map[TierType]StorageBackendWriter
	auditLog      AuditLog
	maxBatchSize  int
}

// StorageBackendWriter defines the interface for writing to storage backends.
type StorageBackendWriter interface {
	Write(ctx context.Context, namespace string, data []byte) (path string, err error)
	Read(ctx context.Context, path string) ([]byte, error)
	Delete(ctx context.Context, path string) error
}

// NewArchivalExecutor creates a new archival executor.
func NewArchivalExecutor(db *sql.DB, policy *RetentionPolicy, auditLog AuditLog) *ArchivalExecutor {
	return &ArchivalExecutor{
		db:              db,
		policy:          policy,
		storageBackends: make(map[TierType]StorageBackendWriter),
		auditLog:        auditLog,
		maxBatchSize:    1000,
	}
}

// RegisterStorageBackend registers a storage backend for a tier.
func (ae *ArchivalExecutor) RegisterStorageBackend(tier TierType, backend StorageBackendWriter) {
	ae.storageBackends[tier] = backend
}

// ArchiveData moves data from one tier to another based on retention policy.
func (ae *ArchivalExecutor) ArchiveData(ctx context.Context, job *ArchivalJob) error {
	job.Status = "in_progress"
	job.StartedAt = time.Now()

	// Scan source tier for data past TTL
	rows, err := ae.identifyRowsForArchival(ctx, job.Category, job.SourceTier)
	if err != nil {
		job.Status = "failed"
		job.Error = err.Error()
		job.CompletedAt = time.Now()
		return fmt.Errorf("failed to identify rows for archival: %w", err)
	}

	if len(rows) == 0 {
		job.Status = "completed"
		job.RowCount = 0
		job.CompletedAt = time.Now()
		ae.auditLog.LogArchival(ctx, job)
		return nil
	}

	// Process in batches
	totalChecksum := sha256.New()
	totalBytes := int64(0)

	for i := 0; i < len(rows); i += ae.maxBatchSize {
		end := i + ae.maxBatchSize
		if end > len(rows) {
			end = len(rows)
		}

		batch := rows[i:end]
		if err := ae.processBatch(ctx, job, batch, totalChecksum, &totalBytes); err != nil {
			job.Status = "failed"
			job.Error = fmt.Sprintf("batch processing failed at offset %d: %v", i, err)
			job.CompletedAt = time.Now()
			ae.auditLog.LogArchival(ctx, job)
			return fmt.Errorf("batch processing failed: %w", err)
		}
	}

	// After successful archival, delete from source if we're confident data is in destination
	if err := ae.verifyAndDelete(ctx, job, rows); err != nil {
		job.Status = "failed"
		job.Error = fmt.Sprintf("verification/deletion failed: %v", err)
		job.CompletedAt = time.Now()
		ae.auditLog.LogArchival(ctx, job)
		return fmt.Errorf("verification failed: %w", err)
	}

	job.Status = "completed"
	job.RowCount = int64(len(rows))
	job.BytesTransferred = totalBytes
	job.Checksum = hex.EncodeToString(totalChecksum.Sum(nil))
	job.CompletedAt = time.Now()

	ae.auditLog.LogArchival(ctx, job)
	return nil
}

// identifyRowsForArchival finds all rows that should be archived.
func (ae *ArchivalExecutor) identifyRowsForArchival(ctx context.Context, category string, tier TierType) ([]map[string]interface{}, error) {
	sourceConfig := ae.policy.GetTierConfig(tier)
	if sourceConfig == nil {
		return nil, fmt.Errorf("no config for tier %s", tier)
	}

	nextTier := ae.policy.NextTier(tier)
	nextConfig := ae.policy.GetTierConfig(nextTier)

	if !sourceConfig.Enabled || !nextConfig.Enabled {
		return nil, fmt.Errorf("source or target tier not enabled")
	}

	// Query for rows past this tier's TTL
	query := `
		SELECT * FROM analytics_events 
		WHERE category = ? 
		AND created_at < datetime('now', '-' || ? || ' days')
		AND tier = ?
		ORDER BY created_at ASC
	`

	rows, err := ae.db.QueryContext(ctx, query, category, sourceConfig.TTLDays, tier)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	var results []map[string]interface{}

	cols, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	for rows.Next() {
		values := make([]interface{}, len(cols))
		valuePtrs := make([]interface{}, len(cols))

		for i := range cols {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		rowMap := make(map[string]interface{})
		for i, col := range cols {
			rowMap[col] = values[i]
		}

		results = append(results, rowMap)
	}

	return results, rows.Err()
}

// processBatch converts and transfers a batch of rows.
func (ae *ArchivalExecutor) processBatch(ctx context.Context, job *ArchivalJob, batch []map[string]interface{}, checksumWriter io.Writer, bytesTransferred *int64) error {
	targetBackend, exists := ae.storageBackends[job.TargetTier]
	if !exists {
		return fmt.Errorf("no storage backend registered for tier %s", job.TargetTier)
	}

	// Convert batch to Parquet or other format (simplified: JSON for now)
	data, err := json.Marshal(batch)
	if err != nil {
		return fmt.Errorf("failed to marshal batch: %w", err)
	}

	// Write to target storage
	namespace := fmt.Sprintf("analytics/%s/%s", job.Category, job.TargetTier)
	path, err := targetBackend.Write(ctx, namespace, data)
	if err != nil {
		return fmt.Errorf("failed to write to target storage: %w", err)
	}

	// Update checksum
	if _, err := checksumWriter.Write(data); err != nil {
		return fmt.Errorf("failed to update checksum: %w", err)
	}

	*bytesTransferred += int64(len(data))

	// Mark rows as having been written to target tier
	ids := make([]string, 0, len(batch))
	for _, row := range batch {
		if id, ok := row["id"].(string); ok {
			ids = append(ids, id)
		}
	}

	if len(ids) > 0 {
		updateQuery := fmt.Sprintf(`
			UPDATE analytics_events 
			SET tier = ?, archived_at = NOW(), archive_path = ?
			WHERE id IN (%s)
		`, ae.placeholders(len(ids)))

		args := make([]interface{}, 0, len(ids)+2)
		args = append(args, job.TargetTier, path)
		for _, id := range ids {
			args = append(args, id)
		}

		if _, err := ae.db.ExecContext(ctx, updateQuery, args...); err != nil {
			return fmt.Errorf("failed to update event tier: %w", err)
		}
	}

	return nil
}

// verifyAndDelete checks data integrity before deletion from source.
func (ae *ArchivalExecutor) verifyAndDelete(ctx context.Context, job *ArchivalJob, rows []map[string]interface{}) error {
	if len(rows) == 0 {
		return nil
	}

	// Verify target storage has the data
	_, exists := ae.storageBackends[job.TargetTier]
	if !exists {
		return fmt.Errorf("target backend not registered")
	}

	// Query to verify all rows have been archived
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if id, ok := row["id"].(string); ok {
			ids = append(ids, id)
		}
	}

	if len(ids) == 0 {
		return nil
	}

	// Count rows that have been archived
	verifyQuery := fmt.Sprintf(`
		SELECT COUNT(*) FROM analytics_events 
		WHERE id IN (%s) AND tier = ? AND archive_path IS NOT NULL
	`, ae.placeholders(len(ids)))

	args := make([]interface{}, 0, len(ids)+1)
	args = append(args, job.TargetTier)
	for _, id := range ids {
		args = append(args, id)
	}

	var count int
	if err := ae.db.QueryRowContext(ctx, verifyQuery, args...).Scan(&count); err != nil {
		return fmt.Errorf("verification query failed: %w", err)
	}

	if count != len(ids) {
		return fmt.Errorf("verification failed: only %d/%d rows archived", count, len(ids))
	}

	// Delete from source tier (mark as deleted with soft delete)
	deleteQuery := fmt.Sprintf(`
		UPDATE analytics_events 
		SET deleted_at = NOW()
		WHERE id IN (%s) AND tier = ?
	`, ae.placeholders(len(ids)))

	args = make([]interface{}, 0, len(ids)+1)
	args = append(args, job.SourceTier)
	for _, id := range ids {
		args = append(args, id)
	}

	if _, err := ae.db.ExecContext(ctx, deleteQuery, args...); err != nil {
		return fmt.Errorf("failed to delete from source tier: %w", err)
	}

	return nil
}

// placeholders generates comma-separated ? placeholders for SQL queries.
func (ae *ArchivalExecutor) placeholders(count int) string {
	result := ""
	for i := 0; i < count; i++ {
		if i > 0 {
			result += ","
		}
		result += "?"
	}
	return result
}

// RetryFailedArchival retries a failed archival job.
func (ae *ArchivalExecutor) RetryFailedArchival(ctx context.Context, job *ArchivalJob) error {
	if job.RetryCount >= 3 {
		return fmt.Errorf("max retries (%d) exceeded", job.RetryCount)
	}

	job.RetryCount++
	job.Status = "pending"
	job.Error = ""

	// Exponential backoff for retries
	backoff := time.Duration(1<<uint(job.RetryCount-1)) * time.Second
	select {
	case <-time.After(backoff):
	case <-ctx.Done():
		return ctx.Err()
	}

	return ae.ArchiveData(ctx, job)
}
