package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aegion/aegion/internal/platform/database"
)

func newUnreachablePool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	pool, err := pgxpool.New(context.Background(), "postgres://postgres:postgres@127.0.0.1:1/postgres?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

type adminTestRow struct {
	scanFn func(dest ...any) error
}

func (r adminTestRow) Scan(dest ...any) error {
	if r.scanFn != nil {
		return r.scanFn(dest...)
	}
	return nil
}

type adminTestTx struct {
	pgx.Tx
	execFn     func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	queryRowFn func(ctx context.Context, sql string, args ...any) pgx.Row
	commitFn   func(ctx context.Context) error
	rollbackFn func(ctx context.Context) error
}

func (tx *adminTestTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if tx.execFn != nil {
		return tx.execFn(ctx, sql, args...)
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (tx *adminTestTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if tx.queryRowFn != nil {
		return tx.queryRowFn(ctx, sql, args...)
	}
	return adminTestRow{}
}

func (tx *adminTestTx) Commit(ctx context.Context) error {
	if tx.commitFn != nil {
		return tx.commitFn(ctx)
	}
	return nil
}

func (tx *adminTestTx) Rollback(ctx context.Context) error {
	if tx.rollbackFn != nil {
		return tx.rollbackFn(ctx)
	}
	return pgx.ErrTxClosed
}

func TestAdminHandlers_DatabaseExecutionFailures(t *testing.T) {
	s := newTestServer(t)
	s.db = &database.DB{Pool: newUnreachablePool(t)}

	t.Run("list identities fails on db count query", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/identities?filter=demo&sort=email", nil)
		rec := httptest.NewRecorder()
		s.handleAdminListIdentities(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
	})

	t.Run("create identity fails while resolving schema", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/admin/identities", mustJSONBody(t, map[string]any{
			"traits": map[string]any{"email": "admin@example.com"},
			"state":  "active",
		}))
		rec := httptest.NewRecorder()
		s.handleAdminCreateIdentity(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}
	})

	t.Run("get identity fails on db query", func(t *testing.T) {
		req := withURLParam(httptest.NewRequest(http.MethodGet, "/admin/identities/x", nil), "id", uuid.NewString())
		rec := httptest.NewRecorder()
		s.handleAdminGetIdentity(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
	})

	t.Run("update identity fails during transaction begin", func(t *testing.T) {
		req := withURLParam(httptest.NewRequest(http.MethodPatch, "/admin/identities/x", mustJSONBody(t, map[string]any{
			"traits": map[string]any{"email": "new@example.com"},
		})), "id", uuid.NewString())
		rec := httptest.NewRecorder()
		s.handleAdminUpdateIdentity(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
	})

	t.Run("delete identity fails during transaction begin", func(t *testing.T) {
		req := withURLParam(httptest.NewRequest(http.MethodDelete, "/admin/identities/x", nil), "id", uuid.NewString())
		rec := httptest.NewRecorder()
		s.handleAdminDeleteIdentity(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
	})

	t.Run("list sessions fails on db count query", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/sessions?page=1&per_page=25", nil)
		rec := httptest.NewRecorder()
		s.handleAdminListSessions(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
	})

	t.Run("delete session fails on update exec", func(t *testing.T) {
		req := withURLParam(httptest.NewRequest(http.MethodDelete, "/admin/sessions/x", nil), "id", uuid.NewString())
		rec := httptest.NewRecorder()
		s.handleAdminDeleteSession(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
	})

	t.Run("delete identity sessions fails on update exec", func(t *testing.T) {
		req := withURLParam(httptest.NewRequest(http.MethodDelete, "/admin/identities/x/sessions", nil), "identityId", uuid.NewString())
		rec := httptest.NewRecorder()
		s.handleAdminDeleteIdentitySessions(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
	})
}

func TestUpsertPrimaryIdentityEmail_Branches(t *testing.T) {
	ctx := context.Background()
	identityID := uuid.New()

	t.Run("updates existing primary email", func(t *testing.T) {
		callCount := 0
		tx := &adminTestTx{
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				callCount++
				if !strings.Contains(sql, "UPDATE core_identity_addresses") {
					t.Fatalf("unexpected SQL: %s", sql)
				}
				return pgconn.NewCommandTag("UPDATE 1"), nil
			},
		}

		if err := upsertPrimaryIdentityEmail(ctx, tx, identityID, "admin@example.com"); err != nil {
			t.Fatalf("upsert failed: %v", err)
		}
		if callCount != 1 {
			t.Fatalf("expected one exec call, got %d", callCount)
		}
	})

	t.Run("inserts when no primary email exists", func(t *testing.T) {
		callCount := 0
		tx := &adminTestTx{
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				callCount++
				if callCount == 1 {
					return pgconn.NewCommandTag("UPDATE 0"), nil
				}
				if !strings.Contains(sql, "INSERT INTO core_identity_addresses") {
					t.Fatalf("unexpected SQL: %s", sql)
				}
				return pgconn.NewCommandTag("INSERT 0 1"), nil
			},
		}

		if err := upsertPrimaryIdentityEmail(ctx, tx, identityID, "admin@example.com"); err != nil {
			t.Fatalf("upsert failed: %v", err)
		}
		if callCount != 2 {
			t.Fatalf("expected two exec calls, got %d", callCount)
		}
	})

	t.Run("returns update and insert errors", func(t *testing.T) {
		tx := &adminTestTx{
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("update failed")
			},
		}
		if err := upsertPrimaryIdentityEmail(ctx, tx, identityID, "admin@example.com"); err == nil {
			t.Fatal("expected update error")
		}

		callCount := 0
		tx = &adminTestTx{
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				callCount++
				if callCount == 1 {
					return pgconn.NewCommandTag("UPDATE 0"), nil
				}
				return pgconn.CommandTag{}, errors.New("insert failed")
			},
		}
		if err := upsertPrimaryIdentityEmail(ctx, tx, identityID, "admin@example.com"); err == nil {
			t.Fatal("expected insert error")
		}
	})
}

func TestResolveBootstrapSchemaID_Branches(t *testing.T) {
	ctx := context.Background()

	t.Run("returns existing default schema", func(t *testing.T) {
		expected := uuid.New()
		tx := &adminTestTx{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return adminTestRow{scanFn: func(dest ...any) error {
					*(dest[0].(*uuid.UUID)) = expected
					return nil
				}}
			},
		}

		got, err := resolveBootstrapSchemaID(ctx, tx)
		if err != nil {
			t.Fatalf("resolveBootstrapSchemaID failed: %v", err)
		}
		if got != expected {
			t.Fatalf("expected %s, got %s", expected, got)
		}
	})

	t.Run("creates default schema on no rows", func(t *testing.T) {
		execCalls := 0
		tx := &adminTestTx{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return adminTestRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
			},
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				execCalls++
				return pgconn.NewCommandTag("INSERT 0 1"), nil
			},
		}

		got, err := resolveBootstrapSchemaID(ctx, tx)
		if err != nil {
			t.Fatalf("resolveBootstrapSchemaID failed: %v", err)
		}
		if got == uuid.Nil {
			t.Fatal("expected created schema UUID")
		}
		if execCalls != 1 {
			t.Fatalf("expected one insert call, got %d", execCalls)
		}
	})

	t.Run("returns query and insert errors", func(t *testing.T) {
		tx := &adminTestTx{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return adminTestRow{scanFn: func(dest ...any) error { return errors.New("query failed") }}
			},
		}
		if _, err := resolveBootstrapSchemaID(ctx, tx); err == nil {
			t.Fatal("expected query error")
		}

		tx = &adminTestTx{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return adminTestRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
			},
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("insert failed")
			},
		}
		if _, err := resolveBootstrapSchemaID(ctx, tx); err == nil {
			t.Fatal("expected insert error")
		}
	})
}

func TestModuleMigrationUtilityPaths(t *testing.T) {
	deps := defaultModuleMigrationDeps()
	if deps.moduleOrder == nil || deps.moduleFS == nil || deps.moduleMigrator == nil {
		t.Fatal("expected default migration deps to be fully wired")
	}

	if err := runEnabledModuleMigrations(context.Background(), nil, &database.DB{}, "configs\\aegion.yaml"); err != nil {
		t.Fatalf("expected nil cfg to no-op, got %v", err)
	}

	if _, err := resolveModuleFS(filepath.Join("..", "..", "configs", "aegion.yaml")); err != nil {
		t.Fatalf("expected module fs resolution from repo root to succeed, got %v", err)
	}

	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	tempDir := t.TempDir()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("failed to chdir temp dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origWd)
	})

	if _, err := resolveModuleFS(filepath.Join(tempDir, "configs", "missing.yaml")); err == nil {
		t.Fatal("expected missing modules directory error")
	}
}

func TestBootstrapAdminOperator_GuardAndBeginError(t *testing.T) {
	if _, err := bootstrapAdminOperator(context.Background(), nil, "admin@example.com", "Password1!"); err == nil {
		t.Fatal("expected nil db error")
	}

	if _, err := bootstrapAdminOperator(context.Background(), &database.DB{}, "admin@example.com", "Password1!"); err == nil {
		t.Fatal("expected nil pool error")
	}

	db := &database.DB{Pool: newUnreachablePool(t)}
	if _, err := bootstrapAdminOperator(context.Background(), db, "admin@example.com", "Password1!"); err == nil {
		t.Fatal("expected begin error with unreachable pool")
	}
}

func TestBootstrapAdminOperator_CreatesIdentityAndOperator(t *testing.T) {
	origBegin := beginBootstrapAdminTx
	t.Cleanup(func() { beginBootstrapAdminTx = origBegin })

	schemaID := uuid.New()
	var (
		identityInsertSeen bool
		operatorInsertSeen bool
	)

	tx := &adminTestTx{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			switch {
			case strings.Contains(sql, "SELECT COUNT(*) FROM adm_operators"):
				return adminTestRow{scanFn: func(dest ...any) error {
					*(dest[0].(*int)) = 0
					return nil
				}}
			case strings.Contains(sql, "FROM core_identities ci"):
				return adminTestRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
			case strings.Contains(sql, "FROM core_identity_schemas"):
				return adminTestRow{scanFn: func(dest ...any) error {
					*(dest[0].(*uuid.UUID)) = schemaID
					return nil
				}}
			default:
				t.Fatalf("unexpected query: %s", sql)
				return adminTestRow{scanFn: func(dest ...any) error { return errors.New("unexpected query") }}
			}
		},
		execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			switch {
			case strings.Contains(sql, "INSERT INTO core_identities"):
				identityInsertSeen = true
				if gotSchemaID, ok := args[1].(uuid.UUID); !ok || gotSchemaID != schemaID {
					t.Fatalf("expected schema id %s, got %#v", schemaID, args[1])
				}
				return pgconn.NewCommandTag("INSERT 0 1"), nil
			case strings.Contains(sql, "INSERT INTO core_identity_addresses"):
				return pgconn.NewCommandTag("INSERT 0 1"), nil
			case strings.Contains(sql, "INSERT INTO pwd_credentials"):
				return pgconn.NewCommandTag("INSERT 0 1"), nil
			case strings.Contains(sql, "INSERT INTO adm_operators"):
				operatorInsertSeen = true
				return pgconn.NewCommandTag("INSERT 0 1"), nil
			default:
				t.Fatalf("unexpected exec: %s", sql)
				return pgconn.CommandTag{}, errors.New("unexpected exec")
			}
		},
	}

	beginBootstrapAdminTx = func(ctx context.Context, db *database.DB) (pgx.Tx, error) {
		return tx, nil
	}

	outcome, err := bootstrapAdminOperator(context.Background(), &database.DB{Pool: &pgxpool.Pool{}}, "admin@example.com", "Password1!")
	if err != nil {
		t.Fatalf("bootstrapAdminOperator failed: %v", err)
	}
	if !identityInsertSeen || !operatorInsertSeen {
		t.Fatalf("expected identity and operator inserts to run, identity=%t operator=%t", identityInsertSeen, operatorInsertSeen)
	}
	if !outcome.CreatedIdentity || !outcome.CreatedOperator {
		t.Fatalf("expected created identity/operator flags true, got %+v", outcome)
	}
	if outcome.IdentityID == uuid.Nil || outcome.OperatorID == uuid.Nil {
		t.Fatalf("expected non-nil ids, got %+v", outcome)
	}
}

func TestBootstrapAdminOperator_ExistingIdentityAndCredentialBackfill(t *testing.T) {
	origBegin := beginBootstrapAdminTx
	t.Cleanup(func() { beginBootstrapAdminTx = origBegin })

	existingIdentityID := uuid.New()
	credentialInsertSeen := false
	operatorInsertSeen := false

	tx := &adminTestTx{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			switch {
			case strings.Contains(sql, "SELECT COUNT(*) FROM adm_operators"):
				return adminTestRow{scanFn: func(dest ...any) error {
					*(dest[0].(*int)) = 0
					return nil
				}}
			case strings.Contains(sql, "FROM core_identities ci"):
				return adminTestRow{scanFn: func(dest ...any) error {
					*(dest[0].(*uuid.UUID)) = existingIdentityID
					return nil
				}}
			case strings.Contains(sql, "SELECT COUNT(*) FROM pwd_credentials"):
				return adminTestRow{scanFn: func(dest ...any) error {
					*(dest[0].(*int)) = 0
					return nil
				}}
			default:
				t.Fatalf("unexpected query: %s", sql)
				return adminTestRow{scanFn: func(dest ...any) error { return errors.New("unexpected query") }}
			}
		},
		execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			switch {
			case strings.Contains(sql, "INSERT INTO pwd_credentials"):
				credentialInsertSeen = true
				return pgconn.NewCommandTag("INSERT 0 1"), nil
			case strings.Contains(sql, "INSERT INTO adm_operators"):
				operatorInsertSeen = true
				if gotIdentityID, ok := args[1].(uuid.UUID); !ok || gotIdentityID != existingIdentityID {
					t.Fatalf("expected existing identity id %s, got %#v", existingIdentityID, args[1])
				}
				return pgconn.NewCommandTag("INSERT 0 1"), nil
			default:
				t.Fatalf("unexpected exec: %s", sql)
				return pgconn.CommandTag{}, errors.New("unexpected exec")
			}
		},
	}

	beginBootstrapAdminTx = func(ctx context.Context, db *database.DB) (pgx.Tx, error) {
		return tx, nil
	}

	outcome, err := bootstrapAdminOperator(context.Background(), &database.DB{Pool: &pgxpool.Pool{}}, "admin@example.com", "Password1!")
	if err != nil {
		t.Fatalf("bootstrapAdminOperator failed: %v", err)
	}
	if outcome.CreatedIdentity {
		t.Fatalf("expected existing identity path, got %+v", outcome)
	}
	if !credentialInsertSeen || !operatorInsertSeen || !outcome.CreatedOperator {
		t.Fatalf("expected credential/operator inserts, credential=%t operator=%t outcome=%+v", credentialInsertSeen, operatorInsertSeen, outcome)
	}
	if outcome.IdentityID != existingIdentityID {
		t.Fatalf("expected identity id %s, got %s", existingIdentityID, outcome.IdentityID)
	}
}

func TestBootstrapAdminOperator_ExistingOperatorAndFailures(t *testing.T) {
	origBegin := beginBootstrapAdminTx
	t.Cleanup(func() { beginBootstrapAdminTx = origBegin })

	t.Run("returns early when operator already exists", func(t *testing.T) {
		execCalls := 0
		tx := &adminTestTx{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return adminTestRow{scanFn: func(dest ...any) error {
					*(dest[0].(*int)) = 1
					return nil
				}}
			},
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				execCalls++
				return pgconn.NewCommandTag("INSERT 0 1"), nil
			},
		}
		beginBootstrapAdminTx = func(ctx context.Context, db *database.DB) (pgx.Tx, error) {
			return tx, nil
		}

		outcome, err := bootstrapAdminOperator(context.Background(), &database.DB{Pool: &pgxpool.Pool{}}, "admin@example.com", "Password1!")
		if err != nil {
			t.Fatalf("bootstrapAdminOperator failed: %v", err)
		}
		if outcome.CreatedOperator || outcome.CreatedIdentity {
			t.Fatalf("expected no-op outcome when operator exists, got %+v", outcome)
		}
		if execCalls != 0 {
			t.Fatalf("expected no exec calls, got %d", execCalls)
		}
	})

	t.Run("returns operator insert and commit errors", func(t *testing.T) {
		existingIdentityID := uuid.New()
		baseQuery := func(ctx context.Context, sql string, args ...any) pgx.Row {
			switch {
			case strings.Contains(sql, "SELECT COUNT(*) FROM adm_operators"):
				return adminTestRow{scanFn: func(dest ...any) error {
					*(dest[0].(*int)) = 0
					return nil
				}}
			case strings.Contains(sql, "FROM core_identities ci"):
				return adminTestRow{scanFn: func(dest ...any) error {
					*(dest[0].(*uuid.UUID)) = existingIdentityID
					return nil
				}}
			case strings.Contains(sql, "SELECT COUNT(*) FROM pwd_credentials"):
				return adminTestRow{scanFn: func(dest ...any) error {
					*(dest[0].(*int)) = 1
					return nil
				}}
			default:
				return adminTestRow{scanFn: func(dest ...any) error { return errors.New("unexpected query") }}
			}
		}

		tx := &adminTestTx{
			queryRowFn: baseQuery,
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				if strings.Contains(sql, "INSERT INTO adm_operators") {
					return pgconn.CommandTag{}, errors.New("operator insert failed")
				}
				return pgconn.NewCommandTag("INSERT 0 1"), nil
			},
		}
		beginBootstrapAdminTx = func(ctx context.Context, db *database.DB) (pgx.Tx, error) {
			return tx, nil
		}

		if _, err := bootstrapAdminOperator(context.Background(), &database.DB{Pool: &pgxpool.Pool{}}, "admin@example.com", "Password1!"); err == nil {
			t.Fatal("expected operator insert error")
		}

		tx = &adminTestTx{
			queryRowFn: baseQuery,
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("INSERT 0 1"), nil
			},
			commitFn: func(ctx context.Context) error { return errors.New("commit failed") },
		}
		beginBootstrapAdminTx = func(ctx context.Context, db *database.DB) (pgx.Tx, error) {
			return tx, nil
		}

		if _, err := bootstrapAdminOperator(context.Background(), &database.DB{Pool: &pgxpool.Pool{}}, "admin@example.com", "Password1!"); err == nil {
			t.Fatal("expected commit error")
		}
	})
}
