package database

import (
	"context"
	"embed"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

//go:embed testdata/migrations_invalid/*
var invalidMigrationFS embed.FS

type errBeginner struct {
	err error
}

func (b errBeginner) Begin(context.Context) (pgx.Tx, error) {
	return nil, b.err
}

func TestLoadMigrations_SkipsInvalidFilenames(t *testing.T) {
	migrator := NewMigrator(&DB{}, invalidMigrationFS, "testdata/migrations_invalid")

	migrations, err := migrator.loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations failed: %v", err)
	}
	if len(migrations) != 1 {
		t.Fatalf("expected exactly one valid migration pair, got %d", len(migrations))
	}
	if migrations[0].Version != 2 || migrations[0].Name != "valid" {
		t.Fatalf("unexpected migration parsed: %+v", migrations[0])
	}
}

func TestNewMigrator_DefaultBeginTxClosureError(t *testing.T) {
	migrator := NewMigrator(&DB{}, invalidMigrationFS, "testdata/migrations_invalid")

	_, err := migrator.beginTx(context.Background(), errBeginner{err: errors.New("begin failed")})
	if err == nil || err.Error() != "begin failed" {
		t.Fatalf("expected default beginTx closure to return begin error, got %v", err)
	}
}

func TestApplyMigration_IgnoresUnexpectedRollbackError(t *testing.T) {
	migrator := NewMigrator(&DB{}, behaviorMigrations, "testdata/migrations")
	tx := &migrationStubTx{rollbackErr: errors.New("rollback failed")}
	migrator.beginTx = func(context.Context, migrationBeginner) (migrationTx, error) {
		return tx, nil
	}

	err := migrator.applyMigration(context.Background(), &migrationStubConn{}, Migration{
		Version: 99,
		Name:    "coverage",
		UpSQL:   "SELECT 1",
	})
	if err != nil {
		t.Fatalf("applyMigration failed: %v", err)
	}
	if !tx.committed {
		t.Fatalf("expected migration transaction to commit")
	}
}

func TestRollback_IgnoresUnexpectedRollbackError(t *testing.T) {
	migrator := NewMigrator(&DB{}, behaviorMigrations, "testdata/migrations")
	conn := &migrationStubConn{
		queryRowFn: func(context.Context, string, ...interface{}) pgx.Row {
			return migrationStubRow{values: []interface{}{2, "add_column"}}
		},
	}
	migrator.acquireConn = func(context.Context) (pooledMigrationConn, error) { return conn, nil }

	tx := &migrationStubTx{rollbackErr: errors.New("rollback failed")}
	migrator.beginTx = func(context.Context, migrationBeginner) (migrationTx, error) {
		return tx, nil
	}

	if err := migrator.Rollback(context.Background()); err != nil {
		t.Fatalf("Rollback should ignore defer rollback error, got %v", err)
	}
}

func TestMigrate_IgnoresUnlockError(t *testing.T) {
	migrator := NewMigrator(&DB{}, behaviorMigrations, "testdata/migrations")
	queryCount := 0
	conn := &migrationStubConn{
		queryRowFn: func(context.Context, string, ...interface{}) pgx.Row {
			queryCount++
			if queryCount == 1 {
				return migrationStubRow{values: []interface{}{true}} // advisory lock acquired
			}
			return migrationStubRow{values: []interface{}{2}} // current version (no pending migrations)
		},
		execFn: func(_ context.Context, sql string, _ ...interface{}) (pgconn.CommandTag, error) {
			if strings.Contains(sql, "pg_advisory_unlock") {
				return pgconn.CommandTag{}, errors.New("unlock failed")
			}
			return pgconn.NewCommandTag("SELECT 1"), nil
		},
	}
	migrator.acquireConn = func(context.Context) (pooledMigrationConn, error) { return conn, nil }

	if err := migrator.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate should ignore unlock errors, got %v", err)
	}
}
