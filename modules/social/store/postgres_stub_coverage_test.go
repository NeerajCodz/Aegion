package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type socialFakeDB struct {
	queryRowFn func(context.Context, string, ...any) pgx.Row
	queryFn    func(context.Context, string, ...any) (pgx.Rows, error)
	execFn     func(context.Context, string, ...any) (pgconn.CommandTag, error)
	beginFn    func(context.Context) (pgx.Tx, error)
}

func (f *socialFakeDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if f.queryRowFn != nil {
		return f.queryRowFn(ctx, sql, args...)
	}
	return socialFakeRow{err: pgx.ErrNoRows}
}
func (f *socialFakeDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if f.queryFn != nil {
		return f.queryFn(ctx, sql, args...)
	}
	return &socialFakeRows{}, nil
}
func (f *socialFakeDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if f.execFn != nil {
		return f.execFn(ctx, sql, args...)
	}
	return pgconn.NewCommandTag("DELETE 1"), nil
}
func (f *socialFakeDB) Begin(ctx context.Context) (pgx.Tx, error) {
	if f.beginFn != nil {
		return f.beginFn(ctx)
	}
	return &socialFakeTx{}, nil
}

type socialFakeTx struct {
	queryRowFn func(context.Context, string, ...any) pgx.Row
	execFn     func(context.Context, string, ...any) (pgconn.CommandTag, error)
	commitFn   func(context.Context) error
}

func (t *socialFakeTx) Begin(context.Context) (pgx.Tx, error) { return nil, errors.New("not implemented") }
func (t *socialFakeTx) Rollback(context.Context) error        { return nil }
func (t *socialFakeTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, errors.New("not implemented")
}
func (t *socialFakeTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults { return nil }
func (t *socialFakeTx) LargeObjects() pgx.LargeObjects                          { return pgx.LargeObjects{} }
func (t *socialFakeTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, errors.New("not implemented")
}
func (t *socialFakeTx) Query(context.Context, string, ...any) (pgx.Rows, error) { return &socialFakeRows{}, nil }
func (t *socialFakeTx) Conn() *pgx.Conn                                          { return nil }
func (t *socialFakeTx) Commit(ctx context.Context) error {
	if t.commitFn != nil {
		return t.commitFn(ctx)
	}
	return nil
}
func (t *socialFakeTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if t.execFn != nil {
		return t.execFn(ctx, sql, args...)
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}
func (t *socialFakeTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if t.queryRowFn != nil {
		return t.queryRowFn(ctx, sql, args...)
	}
	return socialFakeRow{err: pgx.ErrNoRows}
}

type socialFakeRow struct {
	vals []any
	err  error
}

func assignScanDest(dest any, src any) error {
	dv := reflect.ValueOf(dest)
	if dv.Kind() != reflect.Ptr || dv.IsNil() {
		return fmt.Errorf("destination must be non-nil pointer, got %T", dest)
	}
	ev := dv.Elem()
	if src == nil {
		ev.Set(reflect.Zero(ev.Type()))
		return nil
	}
	sv := reflect.ValueOf(src)
	if sv.Type().AssignableTo(ev.Type()) {
		ev.Set(sv)
		return nil
	}
	if sv.Type().ConvertibleTo(ev.Type()) {
		ev.Set(sv.Convert(ev.Type()))
		return nil
	}
	return fmt.Errorf("cannot assign %T to %T", src, dest)
}

func (r socialFakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.vals) {
		return fmt.Errorf("scan mismatch: %d != %d", len(dest), len(r.vals))
	}
	for i := range dest {
		if err := assignScanDest(dest[i], r.vals[i]); err != nil {
			return err
		}
	}
	return nil
}

type socialFakeRows struct {
	data [][]any
	idx  int
	err  error
}

func (r *socialFakeRows) Close()                                       {}
func (r *socialFakeRows) Err() error                                   { return r.err }
func (r *socialFakeRows) CommandTag() pgconn.CommandTag                { return pgconn.NewCommandTag("SELECT 0") }
func (r *socialFakeRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *socialFakeRows) Next() bool {
	if r.idx >= len(r.data) {
		return false
	}
	r.idx++
	return true
}
func (r *socialFakeRows) Scan(dest ...any) error {
	if r.idx == 0 || r.idx > len(r.data) {
		return errors.New("scan without row")
	}
	return socialFakeRow{vals: r.data[r.idx-1]}.Scan(dest...)
}
func (r *socialFakeRows) Values() ([]any, error) { return nil, errors.New("not implemented") }
func (r *socialFakeRows) RawValues() [][]byte     { return nil }
func (r *socialFakeRows) Conn() *pgx.Conn         { return nil }

func socialProviderRow(now time.Time, id uuid.UUID) []any {
	return []any{
		id,
		"google",
		"Google",
		"google",
		ProtocolOIDC,
		"",
		"",
		"",
		"",
		"",
		"",
		[]byte(`["openid","email"]`),
		[]byte(`{"subject":"sub","email":"email"}`),
		[]byte(`{"prompt":"consent"}`),
		PKCES256,
		AuthStyleClientSecretPost,
		ClaimSourceUserInfo,
		true,
		true,
		"http://localhost/callback",
		"client-id",
		"",
		now,
		now,
	}
}

func TestPostgresStoreSocialStubCoverage(t *testing.T) {
	now := time.Now().UTC()
	key := make([]byte, platformcrypto.KeySize)
	s := &PostgresStore{pool: &socialFakeDB{}, cipherKey: key}

	s.pool = &socialFakeDB{queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
		return nil, errors.New("query failed")
	}}
	if _, err := s.ListProviders(context.Background(), true); err == nil || err.Error() != "query failed" {
		t.Fatalf("ListProviders(query error) = %v", err)
	}

	s.pool = &socialFakeDB{queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
		return &socialFakeRows{data: [][]any{{"bad"}}}, nil
	}}
	if _, err := s.ListProviders(context.Background(), false); err == nil {
		t.Fatal("ListProviders(scan error) expected error")
	}

	s.pool = &socialFakeDB{queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
		return &socialFakeRows{err: errors.New("rows failed")}, nil
	}}
	if _, err := s.ListProviders(context.Background(), true); err == nil || err.Error() != "rows failed" {
		t.Fatalf("ListProviders(rows err) = %v", err)
	}

	s.pool = &socialFakeDB{queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
		return &socialFakeRows{data: [][]any{socialProviderRow(now, uuid.New())}}, nil
	}}
	if providers, err := s.ListProviders(context.Background(), true); err != nil || len(providers) != 1 {
		t.Fatalf("ListProviders(success) len=%d err=%v", len(providers), err)
	}

	s.pool = &socialFakeDB{queryRowFn: func(context.Context, string, ...any) pgx.Row { return socialFakeRow{err: pgx.ErrNoRows} }}
	if _, err := s.GetProviderBySlug(context.Background(), "missing"); !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("GetProviderBySlug(not found) = %v", err)
	}

	s.pool = &socialFakeDB{
		queryRowFn: func(context.Context, string, ...any) pgx.Row { return socialFakeRow{vals: socialProviderRow(now, uuid.New())} },
	}
	if p, err := s.GetProviderBySlug(context.Background(), "google"); err != nil || p.Slug != "google" {
		t.Fatalf("GetProviderBySlug(success) provider=%#v err=%v", p, err)
	}

	s.pool = &socialFakeDB{
		beginFn: func(context.Context) (pgx.Tx, error) { return nil, errors.New("begin failed") },
	}
	if _, err := s.UpsertProvider(context.Background(), Provider{Slug: "google"}); err == nil || err.Error() != "begin failed" {
		t.Fatalf("UpsertProvider(begin error) = %v", err)
	}

	s.pool = &socialFakeDB{
		beginFn: func(context.Context) (pgx.Tx, error) {
			return &socialFakeTx{
				queryRowFn: func(context.Context, string, ...any) pgx.Row { return socialFakeRow{err: errors.New("insert failed")} },
			}, nil
		},
	}
	if _, err := s.UpsertProvider(context.Background(), Provider{Slug: "google"}); err == nil || err.Error() != "insert failed" {
		t.Fatalf("UpsertProvider(insert error) = %v", err)
	}

	committedID := uuid.New()
	s.pool = &socialFakeDB{
		beginFn: func(context.Context) (pgx.Tx, error) {
			return &socialFakeTx{
				queryRowFn: func(context.Context, string, ...any) pgx.Row { return socialFakeRow{vals: []any{committedID, now, now}} },
				execFn:     func(context.Context, string, ...any) (pgconn.CommandTag, error) { return pgconn.CommandTag{}, errors.New("credentials failed") },
			}, nil
		},
	}
	if _, err := s.UpsertProvider(context.Background(), Provider{Slug: "google"}); err == nil || err.Error() != "credentials failed" {
		t.Fatalf("UpsertProvider(credentials error) = %v", err)
	}

	s.pool = &socialFakeDB{
		beginFn: func(context.Context) (pgx.Tx, error) {
			return &socialFakeTx{
				queryRowFn: func(context.Context, string, ...any) pgx.Row { return socialFakeRow{vals: []any{committedID, now, now}} },
				execFn:     func(context.Context, string, ...any) (pgconn.CommandTag, error) { return pgconn.NewCommandTag("INSERT 1"), nil },
				commitFn:   func(context.Context) error { return errors.New("commit failed") },
			}, nil
		},
	}
	if _, err := s.UpsertProvider(context.Background(), Provider{Slug: "google"}); err == nil || err.Error() != "commit failed" {
		t.Fatalf("UpsertProvider(commit error) = %v", err)
	}

	s.pool = &socialFakeDB{
		beginFn: func(context.Context) (pgx.Tx, error) {
			return &socialFakeTx{
				queryRowFn: func(context.Context, string, ...any) pgx.Row { return socialFakeRow{vals: []any{committedID, now, now}} },
				execFn:     func(context.Context, string, ...any) (pgconn.CommandTag, error) { return pgconn.NewCommandTag("INSERT 1"), nil },
			}, nil
		},
		queryRowFn: func(context.Context, string, ...any) pgx.Row { return socialFakeRow{vals: socialProviderRow(now, committedID)} },
	}
	if got, err := s.UpsertProvider(context.Background(), Provider{Slug: "google", DisplayName: "Google", Protocol: ProtocolOIDC}); err != nil || got.ID != committedID {
		t.Fatalf("UpsertProvider(success) provider=%#v err=%v", got, err)
	}

	s.pool = &socialFakeDB{execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) { return pgconn.CommandTag{}, errors.New("delete failed") }}
	if err := s.DeleteProvider(context.Background(), "google"); err == nil || err.Error() != "delete failed" {
		t.Fatalf("DeleteProvider(exec error) = %v", err)
	}
	s.pool = &socialFakeDB{execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) { return pgconn.NewCommandTag("DELETE 0"), nil }}
	if err := s.DeleteProvider(context.Background(), "google"); !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("DeleteProvider(not found) = %v", err)
	}
	s.pool = &socialFakeDB{execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) { return pgconn.NewCommandTag("DELETE 1"), nil }}
	if err := s.DeleteProvider(context.Background(), "google"); err != nil {
		t.Fatalf("DeleteProvider(success) = %v", err)
	}

	shortKeyStore := &PostgresStore{pool: &socialFakeDB{}, cipherKey: []byte("short")}
	if err := shortKeyStore.SaveState(context.Background(), AuthState{ID: "state-1", ProviderSlug: "google", PKCEVerifier: "verifier", ExpiresAt: now.Add(time.Minute)}); err == nil {
		t.Fatal("SaveState(encrypt error) expected error")
	}
	s.pool = &socialFakeDB{execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) { return pgconn.NewCommandTag("INSERT 1"), nil }}
	if err := s.SaveState(context.Background(), AuthState{ID: "state-2", ProviderSlug: "google", ExpiresAt: now.Add(time.Minute)}); err != nil {
		t.Fatalf("SaveState(success) = %v", err)
	}

	s.pool = &socialFakeDB{beginFn: func(context.Context) (pgx.Tx, error) { return nil, errors.New("begin failed") }}
	if _, err := s.ConsumeState(context.Background(), "state-1"); err == nil || err.Error() != "begin failed" {
		t.Fatalf("ConsumeState(begin error) = %v", err)
	}
	s.pool = &socialFakeDB{beginFn: func(context.Context) (pgx.Tx, error) { return &socialFakeTx{queryRowFn: func(context.Context, string, ...any) pgx.Row { return socialFakeRow{err: pgx.ErrNoRows} }}, nil }}
	if _, err := s.ConsumeState(context.Background(), "missing"); !errors.Is(err, ErrStateNotFound) {
		t.Fatalf("ConsumeState(not found) = %v", err)
	}
	s.pool = &socialFakeDB{beginFn: func(context.Context) (pgx.Tx, error) {
		return &socialFakeTx{queryRowFn: func(context.Context, string, ...any) pgx.Row { return socialFakeRow{err: errors.New("state query failed")} }}, nil
	}}
	if _, err := s.ConsumeState(context.Background(), "state-3"); err == nil || err.Error() != "state query failed" {
		t.Fatalf("ConsumeState(query error) = %v", err)
	}

	stateFuture := now.Add(5 * time.Minute)
	s.pool = &socialFakeDB{beginFn: func(context.Context) (pgx.Tx, error) {
		return &socialFakeTx{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return socialFakeRow{vals: []any{"state-4", "google", "/app", "nonce", "", stateFuture}}
			},
			execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) { return pgconn.CommandTag{}, errors.New("delete state failed") },
		}, nil
	}}
	if _, err := s.ConsumeState(context.Background(), "state-4"); err == nil || err.Error() != "delete state failed" {
		t.Fatalf("ConsumeState(delete error) = %v", err)
	}

	s.pool = &socialFakeDB{beginFn: func(context.Context) (pgx.Tx, error) {
		return &socialFakeTx{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return socialFakeRow{vals: []any{"state-5", "google", "/app", "nonce", "", stateFuture}}
			},
			execFn:   func(context.Context, string, ...any) (pgconn.CommandTag, error) { return pgconn.NewCommandTag("DELETE 1"), nil },
			commitFn: func(context.Context) error { return errors.New("commit failed") },
		}, nil
	}}
	if _, err := s.ConsumeState(context.Background(), "state-5"); err == nil || err.Error() != "commit failed" {
		t.Fatalf("ConsumeState(commit error) = %v", err)
	}

	s.pool = &socialFakeDB{beginFn: func(context.Context) (pgx.Tx, error) {
		return &socialFakeTx{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return socialFakeRow{vals: []any{"state-6", "google", "/app", "nonce", "", now.Add(-time.Minute)}}
			},
			execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) { return pgconn.NewCommandTag("DELETE 1"), nil },
		}, nil
	}}
	if _, err := s.ConsumeState(context.Background(), "state-6"); !errors.Is(err, ErrStateExpired) {
		t.Fatalf("ConsumeState(expired) = %v", err)
	}

	s.pool = &socialFakeDB{beginFn: func(context.Context) (pgx.Tx, error) {
		return &socialFakeTx{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return socialFakeRow{vals: []any{"state-7", "google", "/app", "nonce", "bad-cipher", stateFuture}}
			},
			execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) { return pgconn.NewCommandTag("DELETE 1"), nil },
		}, nil
	}}
	if _, err := s.ConsumeState(context.Background(), "state-7"); err == nil {
		t.Fatal("ConsumeState(decrypt error) expected error")
	}

	validCipher, err := platformcrypto.EncryptField(key, []byte("pkce-verifier"), []byte("state-8"))
	if err != nil {
		t.Fatalf("encrypt fixture: %v", err)
	}
	s.pool = &socialFakeDB{beginFn: func(context.Context) (pgx.Tx, error) {
		return &socialFakeTx{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return socialFakeRow{vals: []any{"state-8", "google", "/app", "nonce", validCipher, stateFuture}}
			},
			execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) { return pgconn.NewCommandTag("DELETE 1"), nil },
		}, nil
	}}
	if st, err := s.ConsumeState(context.Background(), "state-8"); err != nil || st.PKCEVerifier != "pkce-verifier" {
		t.Fatalf("ConsumeState(success) state=%#v err=%v", st, err)
	}

	s.pool = &socialFakeDB{beginFn: func(context.Context) (pgx.Tx, error) { return nil, errors.New("begin failed") }}
	if _, err := s.ResolveIdentity(context.Background(), Provider{Slug: "google"}, SocialProfile{}); err == nil || err.Error() != "begin failed" {
		t.Fatalf("ResolveIdentity(begin error) = %v", err)
	}
	s.pool = &socialFakeDB{beginFn: func(context.Context) (pgx.Tx, error) { return &socialFakeTx{}, nil }}
	if _, err := s.ResolveIdentity(context.Background(), Provider{Slug: "google"}, SocialProfile{RawClaims: map[string]interface{}{"bad": func() {}}}); err == nil {
		t.Fatal("ResolveIdentity(raw claims marshal error) expected error")
	}
	s.pool = &socialFakeDB{beginFn: func(context.Context) (pgx.Tx, error) {
		return &socialFakeTx{
			queryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
				if strings.Contains(sql, "FROM soc_identity_links") {
					return socialFakeRow{err: errors.New("link lookup failed")}
				}
				return socialFakeRow{err: pgx.ErrNoRows}
			},
		}, nil
	}}
	if _, err := s.ResolveIdentity(context.Background(), Provider{Slug: "google"}, SocialProfile{ProviderUser: "sub-link", RawClaims: map[string]interface{}{"sub": "sub-link"}}); err == nil || err.Error() != "link lookup failed" {
		t.Fatalf("ResolveIdentity(link lookup error) = %v", err)
	}

	existingID := uuid.New()
	s.pool = &socialFakeDB{beginFn: func(context.Context) (pgx.Tx, error) {
		return &socialFakeTx{
			queryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
				if strings.Contains(sql, "FROM soc_identity_links") {
					return socialFakeRow{vals: []any{existingID}}
				}
				return socialFakeRow{err: errors.New("unexpected query")}
			},
			execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("UPDATE 1"), nil
			},
		}, nil
	}}
	result, err := s.ResolveIdentity(context.Background(), Provider{Slug: "google", TrustEmailVerified: true}, SocialProfile{
		ProviderUser:  "sub-1",
		Email:         "user@example.com",
		EmailVerified: true,
		RawClaims:     map[string]interface{}{"sub": "sub-1"},
	})
	if err != nil || result.IdentityID != existingID || !result.Linked || result.Created {
		t.Fatalf("ResolveIdentity(existing link success) result=%#v err=%v", result, err)
	}

	s.pool = &socialFakeDB{beginFn: func(context.Context) (pgx.Tx, error) {
		return &socialFakeTx{
			queryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
				if strings.Contains(sql, "FROM soc_identity_links") {
					return socialFakeRow{vals: []any{existingID}}
				}
				return socialFakeRow{err: errors.New("unexpected query")}
			},
			execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("refresh failed")
			},
		}, nil
	}}
	if _, err := s.ResolveIdentity(context.Background(), Provider{Slug: "google"}, SocialProfile{ProviderUser: "sub-1", RawClaims: map[string]interface{}{"sub": "sub-1"}}); err == nil || err.Error() != "refresh failed" {
		t.Fatalf("ResolveIdentity(existing refresh error) = %v", err)
	}

	s.pool = &socialFakeDB{beginFn: func(context.Context) (pgx.Tx, error) {
		return &socialFakeTx{
			queryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
				if strings.Contains(sql, "FROM soc_identity_links") {
					return socialFakeRow{vals: []any{existingID}}
				}
				return socialFakeRow{err: errors.New("unexpected query")}
			},
			execFn: func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
				switch {
				case strings.Contains(sql, "INSERT INTO soc_identity_links"):
					return pgconn.NewCommandTag("INSERT 1"), nil
				case strings.Contains(sql, "UPDATE core_identity_addresses"):
					return pgconn.CommandTag{}, errors.New("email upsert failed")
				default:
					return pgconn.NewCommandTag("UPDATE 1"), nil
				}
			},
		}, nil
	}}
	if _, err := s.ResolveIdentity(context.Background(), Provider{Slug: "google", TrustEmailVerified: true}, SocialProfile{
		ProviderUser:  "sub-1",
		Email:         "existing-link@example.com",
		EmailVerified: true,
		RawClaims:     map[string]interface{}{"sub": "sub-1"},
	}); err == nil || err.Error() != "email upsert failed" {
		t.Fatalf("ResolveIdentity(existing link email upsert error) = %v", err)
	}

	s.pool = &socialFakeDB{beginFn: func(context.Context) (pgx.Tx, error) {
		return &socialFakeTx{
			queryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
				if strings.Contains(sql, "FROM soc_identity_links") {
					return socialFakeRow{vals: []any{existingID}}
				}
				return socialFakeRow{err: errors.New("unexpected query")}
			},
			execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("UPDATE 1"), nil
			},
			commitFn: func(context.Context) error { return errors.New("commit failed") },
		}, nil
	}}
	if _, err := s.ResolveIdentity(context.Background(), Provider{Slug: "google"}, SocialProfile{ProviderUser: "sub-1", RawClaims: map[string]interface{}{"sub": "sub-1"}}); err == nil || err.Error() != "commit failed" {
		t.Fatalf("ResolveIdentity(existing commit error) = %v", err)
	}

	s.pool = &socialFakeDB{beginFn: func(context.Context) (pgx.Tx, error) {
		return &socialFakeTx{
			queryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
				switch {
				case strings.Contains(sql, "FROM soc_identity_links"):
					return socialFakeRow{err: pgx.ErrNoRows}
				case strings.Contains(sql, "FROM core_identities i"):
					return socialFakeRow{err: errors.New("identity lookup failed")}
				default:
					return socialFakeRow{err: pgx.ErrNoRows}
				}
			},
		}, nil
	}}
	if _, err := s.ResolveIdentity(context.Background(), Provider{Slug: "google"}, SocialProfile{ProviderUser: "sub-lookup", Email: "lookup@example.com", RawClaims: map[string]interface{}{"sub": "sub-lookup"}}); err == nil || err.Error() != "identity lookup failed" {
		t.Fatalf("ResolveIdentity(lookup error) = %v", err)
	}

	s.pool = &socialFakeDB{beginFn: func(context.Context) (pgx.Tx, error) {
		return &socialFakeTx{
			queryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
				switch {
				case strings.Contains(sql, "FROM soc_identity_links"):
					return socialFakeRow{err: pgx.ErrNoRows}
				case strings.Contains(sql, "FROM core_identities i"):
					return socialFakeRow{err: socialFakeRow{err: pgx.ErrNoRows}.err}
				case strings.Contains(sql, "FROM core_identity_schemas"):
					return socialFakeRow{vals: []any{uuid.New()}}
				default:
					return socialFakeRow{err: errors.New("unexpected query")}
				}
			},
			execFn: func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
				switch {
				case strings.Contains(sql, "INSERT INTO core_identities"):
					return pgconn.NewCommandTag("INSERT 1"), nil
				case strings.Contains(sql, "INSERT INTO soc_identity_links"):
					return pgconn.CommandTag{}, errors.New("refresh failed")
				default:
					return pgconn.NewCommandTag("UPDATE 1"), nil
				}
			},
		}, nil
	}}
	if _, err := s.ResolveIdentity(context.Background(), Provider{Slug: "google"}, SocialProfile{ProviderUser: "sub-refresh", RawClaims: map[string]interface{}{"sub": "sub-refresh"}}); err == nil || err.Error() != "refresh failed" {
		t.Fatalf("ResolveIdentity(new refresh error) = %v", err)
	}

	s.pool = &socialFakeDB{beginFn: func(context.Context) (pgx.Tx, error) {
		return &socialFakeTx{
			queryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
				switch {
				case strings.Contains(sql, "FROM soc_identity_links"):
					return socialFakeRow{err: pgx.ErrNoRows}
				case strings.Contains(sql, "FROM core_identities i"):
					return socialFakeRow{err: pgx.ErrNoRows}
				case strings.Contains(sql, "FROM core_identity_schemas"):
					return socialFakeRow{vals: []any{uuid.New()}}
				default:
					return socialFakeRow{err: errors.New("unexpected query")}
				}
			},
			execFn: func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
				switch {
				case strings.Contains(sql, "INSERT INTO core_identities"):
					return pgconn.NewCommandTag("INSERT 1"), nil
				case strings.Contains(sql, "INSERT INTO soc_identity_links"):
					return pgconn.NewCommandTag("INSERT 1"), nil
				default:
					return pgconn.NewCommandTag("UPDATE 1"), nil
				}
			},
			commitFn: func(context.Context) error { return errors.New("commit failed") },
		}, nil
	}}
	if _, err := s.ResolveIdentity(context.Background(), Provider{Slug: "google"}, SocialProfile{ProviderUser: "sub-commit", RawClaims: map[string]interface{}{"sub": "sub-commit"}}); err == nil || err.Error() != "commit failed" {
		t.Fatalf("ResolveIdentity(new commit error) = %v", err)
	}

	s.pool = &socialFakeDB{beginFn: func(context.Context) (pgx.Tx, error) {
		return &socialFakeTx{
			queryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
				switch {
				case strings.Contains(sql, "FROM soc_identity_links"):
					return socialFakeRow{err: pgx.ErrNoRows}
				case strings.Contains(sql, "FROM core_identities i"):
					return socialFakeRow{vals: []any{existingID}}
				default:
					return socialFakeRow{err: errors.New("unexpected query")}
				}
			},
			execFn: func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
				if strings.Contains(sql, "UPDATE core_identity_addresses") {
					return pgconn.CommandTag{}, errors.New("email update failed")
				}
				return pgconn.NewCommandTag("UPDATE 1"), nil
			},
		}, nil
	}}
	if _, err := s.ResolveIdentity(context.Background(), Provider{Slug: "google", TrustEmailVerified: true}, SocialProfile{
		ProviderUser:  "sub-existing-email",
		Email:         "existing@example.com",
		EmailVerified: true,
		RawClaims:     map[string]interface{}{"sub": "sub-existing-email"},
	}); err == nil || err.Error() != "email update failed" {
		t.Fatalf("ResolveIdentity(existing email upsert error) = %v", err)
	}

	s.pool = &socialFakeDB{beginFn: func(context.Context) (pgx.Tx, error) {
		return &socialFakeTx{
			queryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
				switch {
				case strings.Contains(sql, "FROM soc_identity_links"):
					return socialFakeRow{err: pgx.ErrNoRows}
				case strings.Contains(sql, "FROM core_identities i"):
					return socialFakeRow{err: pgx.ErrNoRows}
				case strings.Contains(sql, "FROM core_identity_schemas"):
					return socialFakeRow{vals: []any{uuid.New()}}
				default:
					return socialFakeRow{err: errors.New("unexpected query")}
				}
			},
			execFn: func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
				switch {
				case strings.Contains(sql, "INSERT INTO core_identities"):
					return pgconn.NewCommandTag("INSERT 1"), nil
				case strings.Contains(sql, "UPDATE core_identity_addresses"):
					return pgconn.CommandTag{}, errors.New("email update failed")
				default:
					return pgconn.NewCommandTag("UPDATE 1"), nil
				}
			},
		}, nil
	}}
	if _, err := s.ResolveIdentity(context.Background(), Provider{Slug: "google", TrustEmailVerified: true}, SocialProfile{
		ProviderUser:  "sub-new-email",
		Email:         "new@example.com",
		EmailVerified: true,
		RawClaims:     map[string]interface{}{"sub": "sub-new-email"},
	}); err == nil || err.Error() != "email update failed" {
		t.Fatalf("ResolveIdentity(new email upsert error) = %v", err)
	}

	shortKeyUpsert := &PostgresStore{
		pool: &socialFakeDB{beginFn: func(context.Context) (pgx.Tx, error) {
			return &socialFakeTx{
				queryRowFn: func(context.Context, string, ...any) pgx.Row { return socialFakeRow{vals: []any{uuid.New(), now, now}} },
			}, nil
		}},
		cipherKey: []byte("short"),
	}
	if _, err := shortKeyUpsert.UpsertProvider(context.Background(), Provider{
		Slug:         "google",
		DisplayName:  "Google",
		Protocol:     ProtocolOIDC,
		ClientID:     "client-id",
		ClientSecret: "secret",
	}); err == nil {
		t.Fatal("UpsertProvider(encrypt secret error) expected error")
	}

	s.pool = &socialFakeDB{beginFn: func(context.Context) (pgx.Tx, error) {
		return &socialFakeTx{
			queryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
				switch {
				case strings.Contains(sql, "FROM soc_identity_links"):
					return socialFakeRow{err: pgx.ErrNoRows}
				case strings.Contains(sql, "FROM core_identities i"):
					return socialFakeRow{err: pgx.ErrNoRows}
				case strings.Contains(sql, "FROM core_identity_schemas"):
					return socialFakeRow{vals: []any{uuid.New()}}
				default:
					return socialFakeRow{err: errors.New("unexpected query")}
				}
			},
			execFn: func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
				switch {
				case strings.Contains(sql, "INSERT INTO core_identities"):
					return pgconn.NewCommandTag("INSERT 1"), nil
				case strings.Contains(sql, "UPDATE core_identity_addresses"):
					return pgconn.NewCommandTag("UPDATE 0"), nil
				case strings.Contains(sql, "INSERT INTO core_identity_addresses"):
					return pgconn.NewCommandTag("INSERT 1"), nil
				case strings.Contains(sql, "INSERT INTO soc_identity_links"):
					return pgconn.NewCommandTag("INSERT 1"), nil
				default:
					return pgconn.NewCommandTag("UPDATE 1"), nil
				}
			},
		}, nil
	}}
	result, err = s.ResolveIdentity(context.Background(), Provider{Slug: "google", TrustEmailVerified: true}, SocialProfile{
		ProviderUser:  "sub-2",
		Email:         "new@example.com",
		EmailVerified: true,
		Name:          "New User",
		RawClaims:     map[string]interface{}{"sub": "sub-2"},
	})
	if err != nil || !result.Linked || !result.Created || result.IdentityID == uuid.Nil {
		t.Fatalf("ResolveIdentity(new identity success) result=%#v err=%v", result, err)
	}
}

