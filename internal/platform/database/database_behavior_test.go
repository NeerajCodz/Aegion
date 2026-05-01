package database

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed testdata/migrations/*.sql
var behaviorMigrations embed.FS

type migrationStubRow struct {
	values []interface{}
	err    error
}

func (r migrationStubRow) Scan(dest ...interface{}) error {
	if r.err != nil {
		return r.err
	}

	if len(dest) > len(r.values) {
		return fmt.Errorf("insufficient values for scan")
	}

	for i := range dest {
		switch d := dest[i].(type) {
		case *bool:
			v, ok := r.values[i].(bool)
			if !ok {
				return fmt.Errorf("value %d is not bool", i)
			}
			*d = v
		case *int:
			v, ok := r.values[i].(int)
			if !ok {
				return fmt.Errorf("value %d is not int", i)
			}
			*d = v
		case *string:
			v, ok := r.values[i].(string)
			if !ok {
				return fmt.Errorf("value %d is not string", i)
			}
			*d = v
		default:
			return fmt.Errorf("unsupported destination type %T", dest[i])
		}
	}
	return nil
}

type migrationStubConn struct {
	queryRowFn func(ctx context.Context, sql string, args ...interface{}) pgx.Row
	execFn     func(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error)
	beginFn    func(ctx context.Context) (pgx.Tx, error)

	execSQL  []string
	execArgs [][]interface{}
	released bool
}

func (c *migrationStubConn) Exec(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error) {
	c.execSQL = append(c.execSQL, sql)
	c.execArgs = append(c.execArgs, arguments)
	if c.execFn != nil {
		return c.execFn(ctx, sql, arguments...)
	}
	return pgconn.NewCommandTag("SELECT 1"), nil
}

func (c *migrationStubConn) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	if c.queryRowFn != nil {
		return c.queryRowFn(ctx, sql, args...)
	}
	return migrationStubRow{err: errors.New("unexpected query")}
}

func (c *migrationStubConn) Begin(ctx context.Context) (pgx.Tx, error) {
	if c.beginFn != nil {
		return c.beginFn(ctx)
	}
	return nil, errors.New("unexpected begin")
}

func (c *migrationStubConn) Release() {
	c.released = true
}

type migrationStubTx struct {
	execErrOn   int
	execErr     error
	commitErr   error
	rollbackErr error

	execCalls  int
	execSQL    []string
	execArgs   [][]interface{}
	committed  bool
	rolledBack bool
}

func (t *migrationStubTx) Exec(_ context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error) {
	t.execCalls++
	t.execSQL = append(t.execSQL, sql)
	t.execArgs = append(t.execArgs, arguments)
	if t.execErrOn > 0 && t.execCalls == t.execErrOn {
		if t.execErr != nil {
			return pgconn.CommandTag{}, t.execErr
		}
		return pgconn.CommandTag{}, errors.New("exec failed")
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (t *migrationStubTx) Commit(_ context.Context) error {
	t.committed = true
	return t.commitErr
}

func (t *migrationStubTx) Rollback(_ context.Context) error {
	t.rolledBack = true
	return t.rollbackErr
}

func restorePoolSeams(t *testing.T) {
	t.Helper()
	origParse := parsePoolConfig
	origNewPool := newPoolWithConfig
	origPing := pingPool
	origClose := closePool
	t.Cleanup(func() {
		parsePoolConfig = origParse
		newPoolWithConfig = origNewPool
		pingPool = origPing
		closePool = origClose
	})
}

func TestConnect_WithSeams(t *testing.T) {
	t.Run("parse config failure", func(t *testing.T) {
		restorePoolSeams(t)
		parsePoolConfig = func(string) (*pgxpool.Config, error) {
			return nil, errors.New("bad url")
		}

		db, err := Connect(context.Background(), Config{URL: "bad"})
		if err == nil || !strings.Contains(err.Error(), "failed to parse database URL") {
			t.Fatalf("expected parse error, got %v", err)
		}
		if db != nil {
			t.Fatalf("expected nil db on parse failure")
		}
	})

	t.Run("pool creation failure", func(t *testing.T) {
		restorePoolSeams(t)
		parsePoolConfig = func(string) (*pgxpool.Config, error) {
			return &pgxpool.Config{}, nil
		}
		newPoolWithConfig = func(context.Context, *pgxpool.Config) (*pgxpool.Pool, error) {
			return nil, errors.New("dial failed")
		}

		db, err := Connect(context.Background(), Config{URL: "postgres://example"})
		if err == nil || !strings.Contains(err.Error(), "failed to create connection pool") {
			t.Fatalf("expected create pool error, got %v", err)
		}
		if db != nil {
			t.Fatalf("expected nil db on pool creation failure")
		}
	})

	t.Run("ping failure closes pool", func(t *testing.T) {
		restorePoolSeams(t)
		fakePool := &pgxpool.Pool{}
		closeCalls := 0

		parsePoolConfig = func(string) (*pgxpool.Config, error) {
			return &pgxpool.Config{}, nil
		}
		newPoolWithConfig = func(context.Context, *pgxpool.Config) (*pgxpool.Pool, error) {
			return fakePool, nil
		}
		pingPool = func(context.Context, *pgxpool.Pool) error {
			return errors.New("ping failed")
		}
		closePool = func(pool *pgxpool.Pool) {
			if pool != fakePool {
				t.Fatalf("close called with unexpected pool")
			}
			closeCalls++
		}

		db, err := Connect(context.Background(), Config{URL: "postgres://example"})
		if err == nil || !strings.Contains(err.Error(), "failed to ping database") {
			t.Fatalf("expected ping failure, got %v", err)
		}
		if db != nil {
			t.Fatalf("expected nil db on ping failure")
		}
		if closeCalls != 1 {
			t.Fatalf("expected close called once on ping failure, got %d", closeCalls)
		}
	})

	t.Run("success applies pool config and supports close", func(t *testing.T) {
		restorePoolSeams(t)
		fakePool := &pgxpool.Pool{}
		closeCalls := 0
		var seenCfg *pgxpool.Config

		parsePoolConfig = func(url string) (*pgxpool.Config, error) {
			if url != "postgres://valid" {
				t.Fatalf("unexpected URL passed to parse: %s", url)
			}
			return &pgxpool.Config{}, nil
		}
		newPoolWithConfig = func(_ context.Context, cfg *pgxpool.Config) (*pgxpool.Pool, error) {
			seenCfg = cfg
			return fakePool, nil
		}
		pingPool = func(_ context.Context, pool *pgxpool.Pool) error {
			if pool != fakePool {
				t.Fatalf("ping called with unexpected pool")
			}
			return nil
		}
		closePool = func(pool *pgxpool.Pool) {
			if pool != fakePool {
				t.Fatalf("close called with unexpected pool")
			}
			closeCalls++
		}

		cfg := Config{
			URL:             "postgres://valid",
			MaxOpenConns:    20,
			MaxIdleConns:    5,
			ConnMaxLifetime: 30 * time.Minute,
			ConnMaxIdleTime: 10 * time.Minute,
		}
		db, err := Connect(context.Background(), cfg)
		if err != nil {
			t.Fatalf("Connect returned error: %v", err)
		}
		if db == nil || db.Pool != fakePool {
			t.Fatalf("expected returned DB with fake pool")
		}
		if seenCfg == nil {
			t.Fatalf("expected newPoolWithConfig to receive config")
		}
		if seenCfg.MaxConns != cfg.MaxOpenConns || seenCfg.MinConns != cfg.MaxIdleConns {
			t.Fatalf("pool conn settings were not applied from Config")
		}
		if seenCfg.MaxConnLifetime != cfg.ConnMaxLifetime || seenCfg.MaxConnIdleTime != cfg.ConnMaxIdleTime {
			t.Fatalf("pool lifetime settings were not applied from Config")
		}

		db.Close()
		if closeCalls != 1 {
			t.Fatalf("expected close called once via DB.Close, got %d", closeCalls)
		}

		var nilDB *DB
		nilDB.Close()   // nil receiver should be a no-op
		(&DB{}).Close() // nil pool should be a no-op
		if closeCalls != 1 {
			t.Fatalf("expected no extra close calls for nil cases, got %d", closeCalls)
		}
	})
}

func TestLoadMigrations_BasePathError(t *testing.T) {
	migrator := NewMigrator(&DB{}, behaviorMigrations, "testdata/missing")
	if _, err := migrator.loadMigrations(); err == nil {
		t.Fatalf("expected loadMigrations to fail for missing base path")
	}
}

func TestEnsureMigrationsTableAndGetCurrentVersion(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		migrator := NewMigrator(&DB{}, behaviorMigrations, "testdata/migrations")
		conn := &migrationStubConn{
			execFn: func(_ context.Context, sql string, _ ...interface{}) (pgconn.CommandTag, error) {
				if !strings.Contains(sql, "CREATE TABLE IF NOT EXISTS schema_migrations") {
					t.Fatalf("unexpected SQL for ensure table: %s", sql)
				}
				return pgconn.NewCommandTag("CREATE TABLE"), nil
			},
			queryRowFn: func(_ context.Context, sql string, _ ...interface{}) pgx.Row {
				if !strings.Contains(sql, "COALESCE(MAX(version), 0)") {
					t.Fatalf("unexpected SQL for current version: %s", sql)
				}
				return migrationStubRow{values: []interface{}{7}}
			},
		}

		if err := migrator.ensureMigrationsTable(context.Background(), conn); err != nil {
			t.Fatalf("ensureMigrationsTable failed: %v", err)
		}
		version, err := migrator.getCurrentVersion(context.Background(), conn)
		if err != nil {
			t.Fatalf("getCurrentVersion failed: %v", err)
		}
		if version != 7 {
			t.Fatalf("expected current version 7, got %d", version)
		}
	})

	t.Run("errors bubble up", func(t *testing.T) {
		migrator := NewMigrator(&DB{}, behaviorMigrations, "testdata/migrations")
		conn := &migrationStubConn{
			execFn: func(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("exec failed")
			},
			queryRowFn: func(context.Context, string, ...interface{}) pgx.Row {
				return migrationStubRow{err: errors.New("scan failed")}
			},
		}

		if err := migrator.ensureMigrationsTable(context.Background(), conn); err == nil {
			t.Fatalf("expected ensureMigrationsTable error")
		}
		if _, err := migrator.getCurrentVersion(context.Background(), conn); err == nil {
			t.Fatalf("expected getCurrentVersion error")
		}
	})
}

func TestApplyMigration_WithSeams(t *testing.T) {
	migrator := NewMigrator(&DB{}, behaviorMigrations, "testdata/migrations")
	mig := Migration{Version: 3, Name: "sample", UpSQL: "CREATE TABLE sample (id INT)"}
	conn := &migrationStubConn{}

	t.Run("begin transaction error", func(t *testing.T) {
		migrator.beginTx = func(context.Context, migrationBeginner) (migrationTx, error) {
			return nil, errors.New("begin failed")
		}
		if err := migrator.applyMigration(context.Background(), conn, mig); err == nil || err.Error() != "begin failed" {
			t.Fatalf("expected begin failed, got %v", err)
		}
	})

	t.Run("migration SQL error", func(t *testing.T) {
		tx := &migrationStubTx{execErrOn: 1, execErr: errors.New("sql failed")}
		migrator.beginTx = func(context.Context, migrationBeginner) (migrationTx, error) {
			return tx, nil
		}
		err := migrator.applyMigration(context.Background(), conn, mig)
		if err == nil || !strings.Contains(err.Error(), "migration SQL failed: sql failed") {
			t.Fatalf("expected wrapped migration SQL error, got %v", err)
		}
		if !tx.rolledBack {
			t.Fatalf("expected rollback on migration SQL failure")
		}
	})

	t.Run("record migration error", func(t *testing.T) {
		tx := &migrationStubTx{execErrOn: 2, execErr: errors.New("insert failed")}
		migrator.beginTx = func(context.Context, migrationBeginner) (migrationTx, error) {
			return tx, nil
		}
		err := migrator.applyMigration(context.Background(), conn, mig)
		if err == nil || !strings.Contains(err.Error(), "failed to record migration: insert failed") {
			t.Fatalf("expected wrapped record error, got %v", err)
		}
		if !tx.rolledBack {
			t.Fatalf("expected rollback on record migration failure")
		}
	})

	t.Run("commit error", func(t *testing.T) {
		tx := &migrationStubTx{
			commitErr:   errors.New("commit failed"),
			rollbackErr: pgx.ErrTxClosed,
		}
		migrator.beginTx = func(context.Context, migrationBeginner) (migrationTx, error) {
			return tx, nil
		}
		err := migrator.applyMigration(context.Background(), conn, mig)
		if err == nil || err.Error() != "commit failed" {
			t.Fatalf("expected commit failed, got %v", err)
		}
		if !tx.committed {
			t.Fatalf("expected commit attempt before commit error")
		}
	})

	t.Run("success", func(t *testing.T) {
		tx := &migrationStubTx{rollbackErr: pgx.ErrTxClosed}
		migrator.beginTx = func(context.Context, migrationBeginner) (migrationTx, error) {
			return tx, nil
		}
		if err := migrator.applyMigration(context.Background(), conn, mig); err != nil {
			t.Fatalf("applyMigration returned error: %v", err)
		}
		if !tx.committed {
			t.Fatalf("expected commit on successful migration")
		}
		if len(tx.execSQL) != 2 {
			t.Fatalf("expected two exec calls (up SQL + schema_migrations insert), got %d", len(tx.execSQL))
		}
		if !strings.Contains(tx.execSQL[1], "INSERT INTO schema_migrations") {
			t.Fatalf("expected second exec to record schema migration")
		}
	})
}

func TestMigrate_WithSeams(t *testing.T) {
	t.Run("acquire connection error", func(t *testing.T) {
		migrator := NewMigrator(&DB{}, behaviorMigrations, "testdata/migrations")
		migrator.acquireConn = func(context.Context) (pooledMigrationConn, error) {
			return nil, errors.New("acquire failed")
		}
		err := migrator.Migrate(context.Background())
		if err == nil || !strings.Contains(err.Error(), "failed to acquire connection: acquire failed") {
			t.Fatalf("expected acquire error, got %v", err)
		}
	})

	t.Run("advisory lock query error", func(t *testing.T) {
		migrator := NewMigrator(&DB{}, behaviorMigrations, "testdata/migrations")
		conn := &migrationStubConn{
			queryRowFn: func(context.Context, string, ...interface{}) pgx.Row {
				return migrationStubRow{err: errors.New("lock query failed")}
			},
		}
		migrator.acquireConn = func(context.Context) (pooledMigrationConn, error) { return conn, nil }

		err := migrator.Migrate(context.Background())
		if err == nil || !strings.Contains(err.Error(), "failed to acquire advisory lock: lock query failed") {
			t.Fatalf("expected lock query error, got %v", err)
		}
		if !conn.released {
			t.Fatalf("expected connection release on advisory lock query error")
		}
	})

	t.Run("advisory lock already held", func(t *testing.T) {
		migrator := NewMigrator(&DB{}, behaviorMigrations, "testdata/migrations")
		conn := &migrationStubConn{
			queryRowFn: func(context.Context, string, ...interface{}) pgx.Row {
				return migrationStubRow{values: []interface{}{false}}
			},
		}
		migrator.acquireConn = func(context.Context) (pooledMigrationConn, error) { return conn, nil }

		err := migrator.Migrate(context.Background())
		if err == nil || err.Error() != "another migration is in progress" {
			t.Fatalf("expected lock contention error, got %v", err)
		}
	})

	t.Run("ensure migrations table error", func(t *testing.T) {
		migrator := NewMigrator(&DB{}, behaviorMigrations, "testdata/migrations")
		conn := &migrationStubConn{
			queryRowFn: func(context.Context, string, ...interface{}) pgx.Row {
				return migrationStubRow{values: []interface{}{true}}
			},
			execFn: func(_ context.Context, sql string, _ ...interface{}) (pgconn.CommandTag, error) {
				if strings.Contains(sql, "CREATE TABLE IF NOT EXISTS schema_migrations") {
					return pgconn.CommandTag{}, errors.New("create table failed")
				}
				return pgconn.NewCommandTag("SELECT 1"), nil
			},
		}
		migrator.acquireConn = func(context.Context) (pooledMigrationConn, error) { return conn, nil }

		err := migrator.Migrate(context.Background())
		if err == nil || err.Error() != "create table failed" {
			t.Fatalf("expected create table failure, got %v", err)
		}
	})

	t.Run("load migrations error", func(t *testing.T) {
		migrator := NewMigrator(&DB{}, behaviorMigrations, "testdata/missing")
		conn := &migrationStubConn{
			queryRowFn: func(context.Context, string, ...interface{}) pgx.Row {
				return migrationStubRow{values: []interface{}{true}}
			},
		}
		migrator.acquireConn = func(context.Context) (pooledMigrationConn, error) { return conn, nil }

		err := migrator.Migrate(context.Background())
		if err == nil || !strings.Contains(err.Error(), "failed to load migrations") {
			t.Fatalf("expected load migrations error, got %v", err)
		}
	})

	t.Run("current version error", func(t *testing.T) {
		migrator := NewMigrator(&DB{}, behaviorMigrations, "testdata/migrations")
		queryCount := 0
		conn := &migrationStubConn{
			queryRowFn: func(context.Context, string, ...interface{}) pgx.Row {
				queryCount++
				if queryCount == 1 {
					return migrationStubRow{values: []interface{}{true}}
				}
				return migrationStubRow{err: errors.New("current version failed")}
			},
		}
		migrator.acquireConn = func(context.Context) (pooledMigrationConn, error) { return conn, nil }

		err := migrator.Migrate(context.Background())
		if err == nil || !strings.Contains(err.Error(), "failed to get current version: current version failed") {
			t.Fatalf("expected current version error, got %v", err)
		}
	})

	t.Run("apply migration error bubbles with context", func(t *testing.T) {
		migrator := NewMigrator(&DB{}, behaviorMigrations, "testdata/migrations")
		queryCount := 0
		conn := &migrationStubConn{
			queryRowFn: func(context.Context, string, ...interface{}) pgx.Row {
				queryCount++
				if queryCount == 1 {
					return migrationStubRow{values: []interface{}{true}} // advisory lock
				}
				return migrationStubRow{values: []interface{}{0}} // current version
			},
		}
		migrator.acquireConn = func(context.Context) (pooledMigrationConn, error) { return conn, nil }
		migrator.beginTx = func(context.Context, migrationBeginner) (migrationTx, error) {
			return nil, errors.New("begin failed")
		}

		err := migrator.Migrate(context.Background())
		if err == nil || !strings.Contains(err.Error(), "failed to apply migration") || !strings.Contains(err.Error(), "begin failed") {
			t.Fatalf("expected wrapped apply migration error, got %v", err)
		}
	})

	t.Run("success applies only newer migrations and unlocks", func(t *testing.T) {
		migrator := NewMigrator(&DB{}, behaviorMigrations, "testdata/migrations")
		queryCount := 0
		conn := &migrationStubConn{
			queryRowFn: func(context.Context, string, ...interface{}) pgx.Row {
				queryCount++
				if queryCount == 1 {
					return migrationStubRow{values: []interface{}{true}} // advisory lock
				}
				return migrationStubRow{values: []interface{}{1}} // current version
			},
			execFn: func(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("SELECT 1"), nil
			},
		}
		migrator.acquireConn = func(context.Context) (pooledMigrationConn, error) { return conn, nil }
		tx := &migrationStubTx{rollbackErr: pgx.ErrTxClosed}
		migrator.beginTx = func(context.Context, migrationBeginner) (migrationTx, error) {
			return tx, nil
		}

		if err := migrator.Migrate(context.Background()); err != nil {
			t.Fatalf("Migrate returned error: %v", err)
		}
		if !conn.released {
			t.Fatalf("expected connection release on success")
		}
		if !tx.committed {
			t.Fatalf("expected migration transaction commit")
		}
		if len(tx.execSQL) != 2 {
			t.Fatalf("expected exactly one migration applied (2 execs), got %d", len(tx.execSQL))
		}
		if got := tx.execArgs[1][0]; got != 2 {
			t.Fatalf("expected to record version 2, got %v", got)
		}
		unlocked := false
		for _, sql := range conn.execSQL {
			if strings.Contains(sql, "pg_advisory_unlock") {
				unlocked = true
				break
			}
		}
		if !unlocked {
			t.Fatalf("expected advisory unlock query to be executed")
		}
	})
}

func TestRollback_WithSeams(t *testing.T) {
	t.Run("acquire connection error", func(t *testing.T) {
		migrator := NewMigrator(&DB{}, behaviorMigrations, "testdata/migrations")
		migrator.acquireConn = func(context.Context) (pooledMigrationConn, error) {
			return nil, errors.New("acquire failed")
		}
		if err := migrator.Rollback(context.Background()); err == nil || err.Error() != "acquire failed" {
			t.Fatalf("expected acquire failed, got %v", err)
		}
	})

	t.Run("no migrations to rollback", func(t *testing.T) {
		migrator := NewMigrator(&DB{}, behaviorMigrations, "testdata/migrations")
		conn := &migrationStubConn{
			queryRowFn: func(context.Context, string, ...interface{}) pgx.Row {
				return migrationStubRow{err: pgx.ErrNoRows}
			},
		}
		migrator.acquireConn = func(context.Context) (pooledMigrationConn, error) { return conn, nil }

		err := migrator.Rollback(context.Background())
		if err == nil || err.Error() != "no migrations to rollback" {
			t.Fatalf("expected no migrations error, got %v", err)
		}
		if !conn.released {
			t.Fatalf("expected connection release on no migration case")
		}
	})

	t.Run("query current migration error", func(t *testing.T) {
		migrator := NewMigrator(&DB{}, behaviorMigrations, "testdata/migrations")
		conn := &migrationStubConn{
			queryRowFn: func(context.Context, string, ...interface{}) pgx.Row {
				return migrationStubRow{err: errors.New("query failed")}
			},
		}
		migrator.acquireConn = func(context.Context) (pooledMigrationConn, error) { return conn, nil }

		err := migrator.Rollback(context.Background())
		if err == nil || err.Error() != "query failed" {
			t.Fatalf("expected query failed error, got %v", err)
		}
	})

	t.Run("load migrations error", func(t *testing.T) {
		migrator := NewMigrator(&DB{}, behaviorMigrations, "testdata/missing")
		conn := &migrationStubConn{
			queryRowFn: func(context.Context, string, ...interface{}) pgx.Row {
				return migrationStubRow{values: []interface{}{2, "add_column"}}
			},
		}
		migrator.acquireConn = func(context.Context) (pooledMigrationConn, error) { return conn, nil }

		if err := migrator.Rollback(context.Background()); err == nil {
			t.Fatalf("expected load migrations error")
		}
	})

	t.Run("missing down migration file", func(t *testing.T) {
		migrator := NewMigrator(&DB{}, behaviorMigrations, "testdata/migrations")
		conn := &migrationStubConn{
			queryRowFn: func(context.Context, string, ...interface{}) pgx.Row {
				return migrationStubRow{values: []interface{}{99, "missing"}}
			},
		}
		migrator.acquireConn = func(context.Context) (pooledMigrationConn, error) { return conn, nil }

		err := migrator.Rollback(context.Background())
		if err == nil || err.Error() != "no down migration found for version 99" {
			t.Fatalf("expected missing down migration error, got %v", err)
		}
	})

	t.Run("begin transaction error", func(t *testing.T) {
		migrator := NewMigrator(&DB{}, behaviorMigrations, "testdata/migrations")
		conn := &migrationStubConn{
			queryRowFn: func(context.Context, string, ...interface{}) pgx.Row {
				return migrationStubRow{values: []interface{}{2, "add_column"}}
			},
		}
		migrator.acquireConn = func(context.Context) (pooledMigrationConn, error) { return conn, nil }
		migrator.beginTx = func(context.Context, migrationBeginner) (migrationTx, error) {
			return nil, errors.New("begin failed")
		}

		err := migrator.Rollback(context.Background())
		if err == nil || err.Error() != "begin failed" {
			t.Fatalf("expected begin failed, got %v", err)
		}
	})

	t.Run("down migration SQL error", func(t *testing.T) {
		migrator := NewMigrator(&DB{}, behaviorMigrations, "testdata/migrations")
		conn := &migrationStubConn{
			queryRowFn: func(context.Context, string, ...interface{}) pgx.Row {
				return migrationStubRow{values: []interface{}{2, "add_column"}}
			},
		}
		migrator.acquireConn = func(context.Context) (pooledMigrationConn, error) { return conn, nil }
		tx := &migrationStubTx{execErrOn: 1, execErr: errors.New("down failed")}
		migrator.beginTx = func(context.Context, migrationBeginner) (migrationTx, error) {
			return tx, nil
		}

		err := migrator.Rollback(context.Background())
		if err == nil || !strings.Contains(err.Error(), "rollback SQL failed: down failed") {
			t.Fatalf("expected down SQL error, got %v", err)
		}
		if !tx.rolledBack {
			t.Fatalf("expected rollback called when down SQL fails")
		}
	})

	t.Run("delete migration record error", func(t *testing.T) {
		migrator := NewMigrator(&DB{}, behaviorMigrations, "testdata/migrations")
		conn := &migrationStubConn{
			queryRowFn: func(context.Context, string, ...interface{}) pgx.Row {
				return migrationStubRow{values: []interface{}{2, "add_column"}}
			},
		}
		migrator.acquireConn = func(context.Context) (pooledMigrationConn, error) { return conn, nil }
		tx := &migrationStubTx{execErrOn: 2, execErr: errors.New("delete failed")}
		migrator.beginTx = func(context.Context, migrationBeginner) (migrationTx, error) {
			return tx, nil
		}

		err := migrator.Rollback(context.Background())
		if err == nil || !strings.Contains(err.Error(), "failed to remove migration record: delete failed") {
			t.Fatalf("expected delete record error, got %v", err)
		}
	})

	t.Run("commit error", func(t *testing.T) {
		migrator := NewMigrator(&DB{}, behaviorMigrations, "testdata/migrations")
		conn := &migrationStubConn{
			queryRowFn: func(context.Context, string, ...interface{}) pgx.Row {
				return migrationStubRow{values: []interface{}{2, "add_column"}}
			},
		}
		migrator.acquireConn = func(context.Context) (pooledMigrationConn, error) { return conn, nil }
		tx := &migrationStubTx{
			commitErr:   errors.New("commit failed"),
			rollbackErr: pgx.ErrTxClosed,
		}
		migrator.beginTx = func(context.Context, migrationBeginner) (migrationTx, error) {
			return tx, nil
		}

		err := migrator.Rollback(context.Background())
		if err == nil || err.Error() != "commit failed" {
			t.Fatalf("expected commit failed error, got %v", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		migrator := NewMigrator(&DB{}, behaviorMigrations, "testdata/migrations")
		conn := &migrationStubConn{
			queryRowFn: func(context.Context, string, ...interface{}) pgx.Row {
				return migrationStubRow{values: []interface{}{2, "add_column"}}
			},
		}
		migrator.acquireConn = func(context.Context) (pooledMigrationConn, error) { return conn, nil }
		tx := &migrationStubTx{rollbackErr: pgx.ErrTxClosed}
		migrator.beginTx = func(context.Context, migrationBeginner) (migrationTx, error) {
			return tx, nil
		}

		if err := migrator.Rollback(context.Background()); err != nil {
			t.Fatalf("Rollback returned error: %v", err)
		}
		if !tx.committed {
			t.Fatalf("expected commit on successful rollback migration")
		}
		if len(tx.execSQL) != 2 {
			t.Fatalf("expected two SQL statements in rollback tx, got %d", len(tx.execSQL))
		}
		if !strings.Contains(tx.execSQL[1], "DELETE FROM schema_migrations") {
			t.Fatalf("expected migration record delete SQL in second exec")
		}
		if !conn.released {
			t.Fatalf("expected connection release on rollback success")
		}
	})
}
