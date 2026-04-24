package retention

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// MockStorageBackend is a mock storage backend for testing.
type MockStorageBackend struct {
	data map[string][]byte
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", "file:retention_test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("failed to open sqlite test db: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	// Keep behavior consistent and surface FK mistakes early.
	_, _ = db.Exec("PRAGMA foreign_keys = ON;")

	return db
}

func NewMockStorageBackend() *MockStorageBackend {
	return &MockStorageBackend{
		data: make(map[string][]byte),
	}
}

func (m *MockStorageBackend) Write(ctx context.Context, namespace string, data []byte) (path string, err error) {
	path = namespace + "/" + time.Now().Format("20060102150405")
	m.data[path] = data
	return path, nil
}

func (m *MockStorageBackend) Read(ctx context.Context, path string) ([]byte, error) {
	if data, exists := m.data[path]; exists {
		return data, nil
	}
	return nil, fmt.Errorf("not found")
}

func (m *MockStorageBackend) Delete(ctx context.Context, path string) error {
	delete(m.data, path)
	return nil
}

// TestRetentionPolicyValidation tests policy validation.
func TestRetentionPolicyValidation(t *testing.T) {
	tests := []struct {
		name    string
		policy  *RetentionPolicy
		wantErr bool
	}{
		{
			name:    "valid default policy",
			policy:  DefaultRetentionPolicy(),
			wantErr: false,
		},
		{
			name: "invalid TTL order",
			policy: &RetentionPolicy{
				Hot:  TierConfig{TTLDays: 10},
				Warm: TierConfig{TTLDays: 5},
				Cold: TierConfig{TTLDays: 100},
			},
			wantErr: true,
		},
		{
			name: "valid custom policy",
			policy: &RetentionPolicy{
				DefaultPolicy: "tiered",
				Hot:           TierConfig{TTLDays: 7, Enabled: true},
				Warm:          TierConfig{TTLDays: 90, Enabled: true},
				Cold:          TierConfig{TTLDays: 730, Enabled: true},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.policy.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestGetTierForTimestamp tests tier determination based on timestamp.
func TestGetTierForTimestamp(t *testing.T) {
	policy := DefaultRetentionPolicy()

	tests := []struct {
		name     string
		age      time.Duration
		expected TierType
	}{
		{
			name:     "recent data in hot tier",
			age:      24 * time.Hour,
			expected: TierHot,
		},
		{
			name:     "old data in warm tier",
			age:      30 * 24 * time.Hour,
			expected: TierWarm,
		},
		{
			name:     "very old data in cold tier",
			age:      200 * 24 * time.Hour,
			expected: TierCold,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recordTime := time.Now().Add(-tt.age)
			tier := policy.GetTierForTimestamp("test_category", recordTime)
			if tier != tt.expected {
				t.Errorf("GetTierForTimestamp() got = %v, want %v", tier, tt.expected)
			}
		})
	}
}

// TestIsExpired tests data expiration check.
func TestIsExpired(t *testing.T) {
	policy := DefaultRetentionPolicy()

	tests := []struct {
		name     string
		age      time.Duration
		expected bool
	}{
		{
			name:     "recent data not expired",
			age:      100 * 24 * time.Hour,
			expected: false,
		},
		{
			name:     "very old data expired",
			age:      800 * 24 * time.Hour,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recordTime := time.Now().Add(-tt.age)
			expired := policy.IsExpired("test_category", recordTime)
			if expired != tt.expected {
				t.Errorf("IsExpired() got = %v, want %v", expired, tt.expected)
			}
		})
	}
}

// TestCategorySpecificPolicies tests category-specific retention overrides.
func TestCategorySpecificPolicies(t *testing.T) {
	policy := DefaultRetentionPolicy()
	policy.Categories = map[string]CategoryRetention{
		"audit_events": {
			HotDays:  30,
			WarmDays: 180,
			ColdDays: 730,
		},
	}

	// Test that audit_events uses custom TTLs
	recordTime := time.Now().Add(-15 * 24 * time.Hour)
	tier := policy.GetTierForTimestamp("audit_events", recordTime)
	if tier != TierHot {
		t.Errorf("expected hot tier for 15-day-old audit event, got %v", tier)
	}

	recordTime = time.Now().Add(-50 * 24 * time.Hour)
	tier = policy.GetTierForTimestamp("audit_events", recordTime)
	if tier != TierWarm {
		t.Errorf("expected warm tier for 50-day-old audit event, got %v", tier)
	}
}

// TestTieringEngine tests tier transitions.
func TestTieringEngine(t *testing.T) {
	// Create in-memory database
	db := openTestDB(t)
	defer db.Close()

	// Create test table
	createTableQuery := `
		CREATE TABLE analytics_events (
			id TEXT PRIMARY KEY,
			category TEXT,
			tier TEXT,
			created_at TIMESTAMP,
			tier_updated_at TIMESTAMP,
			deleted_at TIMESTAMP
		)
	`
	if _, err := db.Exec(createTableQuery); err != nil {
		t.Fatalf("failed to create test table: %v", err)
	}

	policy := &RetentionPolicy{
		Hot:  TierConfig{TTLDays: 1, Enabled: true},
		Warm: TierConfig{TTLDays: 2, Enabled: true},
		Cold: TierConfig{TTLDays: 3, Enabled: true},
	}
	if err := policy.Validate(); err != nil {
		t.Fatalf("policy validation failed: %v", err)
	}
	auditLog := &NoopAuditLog{}

	engine := NewTieringEngine(db, policy, auditLog)
	ctx := context.Background()

	// Insert data: one hot stale (should move to warm), one hot fresh (stays hot),
	// and one warm stale (should move to cold).
	_, err := db.ExecContext(ctx, `
		INSERT INTO analytics_events (id, category, tier, created_at)
		VALUES
			('hot_old', 'test', 'hot', datetime('now', '-2 days')),
			('hot_new', 'test', 'hot', datetime('now')),
			('warm_old', 'test', 'warm', datetime('now', '-3 days'))
	`)
	if err != nil {
		t.Fatalf("failed to insert seed events: %v", err)
	}

	transition, err := engine.TransitionStaleData(ctx, "test")
	if err != nil {
		t.Fatalf("TransitionStaleData failed: %v", err)
	}
	if transition.Status != "completed" {
		t.Fatalf("expected completed transition, got %q (error=%q)", transition.Status, transition.Error)
	}

	distribution, err := engine.GetDataDistribution(ctx, "test")
	if err != nil {
		t.Fatalf("GetDataDistribution failed: %v", err)
	}

	if distribution[TierHot] != 1 || distribution[TierWarm] != 1 || distribution[TierCold] != 1 {
		t.Fatalf("unexpected tier distribution: hot=%d warm=%d cold=%d", distribution[TierHot], distribution[TierWarm], distribution[TierCold])
	}

	// Ensure tier_updated_at was set for moved rows.
	var updatedHotOld sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT tier_updated_at FROM analytics_events WHERE id = 'hot_old'`).Scan(&updatedHotOld); err != nil {
		t.Fatalf("failed to query tier_updated_at: %v", err)
	}
	if !updatedHotOld.Valid {
		t.Fatalf("expected tier_updated_at to be set for hot_old")
	}
}

// TestCleanupManager tests cleanup operations.
func TestCleanupManager(t *testing.T) {
	// Create in-memory database
	db := openTestDB(t)
	defer db.Close()

	// Create test table
	createTableQuery := `
		CREATE TABLE analytics_events (
			id TEXT PRIMARY KEY,
			category TEXT,
			tier TEXT,
			data TEXT,
			created_at TIMESTAMP,
			archived_at TIMESTAMP,
			archive_path TEXT,
			deleted_at TIMESTAMP
		)
	`
	if _, err := db.Exec(createTableQuery); err != nil {
		t.Fatalf("failed to create test table: %v", err)
	}

	policy := &RetentionPolicy{
		Hot:  TierConfig{TTLDays: 1, Enabled: true},
		Warm: TierConfig{TTLDays: 2, Enabled: true},
		Cold: TierConfig{TTLDays: 3, Enabled: true},
	}
	if err := policy.Validate(); err != nil {
		t.Fatalf("policy validation failed: %v", err)
	}
	auditLog := &NoopAuditLog{}

	manager := NewCleanupManager(db, policy, auditLog)
	ctx := context.Background()

	// Seed: 1 expired cold record and 1 soft-deleted record eligible for cleanup.
	_, err := db.ExecContext(ctx, `
		INSERT INTO analytics_events (id, category, tier, created_at, deleted_at)
		VALUES
			('expired_cold', 'test', 'cold', datetime('now', '-10 days'), NULL),
			('soft_deleted', 'test', 'hot', datetime('now', '-1 days'), datetime('now', '-10 days'))
	`)
	if err != nil {
		t.Fatalf("failed to insert seed events: %v", err)
	}

	job, err := manager.CleanupExpiredData(ctx, "test")
	if err != nil {
		t.Fatalf("CleanupExpiredData failed: %v", err)
	}
	if job.Status != "completed" {
		t.Fatalf("expected completed expired cleanup, got %q (error=%q)", job.Status, job.Error)
	}
	if job.RowsDeleted != 1 {
		t.Fatalf("expected 1 expired row deleted, got %d", job.RowsDeleted)
	}

	job, err = manager.CleanupSoftDeletedData(ctx, "test", 5)
	if err != nil {
		t.Fatalf("CleanupSoftDeletedData failed: %v", err)
	}
	if job.Status != "completed" {
		t.Fatalf("expected completed soft cleanup, got %q (error=%q)", job.Status, job.Error)
	}
	if job.RowsDeleted != 1 {
		t.Fatalf("expected 1 soft-deleted row deleted, got %d", job.RowsDeleted)
	}

	// Test cleanup stats (now empty)
	stats, err := manager.GetCleanupStats(ctx, "test")
	if err != nil {
		t.Fatalf("GetCleanupStats failed: %v", err)
	}

	if stats.TotalRecords != 0 {
		t.Errorf("expected 0 records, got %d", stats.TotalRecords)
	}
}

// TestAuditLog tests audit logging.
func TestDatabaseAuditLog(t *testing.T) {
	// Create in-memory database
	db := openTestDB(t)
	defer db.Close()

	auditLog := NewDatabaseAuditLog(db)
	ctx := context.Background()

	if err := auditLog.Initialize(ctx); err != nil {
		t.Fatalf("failed to initialize audit log: %v", err)
	}

	// Log a message
	if err := auditLog.LogMessage(ctx, "test_component", "test message"); err != nil {
		t.Fatalf("LogMessage failed: %v", err)
	}

	// Retrieve logs
	logs, err := auditLog.GetLogs(ctx, "test_component", 10)
	if err != nil {
		t.Fatalf("GetLogs failed: %v", err)
	}

	if len(logs) != 1 {
		t.Errorf("expected 1 log entry, got %d", len(logs))
	}

	if logs[0].Component != "test_component" {
		t.Errorf("expected component 'test_component', got %s", logs[0].Component)
	}
}

// TestScheduler tests the job scheduler.
func TestJobScheduler(t *testing.T) {
	policy := DefaultRetentionPolicy()
	executor := NewArchivalExecutor(nil, policy, &NoopAuditLog{})
	tieringEngine := NewTieringEngine(nil, policy, &NoopAuditLog{})
	cleanupManager := NewCleanupManager(nil, policy, &NoopAuditLog{})
	auditLog := &NoopAuditLog{}

	config := &ScheduleConfig{
		ArchivalInterval: 100 * time.Millisecond,
		CleanupInterval:  100 * time.Millisecond,
		TieringInterval:  100 * time.Millisecond,
		Categories:       []string{},
	}

	scheduler := NewJobScheduler(config, executor, tieringEngine, cleanupManager, auditLog)

	if scheduler.IsRunning() {
		t.Error("scheduler should not be running before Start()")
	}

	ctx := context.Background()
	if err := scheduler.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if !scheduler.IsRunning() {
		t.Error("scheduler should be running after Start()")
	}

	if err := scheduler.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	if scheduler.IsRunning() {
		t.Error("scheduler should not be running after Stop()")
	}
}

// TestManager tests the main retention manager.
func TestManager(t *testing.T) {
	// Create in-memory database
	db := openTestDB(t)
	defer db.Close()

	policy := DefaultRetentionPolicy()
	manager := NewManager(db, policy)

	ctx := context.Background()
	if err := manager.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	status := manager.GetStatus()
	if !status.Initialized {
		t.Error("manager should be initialized")
	}

	// Test updating policy
	newPolicy := DefaultRetentionPolicy()
	newPolicy.Hot.TTLDays = 14
	if err := manager.UpdatePolicy(newPolicy); err != nil {
		t.Fatalf("UpdatePolicy failed: %v", err)
	}

	if manager.GetPolicy().Hot.TTLDays != 14 {
		t.Errorf("expected hot TTL of 14, got %d", manager.GetPolicy().Hot.TTLDays)
	}

	if err := manager.Close(ctx); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}
