package analytics

// Migration support utilities for analytics module
// Migrations are stored in ./migrations directory as .up.sql and .down.sql files

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// RunMigrations executes all pending migrations
func RunMigrations(ctx context.Context, db *sql.DB) error {
	// Create migrations table if not exists
	if err := createMigrationsTable(ctx, db); err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	// Load migrations
	migrations, err := loadMigrationFiles()
	if err != nil {
		return fmt.Errorf("failed to load migrations: %w", err)
	}

	// Get applied migrations
	applied := make(map[string]bool)
	rows, err := db.QueryContext(ctx, "SELECT version FROM schema_migrations WHERE type = 'up'")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var version string
			if err := rows.Scan(&version); err != nil {
				return err
			}
			applied[version] = true
		}
	}

	// Sort and apply migrations
	versions := make([]string, 0, len(migrations))
	for v := range migrations {
		versions = append(versions, v)
	}
	sort.Strings(versions)

	for _, version := range versions {
		if applied[version] {
			continue
		}

		upSQL := migrations[version]["up"]
		if upSQL == "" {
			continue
		}

		// Execute migration in transaction
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}

		start := time.Now()
		if _, err := tx.ExecContext(ctx, upSQL); err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				return fmt.Errorf("migration %s failed: %w; rollback error: %v", version, err, rbErr)
			}
			return fmt.Errorf("migration %s failed: %w", version, err)
		}

		duration := int(time.Since(start).Milliseconds())
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO schema_migrations (version, type, execution_time) VALUES ($1, $2, $3)",
			version, "up", duration,
		); err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				return fmt.Errorf("migration %s insert failed: %w; rollback error: %v", version, err, rbErr)
			}
			return err
		}

		if err := tx.Commit(); err != nil {
			return err
		}
	}

	return nil
}

// RollbackMigration rolls back the last applied migration
func RollbackMigration(ctx context.Context, db *sql.DB) error {
	// Get last applied migration
	var version string
	err := db.QueryRowContext(ctx,
		"SELECT version FROM schema_migrations WHERE type = 'up' ORDER BY version DESC LIMIT 1",
	).Scan(&version)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}

	// Load migrations
	migrations, err := loadMigrationFiles()
	if err != nil {
		return err
	}

	downSQL := migrations[version]["down"]
	if downSQL == "" {
		return fmt.Errorf("no down migration for %s", version)
	}

	// Execute rollback in transaction
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, downSQL); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("rollback %s failed: %w; rollback error: %v", version, err, rbErr)
		}
		return fmt.Errorf("rollback %s failed: %w", version, err)
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM schema_migrations WHERE version = $1", version); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("delete migration record %s failed: %w; rollback error: %v", version, err, rbErr)
		}
		return err
	}

	return tx.Commit()
}

func createMigrationsTable(ctx context.Context, db *sql.DB) error {
	query := `
	CREATE TABLE IF NOT EXISTS schema_migrations (
		id SERIAL PRIMARY KEY,
		version VARCHAR(255) NOT NULL UNIQUE,
		type VARCHAR(10) NOT NULL,
		execution_time INTEGER DEFAULT 0,
		installed_on TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)
	`
	_, err := db.ExecContext(ctx, query)
	return err
}

func loadMigrationFiles() (map[string]map[string]string, error) {
	migrations := make(map[string]map[string]string)

	// Get migrations directory
	migrationsDir := getMigrationsDirectory()

	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read migrations directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		filename := entry.Name()
		if !strings.HasSuffix(filename, ".sql") {
			continue
		}

		// Parse filename: 0001_name.up.sql or 0001_name.down.sql
		parts := strings.Split(filename, ".")
		if len(parts) < 3 {
			continue
		}

		version := parts[0]
		direction := parts[len(parts)-2]

		if direction != "up" && direction != "down" {
			continue
		}

		content, err := os.ReadFile(filepath.Join(migrationsDir, filename))
		if err != nil {
			return nil, fmt.Errorf("failed to read migration %s: %w", filename, err)
		}

		if _, ok := migrations[version]; !ok {
			migrations[version] = make(map[string]string)
		}

		migrations[version][direction] = string(content)
	}

	return migrations, nil
}

func getMigrationsDirectory() string {
	// Try common paths
	paths := []string{
		"./modules/analytics/migrations",
		"./migrations",
		"/app/migrations",
	}

	for _, p := range paths {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return p
		}
	}

	// Default fallback
	return "./modules/analytics/migrations"
}
