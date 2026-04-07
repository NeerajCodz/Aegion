// Package database provides database connection and migration utilities.
package database

import (
	"context"
	"fmt"
	"hash/fnv"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	parsePoolConfig   = pgxpool.ParseConfig
	newPoolWithConfig = pgxpool.NewWithConfig
	pingPool          = func(ctx context.Context, pool *pgxpool.Pool) error { return pool.Ping(ctx) }
	closePool         = func(pool *pgxpool.Pool) { pool.Close() }
)

const (
	defaultMigrationLockID = int64(6832918273645123)
	defaultMigrationTable  = "schema_migrations"
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
	poolCfg, err := parsePoolConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database URL: %w", err)
	}

	poolCfg.MaxConns = cfg.MaxOpenConns
	poolCfg.MinConns = cfg.MaxIdleConns
	poolCfg.MaxConnLifetime = cfg.ConnMaxLifetime
	poolCfg.MaxConnIdleTime = cfg.ConnMaxIdleTime

	pool, err := newPoolWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Verify connection
	if err := pingPool(ctx, pool); err != nil {
		closePool(pool)
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &DB{Pool: pool}, nil
}

// Close closes the database connection pool.
func (db *DB) Close() {
	if db == nil || db.Pool == nil {
		return
	}
	closePool(db.Pool)
}

// Migrator handles database migrations.
type Migrator struct {
	db          *DB
	migrations  fs.FS
	basePath    string
	tableName   string
	lockID      int64
	acquireConn func(ctx context.Context) (pooledMigrationConn, error)
	beginTx     func(ctx context.Context, conn migrationBeginner) (migrationTx, error)
}

type migrationExecutor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type migrationBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

type migrationTx interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

type pooledMigrationConn interface {
	migrationExecutor
	migrationBeginner
	Release()
}

// Migration represents a single migration file.
type Migration struct {
	Version int
	Name    string
	UpSQL   string
	DownSQL string
}

// NewMigrator creates a new migrator instance.
func NewMigrator(db *DB, migrations fs.FS, basePath string) *Migrator {
	migrator := &Migrator{
		db:         db,
		migrations: migrations,
		basePath:   basePath,
		tableName:  defaultMigrationTable,
		lockID:     defaultMigrationLockID,
	}
	migrator.acquireConn = func(ctx context.Context) (pooledMigrationConn, error) {
		return migrator.db.Pool.Acquire(ctx)
	}
	migrator.beginTx = func(ctx context.Context, conn migrationBeginner) (migrationTx, error) {
		tx, err := conn.Begin(ctx)
		if err != nil {
			return nil, err
		}
		return tx, nil
	}
	return migrator
}

// NewModuleMigrator creates a migrator that tracks migrations for a specific module.
func NewModuleMigrator(db *DB, migrations fs.FS, basePath, moduleID string) *Migrator {
	migrator := NewMigrator(db, migrations, basePath)
	safeID := sanitizeIdentifier(moduleID)
	migrator.tableName = fmt.Sprintf("%s_%s", defaultMigrationTable, safeID)
	migrator.lockID = moduleMigrationLockID(safeID)
	return migrator
}

func sanitizeIdentifier(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return "module"
	}

	var b strings.Builder
	b.Grow(len(raw))
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}

	sanitized := strings.Trim(b.String(), "_")
	if sanitized == "" {
		return "module"
	}
	return sanitized
}

func moduleMigrationLockID(moduleID string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte("aegion_migrations_" + moduleID))
	return int64(h.Sum64() & 0x7fffffffffffffff)
}

// Migrate runs all pending migrations.
func (m *Migrator) Migrate(ctx context.Context) error {
	// Acquire advisory lock to prevent concurrent migrations
	lockID := m.lockID

	conn, err := m.acquireConn(ctx)
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
	defer func() {
		if _, unlockErr := conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", lockID); unlockErr != nil {
			_ = unlockErr
		}
	}()

	// Ensure migrations table exists
	if err := m.ensureMigrationsTable(ctx, conn); err != nil {
		return err
	}

	// Load migrations
	migrations, err := m.loadMigrations()
	if err != nil {
		return fmt.Errorf("failed to load migrations: %w", err)
	}

	// Get current version
	currentVersion, err := m.getCurrentVersion(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to get current version: %w", err)
	}

	// Apply pending migrations
	for _, mig := range migrations {
		if mig.Version <= currentVersion {
			continue
		}

		if err := m.applyMigration(ctx, conn, mig); err != nil {
			return fmt.Errorf("failed to apply migration %d_%s: %w", mig.Version, mig.Name, err)
		}
	}

	return nil
}

// ensureMigrationsTable creates the migrations tracking table.
func (m *Migrator) ensureMigrationsTable(ctx context.Context, conn migrationExecutor) error {
	stmt := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			version     INT PRIMARY KEY,
			name        TEXT NOT NULL,
			applied_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`, m.tableName)
	_, err := conn.Exec(ctx, stmt)
	return err
}

// getCurrentVersion returns the highest applied migration version.
func (m *Migrator) getCurrentVersion(ctx context.Context, conn migrationExecutor) (int, error) {
	var version int
	query := fmt.Sprintf("SELECT COALESCE(MAX(version), 0) FROM %s", m.tableName)
	err := conn.QueryRow(ctx, query).Scan(&version)
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

		content, err := fs.ReadFile(m.migrations, path)
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
func (m *Migrator) applyMigration(ctx context.Context, conn migrationBeginner, mig Migration) error {
	tx, err := m.beginTx(ctx, conn)
	if err != nil {
		return err
	}
	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && rollbackErr != pgx.ErrTxClosed {
			_ = rollbackErr
		}
	}()

	// Execute migration SQL
	if _, err := tx.Exec(ctx, mig.UpSQL); err != nil {
		return fmt.Errorf("migration SQL failed: %w", err)
	}

	// Record migration
	insertStmt := fmt.Sprintf("INSERT INTO %s (version, name) VALUES ($1, $2)", m.tableName)
	_, err = tx.Exec(ctx,
		insertStmt,
		mig.Version, mig.Name,
	)
	if err != nil {
		return fmt.Errorf("failed to record migration: %w", err)
	}

	return tx.Commit(ctx)
}

// Rollback rolls back the last migration.
func (m *Migrator) Rollback(ctx context.Context) error {
	conn, err := m.acquireConn(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	// Get current version and down SQL
	var version int
	var name string
	err = conn.QueryRow(ctx,
		fmt.Sprintf("SELECT version, name FROM %s ORDER BY version DESC LIMIT 1", m.tableName),
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
	tx, err := m.beginTx(ctx, conn)
	if err != nil {
		return err
	}
	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && rollbackErr != pgx.ErrTxClosed {
			_ = rollbackErr
		}
	}()

	if _, err := tx.Exec(ctx, mig.DownSQL); err != nil {
		return fmt.Errorf("rollback SQL failed: %w", err)
	}

	deleteStmt := fmt.Sprintf("DELETE FROM %s WHERE version = $1", m.tableName)
	if _, err := tx.Exec(ctx, deleteStmt, version); err != nil {
		return fmt.Errorf("failed to remove migration record: %w", err)
	}

	return tx.Commit(ctx)
}
