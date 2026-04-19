package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type txRow struct {
	vals []any
	err  error
}

func (r txRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.vals) {
		return errors.New("scan destination mismatch")
	}
	for i := range dest {
		switch d := dest[i].(type) {
		case *uuid.UUID:
			v, ok := r.vals[i].(uuid.UUID)
			if !ok {
				return errors.New("expected uuid value")
			}
			*d = v
		case *string:
			v, ok := r.vals[i].(string)
			if !ok {
				return errors.New("expected string value")
			}
			*d = v
		case *time.Time:
			v, ok := r.vals[i].(time.Time)
			if !ok {
				return errors.New("expected time value")
			}
			*d = v
		case *bool:
			v, ok := r.vals[i].(bool)
			if !ok {
				return errors.New("expected bool value")
			}
			*d = v
		default:
			return errors.New("unsupported scan destination")
		}
	}
	return nil
}

type txStub struct {
	queryRowFn func(context.Context, string, ...any) pgx.Row
	execFn     func(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func (t *txStub) Begin(context.Context) (pgx.Tx, error) { return nil, errors.New("not implemented") }
func (t *txStub) Commit(context.Context) error          { return nil }
func (t *txStub) Rollback(context.Context) error        { return nil }
func (t *txStub) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, errors.New("not implemented")
}
func (t *txStub) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults { return nil }
func (t *txStub) LargeObjects() pgx.LargeObjects                         { return pgx.LargeObjects{} }
func (t *txStub) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, errors.New("not implemented")
}
func (t *txStub) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("not implemented")
}
func (t *txStub) Conn() *pgx.Conn { return nil }

func (t *txStub) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if t.execFn != nil {
		return t.execFn(ctx, sql, args...)
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (t *txStub) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if t.queryRowFn != nil {
		return t.queryRowFn(ctx, sql, args...)
	}
	return txRow{err: pgx.ErrNoRows}
}

func TestPostgresInternalIdentityByLink(t *testing.T) {
	s := &PostgresStore{}
	wantID := uuid.New()
	boom := errors.New("boom")

	tx := &txStub{queryRowFn: func(context.Context, string, ...any) pgx.Row {
		return txRow{vals: []any{wantID}}
	}}
	gotID, ok, err := s.identityByLink(context.Background(), tx, "google", "sub-1")
	if err != nil || !ok || gotID != wantID {
		t.Fatalf("identityByLink(success) id=%s ok=%v err=%v", gotID, ok, err)
	}

	tx = &txStub{queryRowFn: func(context.Context, string, ...any) pgx.Row {
		return txRow{err: pgx.ErrNoRows}
	}}
	gotID, ok, err = s.identityByLink(context.Background(), tx, "google", "missing")
	if err != nil || ok || gotID != uuid.Nil {
		t.Fatalf("identityByLink(missing) id=%s ok=%v err=%v", gotID, ok, err)
	}

	tx = &txStub{queryRowFn: func(context.Context, string, ...any) pgx.Row {
		return txRow{err: boom}
	}}
	if _, _, err := s.identityByLink(context.Background(), tx, "google", "err"); !errors.Is(err, boom) {
		t.Fatalf("identityByLink(error) = %v", err)
	}
}

func TestPostgresInternalUpsertPrimaryEmail(t *testing.T) {
	s := &PostgresStore{}
	identityID := uuid.New()
	boom := errors.New("boom")

	tx := &txStub{}
	if err := s.upsertPrimaryEmail(context.Background(), tx, identityID, "   ", true); err != nil {
		t.Fatalf("upsertPrimaryEmail(empty) error = %v", err)
	}

	tx = &txStub{execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, boom
	}}
	if err := s.upsertPrimaryEmail(context.Background(), tx, identityID, "user@example.com", true); !errors.Is(err, boom) {
		t.Fatalf("upsertPrimaryEmail(update error) = %v", err)
	}

	call := 0
	tx = &txStub{execFn: func(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
		call++
		if call == 1 {
			return pgconn.NewCommandTag("UPDATE 1"), nil
		}
		return pgconn.NewCommandTag("INSERT 1"), nil
	}}
	if err := s.upsertPrimaryEmail(context.Background(), tx, identityID, "user@example.com", true); err != nil {
		t.Fatalf("upsertPrimaryEmail(update hit) error = %v", err)
	}
	if call != 1 {
		t.Fatalf("expected single update call when row exists, got %d", call)
	}

	call = 0
	tx = &txStub{execFn: func(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
		call++
		if call == 1 {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}
		return pgconn.NewCommandTag("INSERT 1"), nil
	}}
	if err := s.upsertPrimaryEmail(context.Background(), tx, identityID, "user@example.com", false); err != nil {
		t.Fatalf("upsertPrimaryEmail(insert fallback) error = %v", err)
	}
	if call != 2 {
		t.Fatalf("expected update+insert call sequence when row missing, got %d", call)
	}
}

func TestPostgresInternalRefreshLink(t *testing.T) {
	s := &PostgresStore{}
	boom := errors.New("boom")
	provider := Provider{Slug: "google"}
	profile := SocialProfile{ProviderUser: "sub-1", Email: "user@example.com", EmailVerified: true}
	identityID := uuid.New()
	now := time.Now().UTC()
	rawClaims := []byte(`{"sub":"sub-1"}`)

	tx := &txStub{execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
		return pgconn.NewCommandTag("INSERT 1"), nil
	}}
	if err := s.refreshLink(context.Background(), tx, provider, identityID, profile, rawClaims, now); err != nil {
		t.Fatalf("refreshLink(success) error = %v", err)
	}

	tx = &txStub{execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, boom
	}}
	if err := s.refreshLink(context.Background(), tx, provider, identityID, profile, rawClaims, now); !errors.Is(err, boom) {
		t.Fatalf("refreshLink(error) = %v", err)
	}
}

func TestPostgresInternalLookupOrCreateIdentity(t *testing.T) {
	s := &PostgresStore{}
	boom := errors.New("boom")
	provider := Provider{Slug: "google", TrustEmailVerified: true}
	profile := SocialProfile{ProviderUser: "sub-1", Email: "user@example.com", EmailVerified: true, Name: "User"}

	t.Run("existing identity by email", func(t *testing.T) {
		existingID := uuid.New()
		execCalls := 0
		tx := &txStub{
			queryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
				if strings.Contains(sql, "FROM core_identities i") {
					return txRow{vals: []any{existingID}}
				}
				return txRow{err: errors.New("unexpected query")}
			},
			execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
				execCalls++
				return pgconn.NewCommandTag("UPDATE 1"), nil
			},
		}
		gotID, created, err := s.lookupOrCreateIdentity(context.Background(), tx, provider, profile)
		if err != nil || created || gotID != existingID {
			t.Fatalf("lookupOrCreateIdentity(existing) id=%s created=%v err=%v", gotID, created, err)
		}
		if execCalls != 1 {
			t.Fatalf("expected one upsert email call for existing identity, got %d", execCalls)
		}
	})

	t.Run("email query non-no-rows error", func(t *testing.T) {
		tx := &txStub{queryRowFn: func(context.Context, string, ...any) pgx.Row {
			return txRow{err: boom}
		}}
		if _, _, err := s.lookupOrCreateIdentity(context.Background(), tx, provider, profile); !errors.Is(err, boom) {
			t.Fatalf("lookupOrCreateIdentity(email query error) = %v", err)
		}
	})

	t.Run("schema query error", func(t *testing.T) {
		tx := &txStub{
			queryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
				if strings.Contains(sql, "FROM core_identities i") {
					return txRow{err: pgx.ErrNoRows}
				}
				return txRow{err: boom}
			},
		}
		if _, _, err := s.lookupOrCreateIdentity(context.Background(), tx, provider, profile); !errors.Is(err, boom) {
			t.Fatalf("lookupOrCreateIdentity(schema query error) = %v", err)
		}
	})

	t.Run("insert identity exec error", func(t *testing.T) {
		schemaID := uuid.New()
		tx := &txStub{
			queryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
				if strings.Contains(sql, "FROM core_identities i") {
					return txRow{err: pgx.ErrNoRows}
				}
				if strings.Contains(sql, "FROM core_identity_schemas") {
					return txRow{vals: []any{schemaID}}
				}
				return txRow{err: errors.New("unexpected query")}
			},
			execFn: func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
				if strings.Contains(sql, "INSERT INTO core_identities") {
					return pgconn.CommandTag{}, boom
				}
				return pgconn.NewCommandTag("UPDATE 1"), nil
			},
		}
		if _, _, err := s.lookupOrCreateIdentity(context.Background(), tx, provider, profile); !errors.Is(err, boom) {
			t.Fatalf("lookupOrCreateIdentity(insert identity error) = %v", err)
		}
	})

	t.Run("new identity create success with primary email insert", func(t *testing.T) {
		schemaID := uuid.New()
		execCalls := 0
		tx := &txStub{
			queryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
				if strings.Contains(sql, "FROM core_identities i") {
					return txRow{err: pgx.ErrNoRows}
				}
				if strings.Contains(sql, "FROM core_identity_schemas") {
					return txRow{vals: []any{schemaID}}
				}
				return txRow{err: errors.New("unexpected query")}
			},
			execFn: func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
				execCalls++
				switch {
				case strings.Contains(sql, "INSERT INTO core_identities"):
					return pgconn.NewCommandTag("INSERT 1"), nil
				case strings.Contains(sql, "UPDATE core_identity_addresses"):
					return pgconn.NewCommandTag("UPDATE 0"), nil
				case strings.Contains(sql, "INSERT INTO core_identity_addresses"):
					return pgconn.NewCommandTag("INSERT 1"), nil
				default:
					return pgconn.NewCommandTag("UPDATE 1"), nil
				}
			},
		}
		gotID, created, err := s.lookupOrCreateIdentity(context.Background(), tx, provider, profile)
		if err != nil || !created || gotID == uuid.Nil {
			t.Fatalf("lookupOrCreateIdentity(create success) id=%s created=%v err=%v", gotID, created, err)
		}
		if execCalls < 3 {
			t.Fatalf("expected identity insert and email upsert calls, got %d", execCalls)
		}
	})
}
