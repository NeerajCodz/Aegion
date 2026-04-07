package main

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type migrationTestRow struct {
	scanFn func(dest ...any) error
}

func (r migrationTestRow) Scan(dest ...any) error {
	if r.scanFn != nil {
		return r.scanFn(dest...)
	}
	return nil
}

type migrationTestRows struct {
	scanFns []func(dest ...any) error
	index   int
	err     error
}

func (r *migrationTestRows) Close() {}

func (r *migrationTestRows) Err() error {
	return r.err
}

func (r *migrationTestRows) CommandTag() pgconn.CommandTag {
	return pgconn.CommandTag{}
}

func (r *migrationTestRows) FieldDescriptions() []pgconn.FieldDescription {
	return nil
}

func (r *migrationTestRows) Next() bool {
	return r.index < len(r.scanFns)
}

func (r *migrationTestRows) Scan(dest ...any) error {
	if r.index >= len(r.scanFns) {
		return errors.New("no rows")
	}
	fn := r.scanFns[r.index]
	r.index++
	if fn != nil {
		return fn(dest...)
	}
	return nil
}

func (r *migrationTestRows) Values() ([]any, error) {
	return nil, nil
}

func (r *migrationTestRows) RawValues() [][]byte {
	return nil
}

func (r *migrationTestRows) Conn() *pgx.Conn {
	return nil
}

type migrationTestTx struct {
	pgx.Tx
	execFn     func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	commitFn   func(ctx context.Context) error
	rollbackFn func(ctx context.Context) error
}

func (tx *migrationTestTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if tx.execFn != nil {
		return tx.execFn(ctx, sql, args...)
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func (tx *migrationTestTx) Commit(ctx context.Context) error {
	if tx.commitFn != nil {
		return tx.commitFn(ctx)
	}
	return nil
}

func (tx *migrationTestTx) Rollback(ctx context.Context) error {
	if tx.rollbackFn != nil {
		return tx.rollbackFn(ctx)
	}
	return pgx.ErrTxClosed
}

type migrationTestDB struct {
	queryRowFn func(ctx context.Context, sql string, args ...any) pgx.Row
	execFn     func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	queryFn    func(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	beginFn    func(ctx context.Context) (pgx.Tx, error)
}

type readErrMigrationFS struct {
	fstest.MapFS
	err error
}

func (f readErrMigrationFS) ReadFile(name string) ([]byte, error) {
	return nil, f.err
}

func (db *migrationTestDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if db.queryRowFn != nil {
		return db.queryRowFn(ctx, sql, args...)
	}
	return migrationTestRow{}
}

func (db *migrationTestDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if db.execFn != nil {
		return db.execFn(ctx, sql, args...)
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (db *migrationTestDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if db.queryFn != nil {
		return db.queryFn(ctx, sql, args...)
	}
	return &migrationTestRows{}, nil
}

func (db *migrationTestDB) Begin(ctx context.Context) (pgx.Tx, error) {
	if db.beginFn != nil {
		return db.beginFn(ctx)
	}
	return &migrationTestTx{}, nil
}

func TestRunMigrationsWithFS_AppliesPendingMigrations(t *testing.T) {
	fsys := fstest.MapFS{
		"migrations/0001_first.up.sql":  {Data: []byte("SELECT 1;")},
		"migrations/0002_second.up.sql": {Data: []byte("SELECT 2;")},
	}

	beginCalls := 0
	appliedSQL := []string{}
	db := &migrationTestDB{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			if strings.Contains(sql, "pg_try_advisory_lock") {
				return migrationTestRow{scanFn: func(dest ...any) error {
					*(dest[0].(*bool)) = true
					return nil
				}}
			}
			return migrationTestRow{}
		},
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			// Migration version 1 already applied, so only 2 should execute.
			return &migrationTestRows{
				scanFns: []func(dest ...any) error{
					func(dest ...any) error {
						*(dest[0].(*int)) = 1
						return nil
					},
				},
			}, nil
		},
		beginFn: func(ctx context.Context) (pgx.Tx, error) {
			beginCalls++
			return &migrationTestTx{
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					appliedSQL = append(appliedSQL, sql)
					return pgconn.NewCommandTag("INSERT 0 1"), nil
				},
			}, nil
		},
	}

	if err := runMigrationsWithFS(context.Background(), db, fsys); err != nil {
		t.Fatalf("runMigrationsWithFS failed: %v", err)
	}
	if beginCalls != 1 {
		t.Fatalf("expected one migration transaction, got %d", beginCalls)
	}
	if len(appliedSQL) < 1 || !strings.Contains(appliedSQL[0], "SELECT 2") {
		t.Fatalf("expected second migration sql to execute, got %v", appliedSQL)
	}
}

func TestRunMigrationsWithFS_ErrorPaths(t *testing.T) {
	fsys := fstest.MapFS{
		"migrations/0001_first.up.sql": {Data: []byte("SELECT 1;")},
	}

	t.Run("advisory lock query failure", func(t *testing.T) {
		db := &migrationTestDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return migrationTestRow{scanFn: func(dest ...any) error { return errors.New("lock query failed") }}
			},
		}
		if err := runMigrationsWithFS(context.Background(), db, fsys); err == nil || !strings.Contains(err.Error(), "failed to acquire admin migration lock") {
			t.Fatalf("expected lock query error, got %v", err)
		}
	})

	t.Run("lock not acquired", func(t *testing.T) {
		db := &migrationTestDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return migrationTestRow{scanFn: func(dest ...any) error {
					*(dest[0].(*bool)) = false
					return nil
				}}
			},
		}
		if err := runMigrationsWithFS(context.Background(), db, fsys); err == nil || !strings.Contains(err.Error(), "another admin migration is in progress") {
			t.Fatalf("expected lock contention error, got %v", err)
		}
	})

	t.Run("ensure migration table fails", func(t *testing.T) {
		db := &migrationTestDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return migrationTestRow{scanFn: func(dest ...any) error {
					*(dest[0].(*bool)) = true
					return nil
				}}
			},
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				if strings.Contains(sql, "CREATE TABLE IF NOT EXISTS adm_schema_migrations") {
					return pgconn.CommandTag{}, errors.New("create table failed")
				}
				return pgconn.NewCommandTag("UPDATE 1"), nil
			},
		}
		if err := runMigrationsWithFS(context.Background(), db, fsys); err == nil || !strings.Contains(err.Error(), "failed to ensure admin migration table") {
			t.Fatalf("expected ensure table error, got %v", err)
		}
	})

	t.Run("load applied migrations fails", func(t *testing.T) {
		db := &migrationTestDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return migrationTestRow{scanFn: func(dest ...any) error {
					*(dest[0].(*bool)) = true
					return nil
				}}
			},
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return nil, errors.New("query failed")
			},
		}
		if err := runMigrationsWithFS(context.Background(), db, fsys); err == nil || !strings.Contains(err.Error(), "failed to load applied admin migrations") {
			t.Fatalf("expected load applied error, got %v", err)
		}
	})

	t.Run("apply migration begin failure", func(t *testing.T) {
		db := &migrationTestDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return migrationTestRow{scanFn: func(dest ...any) error {
					*(dest[0].(*bool)) = true
					return nil
				}}
			},
			beginFn: func(ctx context.Context) (pgx.Tx, error) {
				return nil, errors.New("begin failed")
			},
		}
		if err := runMigrationsWithFS(context.Background(), db, fsys); err == nil || !strings.Contains(err.Error(), "begin failed") {
			t.Fatalf("expected begin failure, got %v", err)
		}
	})
}

func TestMigrationHelperFunctions(t *testing.T) {
	ctx := context.Background()

	t.Run("load applied migrations scan and rows errors", func(t *testing.T) {
		db := &migrationTestDB{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &migrationTestRows{
					scanFns: []func(dest ...any) error{
						func(dest ...any) error { return errors.New("scan failed") },
					},
				}, nil
			},
		}
		if _, err := loadAppliedAdminMigrations(ctx, db); err == nil || !strings.Contains(err.Error(), "scan failed") {
			t.Fatalf("expected scan error, got %v", err)
		}

		db.queryFn = func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &migrationTestRows{err: errors.New("rows error")}, nil
		}
		if _, err := loadAppliedAdminMigrations(ctx, db); err == nil || !strings.Contains(err.Error(), "rows error") {
			t.Fatalf("expected rows error, got %v", err)
		}
	})

	t.Run("apply migration errors", func(t *testing.T) {
		m := adminMigration{Version: 42, Name: "answer", UpSQL: "SELECT 42;"}

		db := &migrationTestDB{
			beginFn: func(ctx context.Context) (pgx.Tx, error) {
				return &migrationTestTx{
					execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
						return pgconn.CommandTag{}, errors.New("exec up failed")
					},
				}, nil
			},
		}
		if err := applyAdminMigration(ctx, db, m); err == nil || !strings.Contains(err.Error(), "failed to apply admin migration") {
			t.Fatalf("expected apply migration exec error, got %v", err)
		}

		callCount := 0
		db.beginFn = func(ctx context.Context) (pgx.Tx, error) {
			return &migrationTestTx{
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					callCount++
					if callCount == 1 {
						return pgconn.NewCommandTag("UPDATE 1"), nil
					}
					return pgconn.CommandTag{}, errors.New("insert record failed")
				},
			}, nil
		}
		if err := applyAdminMigration(ctx, db, m); err == nil || !strings.Contains(err.Error(), "failed to record admin migration") {
			t.Fatalf("expected record migration error, got %v", err)
		}

		db.beginFn = func(ctx context.Context) (pgx.Tx, error) {
			return &migrationTestTx{
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					return pgconn.NewCommandTag("UPDATE 1"), nil
				},
				commitFn: func(ctx context.Context) error { return errors.New("commit failed") },
			}, nil
		}
		if err := applyAdminMigration(ctx, db, m); err == nil || !strings.Contains(err.Error(), "commit failed") {
			t.Fatalf("expected commit failure, got %v", err)
		}
	})
}

func TestRun_FlagParseErrorBranch(t *testing.T) {
	deps, _, _ := baseRunDeps(baseRunConfig())
	if err := run([]string{"-unknown"}, deps); err == nil || !strings.Contains(err.Error(), "failed to parse flags") {
		t.Fatalf("expected flag parse error, got %v", err)
	}
}

func TestDefaultMainDeps_DBClosures(t *testing.T) {
	deps := defaultMainDeps()

	cfg, err := pgxpool.ParseConfig("postgres://admin:admin@127.0.0.1:1/aegion?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatalf("parse config failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("new pool failed: %v", err)
	}
	if err := deps.pingDB(ctx, pool); err == nil {
		t.Fatalf("expected ping error against unreachable endpoint")
	}
	deps.closeDB(pool)
}

func TestLoadConfig_SuperConfigParseError(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "aegion.yaml")
	content := strings.TrimSpace(`
module_versions:
  admin: latest
database: []
`)
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := loadConfig(cfgPath); err == nil || !strings.Contains(err.Error(), "failed to parse config") {
		t.Fatalf("expected superconfig parse error, got %v", err)
	}
}

func TestIsAegionSuperConfig_KeyBranches(t *testing.T) {
	cases := []string{
		"secrets:\n  internal: [a]\n",
		"sessions:\n  lifespan: 1h\n",
		"password:\n  enabled: true\n",
		"magic_link:\n  enabled: true\n",
	}
	for _, input := range cases {
		if !isAegionSuperConfig([]byte(input)) {
			t.Fatalf("expected superconfig detection for input: %q", input)
		}
	}
}

func TestMigrationLoaderAndRunnerAdditionalBranches(t *testing.T) {
	t.Run("nil interface db rejected", func(t *testing.T) {
		if err := runMigrationsWithFS(context.Background(), nil, fstest.MapFS{}); err == nil || !strings.Contains(err.Error(), "database pool is nil") {
			t.Fatalf("expected nil db error, got %v", err)
		}
	})

	t.Run("load migrations failure wrapped", func(t *testing.T) {
		db := &migrationTestDB{}
		if err := runMigrationsWithFS(context.Background(), db, fstest.MapFS{}); err == nil || !strings.Contains(err.Error(), "failed to load admin migrations") {
			t.Fatalf("expected load migrations failure, got %v", err)
		}
	})

	t.Run("no migrations returns nil", func(t *testing.T) {
		db := &migrationTestDB{}
		fsys := fstest.MapFS{
			"migrations": {Mode: fs.ModeDir},
		}
		if err := runMigrationsWithFS(context.Background(), db, fsys); err != nil {
			t.Fatalf("expected nil when no migrations exist, got %v", err)
		}
	})

	t.Run("directory entries are ignored", func(t *testing.T) {
		fsys := fstest.MapFS{
			"migrations":                  {Mode: fs.ModeDir},
			"migrations/subdir":           {Mode: fs.ModeDir},
			"migrations/0001_init.up.sql": {Data: []byte("SELECT 1;")},
		}
		migrations, err := loadAdminMigrations(fsys)
		if err != nil {
			t.Fatalf("loadAdminMigrations failed: %v", err)
		}
		if len(migrations) != 1 || migrations[0].Version != 1 {
			t.Fatalf("expected one migration version 1, got %+v", migrations)
		}
	})

	t.Run("invalid version parse failure", func(t *testing.T) {
		fsys := fstest.MapFS{
			"migrations/abcd_bad.up.sql": {Data: []byte("SELECT 1;")},
		}
		if _, err := loadAdminMigrations(fsys); err == nil || !strings.Contains(err.Error(), "invalid migration version") {
			t.Fatalf("expected invalid version error, got %v", err)
		}
	})

	t.Run("read migration file failure", func(t *testing.T) {
		fsys := readErrMigrationFS{
			MapFS: fstest.MapFS{
				"migrations/0001_init.up.sql": {Data: []byte("SELECT 1;")},
			},
			err: errors.New("read failed"),
		}
		if _, err := loadAdminMigrations(fsys); err == nil || !strings.Contains(err.Error(), "failed to read migration") {
			t.Fatalf("expected read migration error, got %v", err)
		}
	})
}

func TestApplyAdminMigration_RollbackErrorIgnored(t *testing.T) {
	db := &migrationTestDB{
		beginFn: func(ctx context.Context) (pgx.Tx, error) {
			return &migrationTestTx{
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					return pgconn.NewCommandTag("UPDATE 1"), nil
				},
				rollbackFn: func(ctx context.Context) error {
					return errors.New("rollback failed")
				},
			}, nil
		},
	}

	err := applyAdminMigration(context.Background(), db, adminMigration{
		Version: 1,
		Name:    "init",
		UpSQL:   "SELECT 1;",
	})
	if err != nil {
		t.Fatalf("expected rollback error to be ignored after commit, got %v", err)
	}
}
