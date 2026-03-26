// Package database provides database connection and migration utilities.
package database

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DB wraps a pgxpool.Pool with additional utilities.
type DB struct {
	Pool *pgxpool.Pool
}

// Config holds database connection configuration.
type Config struct {
	URL             string
	MaxOpenConns    int32
	MaxIdleConns    int32
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

// Connect creates a new database connection pool.
func Connect(ctx context.Context, cfg Config) (*DB, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database URL: %w", err)
	}

	poolCfg.MaxConns = cfg.MaxOpenConns
	poolCfg.MinConns = cfg.MaxIdleConns
	poolCfg.MaxConnLifetime = cfg.ConnMaxLifetime
	poolCfg.MaxConnIdleTime = cfg.ConnMaxIdleTime

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Verify connection
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &DB{Pool: pool}, nil
}

// Close closes the database connection pool.
func (db *DB) Close() {
	db.Pool.Close()
}

// Migrator handles database migrations.
type Migrator struct {
	db         *DB
	migrations embed.FS
	basePath   string
}

// Migration represents a single migration file.
type Migration struct {
	Version int
	Name    string
	UpSQL   string
	DownSQL string
}

// NewMigrator creates a new migrator instance.
func NewMigrator(db *DB, migrations embed.FS, basePath string) *Migrator {
	return &Migrator{
		db:         db,
		migrations: migrations,
		basePath:   basePath,
	}
}

// Migrate runs all pending migrations.
func (m *Migrator) Migrate(ctx context.Context) error {
	// Acquire advisory lock to prevent concurrent migrations
	lockID := int64(6832918273645123) // Unique lock ID for Aegion migrations

	conn, err := m.db.Pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Release()

	// Try to acquire lock
	var acquired bool
	err = conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", lockID).Scan(&acquired)
	if err != nil {
		return fmt.Errorf("failed to acquire advisory lock: %w", err)
	}
	if !acquired {
		return fmt.Errorf("another migration is in progress")
	}
	defer conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", lockID)

	// Ensure migrations table exists
	if err := m.ensureMigrationsTable(ctx, conn.Conn()); err != nil {
		return err
	}

	// Load migrations
	migrations, err := m.loadMigrations()
	if err != nil {
		return fmt.Errorf("failed to load migrations: %w", err)
	}

	// Get current version
	currentVersion, err := m.getCurrentVersion(ctx, conn.Conn())
	if err != nil {
		return fmt.Errorf("failed to get current version: %w", err)
	}

	// Apply pending migrations
	for _, mig := range migrations {
		if mig.Version <= currentVersion {
			continue
		}

		if err := m.applyMigration(ctx, conn.Conn(), mig); err != nil {
			return fmt.Errorf("failed to apply migration %d_%s: %w", mig.Version, mig.Name, err)
		}
	}

	return nil
}

// ensureMigrationsTable creates the migrations tracking table.
func (m *Migrator) ensureMigrationsTable(ctx context.Context, conn *pgx.Conn) error {
	_, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version     INT PRIMARY KEY,
			name        TEXT NOT NULL,
			applied_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	return err
}

// getCurrentVersion returns the highest applied migration version.
func (m *Migrator) getCurrentVersion(ctx context.Context, conn *pgx.Conn) (int, error) {
	var version int
	err := conn.QueryRow(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&version)
	return version, err
}

// loadMigrations loads all migration files from the embedded filesystem.
func (m *Migrator) loadMigrations() ([]Migration, error) {
	var migrations []Migration
	migMap := make(map[int]*Migration)

	err := fs.WalkDir(m.migrations, m.basePath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		name := d.Name()
		if !strings.HasSuffix(name, ".sql") {
			return nil
		}

		// Parse version from filename (e.g., "0001_core_identities.up.sql")
		var version int
		var migName string
		var direction string

		parts := strings.SplitN(name, "_", 2)
		if len(parts) < 2 {
			return nil
		}

		if _, err := fmt.Sscanf(parts[0], "%d", &version); err != nil {
			return nil
		}

		rest := parts[1]
		if strings.HasSuffix(rest, ".up.sql") {
			direction = "up"
			migName = strings.TrimSuffix(rest, ".up.sql")
		} else if strings.HasSuffix(rest, ".down.sql") {
			direction = "down"
			migName = strings.TrimSuffix(rest, ".down.sql")
		} else {
			return nil
		}

		content, err := m.migrations.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", path, err)
		}

		mig, ok := migMap[version]
		if !ok {
			mig = &Migration{Version: version, Name: migName}
			migMap[version] = mig
		}

		if direction == "up" {
			mig.UpSQL = string(content)
		} else {
			mig.DownSQL = string(content)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Convert map to sorted slice
	for _, mig := range migMap {
		migrations = append(migrations, *mig)
	}
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})

	return migrations, nil
}

// applyMigration applies a single migration within a transaction.
func (m *Migrator) applyMigration(ctx context.Context, conn *pgx.Conn, mig Migration) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Execute migration SQL
	if _, err := tx.Exec(ctx, mig.UpSQL); err != nil {
		return fmt.Errorf("migration SQL failed: %w", err)
	}

	// Record migration
	_, err = tx.Exec(ctx,
		"INSERT INTO schema_migrations (version, name) VALUES ($1, $2)",
		mig.Version, mig.Name,
	)
	if err != nil {
		return fmt.Errorf("failed to record migration: %w", err)
	}

	return tx.Commit(ctx)
}

// Rollback rolls back the last migration.
func (m *Migrator) Rollback(ctx context.Context) error {
	conn, err := m.db.Pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	// Get current version and down SQL
	var version int
	var name string
	err = conn.QueryRow(ctx,
		"SELECT version, name FROM schema_migrations ORDER BY version DESC LIMIT 1",
	).Scan(&version, &name)
	if err != nil {
		if err == pgx.ErrNoRows {
			return fmt.Errorf("no migrations to rollback")
		}
		return err
	}

	// Find migration file
	migrations, err := m.loadMigrations()
	if err != nil {
		return err
	}

	var mig *Migration
	for i := range migrations {
		if migrations[i].Version == version {
			mig = &migrations[i]
			break
		}
	}

	if mig == nil || mig.DownSQL == "" {
		return fmt.Errorf("no down migration found for version %d", version)
	}

	// Apply rollback
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, mig.DownSQL); err != nil {
		return fmt.Errorf("rollback SQL failed: %w", err)
	}

	if _, err := tx.Exec(ctx, "DELETE FROM schema_migrations WHERE version = $1", version); err != nil {
		return fmt.Errorf("failed to remove migration record: %w", err)
	}

	return tx.Commit(ctx)
}
