package retention

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// AuditLogEntry represents a single audit log entry.
type AuditLogEntry struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Component string    `json:"component"`
	Operation string    `json:"operation"`
	Details   string    `json:"details"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// AuditLog interface defines operations for audit logging.
type AuditLog interface {
	LogArchival(ctx context.Context, job *ArchivalJob) error
	LogTierTransition(ctx context.Context, transition *TierTransition) error
	LogCleanup(ctx context.Context, job *CleanupJob) error
	LogMessage(ctx context.Context, component, message string) error
	GetLogs(ctx context.Context, component string, limit int) ([]AuditLogEntry, error)
}

// DatabaseAuditLog implements AuditLog using a database backend.
type DatabaseAuditLog struct {
	db *sql.DB
}

// NewDatabaseAuditLog creates a new database-backed audit log.
func NewDatabaseAuditLog(db *sql.DB) *DatabaseAuditLog {
	return &DatabaseAuditLog{db: db}
}

// Initialize creates the audit log table if it doesn't exist.
func (dal *DatabaseAuditLog) Initialize(ctx context.Context) error {
	query := `
		CREATE TABLE IF NOT EXISTS retention_audit_log (
			id TEXT PRIMARY KEY,
			timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			component TEXT NOT NULL,
			operation TEXT NOT NULL,
			details TEXT,
			metadata TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`

	_, err := dal.db.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to create audit log table: %w", err)
	}

	// Create index for faster queries
	indexQuery := `CREATE INDEX IF NOT EXISTS idx_audit_log_component_timestamp 
		ON retention_audit_log(component, timestamp DESC)`
	_, _ = dal.db.ExecContext(ctx, indexQuery)

	return nil
}

// LogArchival logs an archival job.
func (dal *DatabaseAuditLog) LogArchival(ctx context.Context, job *ArchivalJob) error {
	metadata := map[string]interface{}{
		"job_id":            job.ID,
		"category":          job.Category,
		"source_tier":       job.SourceTier,
		"target_tier":       job.TargetTier,
		"row_count":         job.RowCount,
		"bytes_transferred": job.BytesTransferred,
		"checksum":          job.Checksum,
		"retry_count":       job.RetryCount,
	}

	metaJSON, _ := json.Marshal(metadata)

	return dal.logEntry(ctx, AuditLogEntry{
		ID:        fmt.Sprintf("audit_arch_%d", time.Now().UnixNano()),
		Timestamp: time.Now(),
		Component: "ArchivalExecutor",
		Operation: "Archive",
		Details:   fmt.Sprintf("Archived %d rows from %s to %s (status: %s, error: %s)",
			job.RowCount, job.SourceTier, job.TargetTier, job.Status, job.Error),
		Metadata: metadata,
	}, string(metaJSON))
}

// LogTierTransition logs a tier transition.
func (dal *DatabaseAuditLog) LogTierTransition(ctx context.Context, transition *TierTransition) error {
	metadata := map[string]interface{}{
		"transition_id": transition.ID,
		"category":      transition.Category,
		"from_tier":     transition.FromTier,
		"to_tier":       transition.ToTier,
		"rows_affected": transition.RowsAffected,
	}

	metaJSON, _ := json.Marshal(metadata)

	return dal.logEntry(ctx, AuditLogEntry{
		ID:        fmt.Sprintf("audit_tier_%d", time.Now().UnixNano()),
		Timestamp: time.Now(),
		Component: "TieringEngine",
		Operation: "TierTransition",
		Details:   fmt.Sprintf("Transitioned %d rows from %s to %s for category %s (status: %s, error: %s)",
			transition.RowsAffected, transition.FromTier, transition.ToTier, transition.Category, transition.Status, transition.Error),
		Metadata: metadata,
	}, string(metaJSON))
}

// LogCleanup logs a cleanup job.
func (dal *DatabaseAuditLog) LogCleanup(ctx context.Context, job *CleanupJob) error {
	metadata := map[string]interface{}{
		"job_id":        job.ID,
		"job_type":      job.JobType,
		"category":      job.Category,
		"rows_processed": job.RowsProcessed,
		"rows_deleted":   job.RowsDeleted,
	}

	metaJSON, _ := json.Marshal(metadata)

	return dal.logEntry(ctx, AuditLogEntry{
		ID:        fmt.Sprintf("audit_clean_%d", time.Now().UnixNano()),
		Timestamp: time.Now(),
		Component: "CleanupManager",
		Operation: fmt.Sprintf("Cleanup-%s", job.JobType),
		Details:   fmt.Sprintf("Cleaned up %d rows (deleted %d) for category %s (status: %s, error: %s)",
			job.RowsProcessed, job.RowsDeleted, job.Category, job.Status, job.Error),
		Metadata: metadata,
	}, string(metaJSON))
}

// LogMessage logs a general message.
func (dal *DatabaseAuditLog) LogMessage(ctx context.Context, component, message string) error {
	return dal.logEntry(ctx, AuditLogEntry{
		ID:        fmt.Sprintf("audit_msg_%d", time.Now().UnixNano()),
		Timestamp: time.Now(),
		Component: component,
		Operation: "Message",
		Details:   message,
	}, "")
}

// logEntry inserts an entry into the audit log.
func (dal *DatabaseAuditLog) logEntry(ctx context.Context, entry AuditLogEntry, metadataJSON string) error {
	query := `
		INSERT INTO retention_audit_log (id, timestamp, component, operation, details, metadata)
		VALUES (?, ?, ?, ?, ?, ?)
	`

	_, err := dal.db.ExecContext(ctx, query, entry.ID, entry.Timestamp, entry.Component, entry.Operation, entry.Details, metadataJSON)
	if err != nil {
		return fmt.Errorf("failed to insert audit log entry: %w", err)
	}

	return nil
}

// GetLogs retrieves audit log entries.
func (dal *DatabaseAuditLog) GetLogs(ctx context.Context, component string, limit int) ([]AuditLogEntry, error) {
	query := `
		SELECT id, timestamp, component, operation, details, metadata
		FROM retention_audit_log
		WHERE component = ?
		ORDER BY timestamp DESC
		LIMIT ?
	`

	rows, err := dal.db.QueryContext(ctx, query, component, limit)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	var entries []AuditLogEntry
	for rows.Next() {
		var entry AuditLogEntry
		var metadataJSON sql.NullString

		if err := rows.Scan(&entry.ID, &entry.Timestamp, &entry.Component, &entry.Operation, &entry.Details, &metadataJSON); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		if metadataJSON.Valid {
			if err := json.Unmarshal([]byte(metadataJSON.String), &entry.Metadata); err != nil {
				entry.Metadata = make(map[string]interface{})
			}
		}

		entries = append(entries, entry)
	}

	return entries, rows.Err()
}

// GetLogsByDateRange retrieves audit logs within a date range.
func (dal *DatabaseAuditLog) GetLogsByDateRange(ctx context.Context, component string, start, end time.Time, limit int) ([]AuditLogEntry, error) {
	query := `
		SELECT id, timestamp, component, operation, details, metadata
		FROM retention_audit_log
		WHERE component = ? AND timestamp >= ? AND timestamp <= ?
		ORDER BY timestamp DESC
		LIMIT ?
	`

	rows, err := dal.db.QueryContext(ctx, query, component, start, end, limit)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	var entries []AuditLogEntry
	for rows.Next() {
		var entry AuditLogEntry
		var metadataJSON sql.NullString

		if err := rows.Scan(&entry.ID, &entry.Timestamp, &entry.Component, &entry.Operation, &entry.Details, &metadataJSON); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		if metadataJSON.Valid {
			if err := json.Unmarshal([]byte(metadataJSON.String), &entry.Metadata); err != nil {
				entry.Metadata = make(map[string]interface{})
			}
		}

		entries = append(entries, entry)
	}

	return entries, rows.Err()
}

// ArchiveAuditLogs moves old audit logs to cold storage.
func (dal *DatabaseAuditLog) ArchiveAuditLogs(ctx context.Context, daysOld int) (int64, error) {
	// For now, just delete old audit logs after a certain period (compliance might require different handling)
	query := `
		DELETE FROM retention_audit_log
		WHERE timestamp < datetime('now', '-' || ? || ' days')
	`

	result, err := dal.db.ExecContext(ctx, query, daysOld)
	if err != nil {
		return 0, fmt.Errorf("failed to archive audit logs: %w", err)
	}

	return result.RowsAffected()
}

// NoopAuditLog is a no-operation audit log for testing.
type NoopAuditLog struct{}

func (nal *NoopAuditLog) LogArchival(ctx context.Context, job *ArchivalJob) error      { return nil }
func (nal *NoopAuditLog) LogTierTransition(ctx context.Context, transition *TierTransition) error { return nil }
func (nal *NoopAuditLog) LogCleanup(ctx context.Context, job *CleanupJob) error         { return nil }
func (nal *NoopAuditLog) LogMessage(ctx context.Context, component, message string) error { return nil }
func (nal *NoopAuditLog) GetLogs(ctx context.Context, component string, limit int) ([]AuditLogEntry, error) {
	return []AuditLogEntry{}, nil
}
