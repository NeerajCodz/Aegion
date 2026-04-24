package retention

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// TierTransition represents a data movement operation between tiers.
type TierTransition struct {
	ID              string    `json:"id"`
	Category        string    `json:"category"`
	FromTier        TierType  `json:"from_tier"`
	ToTier          TierType  `json:"to_tier"`
	RowsAffected    int64     `json:"rows_affected"`
	StartedAt       time.Time `json:"started_at"`
	CompletedAt     time.Time `json:"completed_at,omitempty"`
	Status          string    `json:"status"` // pending, completed, failed
	Error           string    `json:"error,omitempty"`
}

// TieringEngine manages tier transitions for data.
type TieringEngine struct {
	db             *sql.DB
	policy         *RetentionPolicy
	auditLog       AuditLog
	transitionJobs map[string]*TierTransition
}

// NewTieringEngine creates a new tiering engine.
func NewTieringEngine(db *sql.DB, policy *RetentionPolicy, auditLog AuditLog) *TieringEngine {
	return &TieringEngine{
		db:             db,
		policy:         policy,
		auditLog:       auditLog,
		transitionJobs: make(map[string]*TierTransition),
	}
}

// DetermineTier calculates which tier a record should be in.
func (te *TieringEngine) DetermineTier(ctx context.Context, category string, recordTime time.Time) (TierType, error) {
	tier := te.policy.GetTierForTimestamp(category, recordTime)
	return tier, nil
}

// TransitionStaleData moves data that has aged past a tier's TTL to the next tier.
func (te *TieringEngine) TransitionStaleData(ctx context.Context, category string) (*TierTransition, error) {
	transition := &TierTransition{
		ID:        fmt.Sprintf("trans_%d", time.Now().UnixNano()),
		Category:  category,
		StartedAt: time.Now(),
		Status:    "pending",
	}

	// Find data in hot tier that should move to warm
	if err := te.transitionTier(ctx, transition, TierHot, TierWarm); err != nil {
		transition.Status = "failed"
		transition.Error = err.Error()
		transition.CompletedAt = time.Now()
		te.auditLog.LogTierTransition(ctx, transition)
		return transition, err
	}

	// Find data in warm tier that should move to cold
	if err := te.transitionTier(ctx, transition, TierWarm, TierCold); err != nil {
		// Log but don't fail - we at least moved hot to warm
		te.auditLog.LogMessage(ctx, "TieringEngine", fmt.Sprintf("warm->cold transition failed: %v", err))
	}

	transition.Status = "completed"
	transition.CompletedAt = time.Now()
	te.auditLog.LogTierTransition(ctx, transition)

	te.transitionJobs[transition.ID] = transition
	return transition, nil
}

// transitionTier performs the actual tier transition for a category.
func (te *TieringEngine) transitionTier(ctx context.Context, transition *TierTransition, fromTier, toTier TierType) error {
	if transition.FromTier == "" {
		transition.FromTier = fromTier
		transition.ToTier = toTier
	}

	fromConfig := te.policy.GetTierConfig(fromTier)
	if fromConfig == nil || !fromConfig.Enabled {
		return fmt.Errorf("source tier %s not enabled or configured", fromTier)
	}

	// Query for rows that should move from this tier to the next
	query := `
		SELECT COUNT(*) FROM analytics_events 
		WHERE category = ? 
		AND tier = ?
		AND created_at < datetime('now', '-' || ? || ' days')
		AND deleted_at IS NULL
	`

	var count int64
	if err := te.db.QueryRowContext(ctx, query, transition.Category, fromTier, fromConfig.TTLDays).Scan(&count); err != nil {
		return fmt.Errorf("failed to count rows for transition: %w", err)
	}

	if count == 0 {
		return nil
	}

	// Update tier for matching rows
	updateQuery := `
		UPDATE analytics_events 
		SET tier = ?, tier_updated_at = CURRENT_TIMESTAMP
		WHERE category = ? 
		AND tier = ?
		AND created_at < datetime('now', '-' || ? || ' days')
		AND deleted_at IS NULL
	`

	result, err := te.db.ExecContext(ctx, updateQuery, toTier, transition.Category, fromTier, fromConfig.TTLDays)
	if err != nil {
		return fmt.Errorf("failed to update tier: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	transition.RowsAffected = rowsAffected
	return nil
}

// GetTransitionHistory returns past tier transitions.
func (te *TieringEngine) GetTransitionHistory(ctx context.Context, category string) ([]TierTransition, error) {
	query := `
		SELECT id, category, from_tier, to_tier, rows_affected, started_at, completed_at, status, error
		FROM tier_transitions
		WHERE category = ?
		ORDER BY started_at DESC
	`

	rows, err := te.db.QueryContext(ctx, query, category)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	var transitions []TierTransition
	for rows.Next() {
		var t TierTransition
		if err := rows.Scan(&t.ID, &t.Category, &t.FromTier, &t.ToTier, &t.RowsAffected,
			&t.StartedAt, &t.CompletedAt, &t.Status, &t.Error); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		transitions = append(transitions, t)
	}

	return transitions, rows.Err()
}

// GetDataDistribution returns how data is distributed across tiers.
func (te *TieringEngine) GetDataDistribution(ctx context.Context, category string) (map[TierType]int64, error) {
	distribution := make(map[TierType]int64)

	query := `
		SELECT tier, COUNT(*) as count
		FROM analytics_events
		WHERE category = ? AND deleted_at IS NULL
		GROUP BY tier
	`

	rows, err := te.db.QueryContext(ctx, query, category)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var tier TierType
		var count int64
		if err := rows.Scan(&tier, &count); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		distribution[tier] = count
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Ensure all tiers are represented
	for _, tierType := range []TierType{TierHot, TierWarm, TierCold} {
		if _, exists := distribution[tierType]; !exists {
			distribution[tierType] = 0
		}
	}

	return distribution, nil
}

// GetTierMetrics returns detailed metrics for each tier.
type TierMetrics struct {
	Tier              TierType `json:"tier"`
	RowCount          int64    `json:"row_count"`
	EstimatedSizeGB   float64  `json:"estimated_size_gb"`
	OldestRecordAge   int      `json:"oldest_record_age_days"`
	NewestRecordAge   int      `json:"newest_record_age_days"`
	CompressionRatio  float64  `json:"compression_ratio,omitempty"`
	EstimatedMonthlyCost float64 `json:"estimated_monthly_cost"`
}

// GetMetricsForCategory returns metrics for all tiers in a category.
func (te *TieringEngine) GetMetricsForCategory(ctx context.Context, category string) ([]TierMetrics, error) {
	var metrics []TierMetrics

	query := `
		SELECT 
			tier,
			COUNT(*) as row_count,
			AVG(json_extract(data, '$.estimated_size')) as avg_size,
			CAST((julianday('now') - julianday(MIN(created_at))) as INTEGER) as oldest_age,
			CAST((julianday('now') - julianday(MAX(created_at))) as INTEGER) as newest_age
		FROM analytics_events
		WHERE category = ? AND deleted_at IS NULL
		GROUP BY tier
	`

	rows, err := te.db.QueryContext(ctx, query, category)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var m TierMetrics
		var avgSize sql.NullFloat64
		var oldestAge sql.NullInt32
		var newestAge sql.NullInt32

		if err := rows.Scan(&m.Tier, &m.RowCount, &avgSize, &oldestAge, &newestAge); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// Estimate size in GB (rough calculation)
		if avgSize.Valid {
			m.EstimatedSizeGB = float64(m.RowCount) * avgSize.Float64 / (1024 * 1024 * 1024)
		}

		if oldestAge.Valid {
			m.OldestRecordAge = int(oldestAge.Int32)
		}
		if newestAge.Valid {
			m.NewestRecordAge = int(newestAge.Int32)
		}

		// Calculate estimated cost based on tier storage type
		// Rough estimates: hot=$0.023/GB/month, warm=$0.025/GB/month (S3), cold=$0.004/GB/month (Glacier)
		switch m.Tier {
		case TierHot:
			m.EstimatedMonthlyCost = m.EstimatedSizeGB * 0.023
		case TierWarm:
			m.EstimatedMonthlyCost = m.EstimatedSizeGB * 0.025
		case TierCold:
			m.EstimatedMonthlyCost = m.EstimatedSizeGB * 0.004
		}

		metrics = append(metrics, m)
	}

	return metrics, rows.Err()
}
