package retention

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
)

func TestArchivalExecutor_ArchiveData_UpdatesRowAndWritesToBackend(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	ctx := context.Background()

	policy := DefaultRetentionPolicy()
	manager := NewManager(db, policy)

	backend := NewMockStorageBackend()
	manager.RegisterStorageBackend(TierWarm, backend)

	if err := manager.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Seed category and one stale hot event.
	if _, err := db.ExecContext(ctx, `INSERT INTO event_categories (name, description) VALUES (?, ?)`, "audit_events", "Audit"); err != nil {
		t.Fatalf("failed to seed category: %v", err)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO analytics_events (id, category, tier, data, created_at, updated_at)
		VALUES (?, ?, ?, ?, datetime('now', '-10 days'), datetime('now', '-10 days'))
	`, "evt_1", "audit_events", "hot", `{"estimated_size":1024}`); err != nil {
		t.Fatalf("failed to seed event: %v", err)
	}

	identified, err := manager.executor.identifyRowsForArchival(ctx, "audit_events", TierHot)
	if err != nil {
		t.Fatalf("identifyRowsForArchival failed: %v", err)
	}
	if len(identified) != 1 {
		t.Fatalf("expected identifyRowsForArchival to return 1 row, got %d", len(identified))
	}
	if id, ok := getRowID(identified[0]); !ok || id != "evt_1" {
		t.Fatalf("expected identified row to have id evt_1, got ok=%v id=%q (raw=%T)", ok, id, identified[0]["id"])
	}

	job := &ArchivalJob{
		ID:         "job_1",
		Category:   "audit_events",
		SourceTier: TierHot,
		TargetTier: TierWarm,
	}

	if err := manager.executor.ArchiveData(ctx, job); err != nil {
		var tier string
		var archivePath, archivedAt, deletedAt sql.NullString
		_ = db.QueryRowContext(ctx, `SELECT tier, archive_path, archived_at, deleted_at FROM analytics_events WHERE id = ?`, "evt_1").
			Scan(&tier, &archivePath, &archivedAt, &deletedAt)

		t.Fatalf("ArchiveData failed: %v (row: tier=%q archive_path_valid=%v archived_at_valid=%v deleted_at_valid=%v)",
			err, tier, archivePath.Valid, archivedAt.Valid, deletedAt.Valid)
	}
	if job.Status != "completed" {
		t.Fatalf("expected job status completed, got %q (error=%q)", job.Status, job.Error)
	}
	if job.RowCount != 1 {
		t.Fatalf("expected row_count=1, got %d", job.RowCount)
	}
	if job.BytesTransferred == 0 {
		t.Fatalf("expected bytes_transferred > 0")
	}
	if job.Checksum == "" {
		t.Fatalf("expected checksum to be set")
	}

	var (
		tier       string
		archivePath sql.NullString
		archivedAt sql.NullString
		deletedAt  sql.NullString
	)
	if err := db.QueryRowContext(ctx, `SELECT tier, archive_path, archived_at, deleted_at FROM analytics_events WHERE id = ?`, "evt_1").
		Scan(&tier, &archivePath, &archivedAt, &deletedAt); err != nil {
		t.Fatalf("failed to query archived row: %v", err)
	}

	if tier != "warm" {
		t.Fatalf("expected tier=warm, got %q", tier)
	}
	if !archivePath.Valid || archivePath.String == "" {
		t.Fatalf("expected archive_path to be set")
	}
	if !archivedAt.Valid || archivedAt.String == "" {
		t.Fatalf("expected archived_at to be set")
	}
	if deletedAt.Valid {
		t.Fatalf("expected deleted_at to remain NULL")
	}

	// Ensure we wrote payload into the backend.
	raw, err := backend.Read(ctx, archivePath.String)
	if err != nil {
		t.Fatalf("backend.Read failed: %v", err)
	}
	var decoded []map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("failed to unmarshal backend payload: %v", err)
	}
	if len(decoded) != 1 {
		t.Fatalf("expected 1 row in archived payload, got %d", len(decoded))
	}
	id, _ := decoded[0]["id"].(string)
	if id != "evt_1" {
		t.Fatalf("expected archived payload to include evt_1, got id=%q", id)
	}
}

func TestArchivalExecutor_ArchiveData_NoRowsCompletes(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	ctx := context.Background()

	policy := DefaultRetentionPolicy()
	manager := NewManager(db, policy)

	backend := NewMockStorageBackend()
	manager.RegisterStorageBackend(TierWarm, backend)

	if err := manager.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	if _, err := db.ExecContext(ctx, `INSERT INTO event_categories (name, description) VALUES (?, ?)`, "audit_events", "Audit"); err != nil {
		t.Fatalf("failed to seed category: %v", err)
	}

	// Fresh hot record should not be picked up for archival.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO analytics_events (id, category, tier, data, created_at, updated_at)
		VALUES (?, ?, ?, ?, datetime('now'), datetime('now'))
	`, "evt_fresh", "audit_events", "hot", `{}`); err != nil {
		t.Fatalf("failed to seed event: %v", err)
	}

	job := &ArchivalJob{
		ID:         "job_no_rows",
		Category:   "audit_events",
		SourceTier: TierHot,
		TargetTier: TierWarm,
	}
	if err := manager.executor.ArchiveData(ctx, job); err != nil {
		t.Fatalf("ArchiveData failed: %v", err)
	}
	if job.Status != "completed" {
		t.Fatalf("expected job status completed, got %q (error=%q)", job.Status, job.Error)
	}
	if job.RowCount != 0 {
		t.Fatalf("expected row_count=0, got %d", job.RowCount)
	}
}
