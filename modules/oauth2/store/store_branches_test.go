package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type mockDB struct {
	execFn     func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	queryRowFn func(ctx context.Context, sql string, args ...any) pgx.Row
	queryFn    func(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func (m *mockDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if m.execFn != nil {
		return m.execFn(ctx, sql, args...)
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (m *mockDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if m.queryRowFn != nil {
		return m.queryRowFn(ctx, sql, args...)
	}
	return mockRow{err: pgx.ErrNoRows}
}

func (m *mockDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if m.queryFn != nil {
		return m.queryFn(ctx, sql, args...)
	}
	return &mockRows{}, nil
}

type mockRow struct {
	values []any
	err    error
}

func (r mockRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return fmt.Errorf("scan mismatch: got %d destinations for %d values", len(dest), len(r.values))
	}
	for i := range dest {
		if err := assignScanValue(dest[i], r.values[i]); err != nil {
			return err
		}
	}
	return nil
}

type mockRows struct {
	rows    [][]any
	idx     int
	err     error
	scanErr error
}

func (r *mockRows) Close() {}

func (r *mockRows) Err() error { return r.err }

func (r *mockRows) CommandTag() pgconn.CommandTag { return pgconn.NewCommandTag("SELECT 0") }

func (r *mockRows) FieldDescriptions() []pgconn.FieldDescription { return nil }

func (r *mockRows) Next() bool {
	if r.idx >= len(r.rows) {
		return false
	}
	r.idx++
	return true
}

func (r *mockRows) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	if r.idx == 0 || r.idx > len(r.rows) {
		return errors.New("scan called without current row")
	}
	row := r.rows[r.idx-1]
	if len(dest) != len(row) {
		return fmt.Errorf("scan mismatch: got %d destinations for %d values", len(dest), len(row))
	}
	for i := range dest {
		if err := assignScanValue(dest[i], row[i]); err != nil {
			return err
		}
	}
	return nil
}

func (r *mockRows) Values() ([]any, error) {
	if r.idx == 0 || r.idx > len(r.rows) {
		return nil, errors.New("values called without current row")
	}
	return r.rows[r.idx-1], nil
}

func (r *mockRows) RawValues() [][]byte { return nil }

func (r *mockRows) Conn() *pgx.Conn { return nil }

func assignScanValue(dest any, value any) error {
	destValue := reflect.ValueOf(dest)
	if destValue.Kind() != reflect.Ptr || destValue.IsNil() {
		return fmt.Errorf("destination must be a non-nil pointer, got %T", dest)
	}

	target := destValue.Elem()
	if value == nil {
		target.Set(reflect.Zero(target.Type()))
		return nil
	}

	valueValue := reflect.ValueOf(value)
	if valueValue.Type().AssignableTo(target.Type()) {
		target.Set(valueValue)
		return nil
	}
	if valueValue.Type().ConvertibleTo(target.Type()) {
		target.Set(valueValue.Convert(target.Type()))
		return nil
	}
	return fmt.Errorf("cannot assign %T to %T", value, dest)
}

func duplicateKeyError() error {
	return &pgconn.PgError{Code: "23505"}
}

func TestStoreHelpers(t *testing.T) {
	t.Run("New handles pool input", func(t *testing.T) {
		if New(nil) == nil {
			t.Fatal("expected non-nil store")
		}
	})

	t.Run("NewWithDB wires db dependency", func(t *testing.T) {
		db := &mockDB{}
		store := NewWithDB(db)
		if store == nil {
			t.Fatal("expected non-nil store")
		}
		if store.db != db {
			t.Fatal("store did not keep provided db")
		}
	})

	t.Run("generators produce prefixed values", func(t *testing.T) {
		if !strings.HasPrefix(GenerateClientID(), "oa2_") {
			t.Fatal("client ID is missing oa2_ prefix")
		}
		if GenerateAuthCode() == "" {
			t.Fatal("auth code should not be empty")
		}
		if !strings.HasPrefix(GenerateDeviceCode(), "dc_") {
			t.Fatal("device code is missing dc_ prefix")
		}

		code := GenerateUserCode()
		if len(code) != 9 || code[4] != '-' {
			t.Fatalf("unexpected user code format: %q", code)
		}
		charset := "BCDFGHJKLMNPQRSTVWXZ"
		for _, c := range strings.ReplaceAll(code, "-", "") {
			if !strings.ContainsRune(charset, c) {
				t.Fatalf("unexpected character %q in code %q", c, code)
			}
		}
	})

	t.Run("duplicate key detection", func(t *testing.T) {
		if isDuplicateKeyError(nil) {
			t.Fatal("nil error should not be treated as duplicate key")
		}
		if !isDuplicateKeyError(duplicateKeyError()) {
			t.Fatal("expected duplicate key error to be detected")
		}
		if isDuplicateKeyError(errors.New("other error")) {
			t.Fatal("non-pg duplicate error should not match")
		}
	})
}

func TestClientStoreBranches(t *testing.T) {
	ctx := context.Background()

	t.Run("CreateClient maps duplicate key to ErrAlreadyExists", func(t *testing.T) {
		s := NewWithDB(&mockDB{
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				if !strings.Contains(sql, "INSERT INTO oa2_clients") {
					t.Fatalf("unexpected SQL: %s", sql)
				}
				return pgconn.CommandTag{}, duplicateKeyError()
			},
		})

		err := s.CreateClient(ctx, &Client{Name: "client-a"})
		if !errors.Is(err, ErrAlreadyExists) {
			t.Fatalf("expected ErrAlreadyExists, got %v", err)
		}
	})

	t.Run("GetClient maps no rows to ErrNotFound", func(t *testing.T) {
		s := NewWithDB(&mockDB{queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return mockRow{err: pgx.ErrNoRows}
		}})

		_, err := s.GetClient(ctx, "missing")
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("UpdateClient returns ErrNotFound when no rows are affected", func(t *testing.T) {
		s := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}})

		err := s.UpdateClient(ctx, &Client{ID: "missing"})
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("UpdateClientSecret returns ErrNotFound when no rows are affected", func(t *testing.T) {
		s := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}})

		err := s.UpdateClientSecret(ctx, "missing", "hashed-secret")
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("DeleteClient returns ErrNotFound when no rows are affected", func(t *testing.T) {
		s := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("DELETE 0"), nil
		}})

		err := s.DeleteClient(ctx, "missing")
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("ListClients applies defaults and scans rows", func(t *testing.T) {
		description := "demo"
		createdAt := time.Now().UTC().Add(-time.Minute)
		updatedAt := time.Now().UTC()
		var gotArgs []any

		s := NewWithDB(&mockDB{queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			gotArgs = args
			return &mockRows{rows: [][]any{{
				"client-1",
				"Client One",
				&description,
				[]string{"https://app.example.com/callback"},
				[]string{"authorization_code"},
				[]string{"openid"},
				"none",
				true,
				createdAt,
				updatedAt,
			}}}, nil
		}})

		clients, err := s.ListClients(ctx, nil, 0, 7)
		if err != nil {
			t.Fatalf("ListClients returned error: %v", err)
		}
		if len(clients) != 1 {
			t.Fatalf("expected 1 client, got %d", len(clients))
		}
		if len(gotArgs) != 2 || gotArgs[0] != 50 || gotArgs[1] != 7 {
			t.Fatalf("expected args [50 7], got %#v", gotArgs)
		}
	})

	t.Run("ListClients owner filter clamps upper limit", func(t *testing.T) {
		ownerID := "owner-1"
		var gotArgs []any

		s := NewWithDB(&mockDB{queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			if !strings.Contains(sql, "WHERE owner_id = $1") {
				t.Fatalf("owner filter query missing WHERE owner_id clause: %s", sql)
			}
			gotArgs = args
			return &mockRows{}, nil
		}})

		clients, err := s.ListClients(ctx, &ownerID, 200, 3)
		if err != nil {
			t.Fatalf("ListClients returned error: %v", err)
		}
		if len(clients) != 0 {
			t.Fatalf("expected no clients, got %d", len(clients))
		}
		if len(gotArgs) != 3 || gotArgs[0] != ownerID || gotArgs[1] != 100 || gotArgs[2] != 3 {
			t.Fatalf("expected args [owner-1 100 3], got %#v", gotArgs)
		}
	})

	t.Run("ListClients returns query error", func(t *testing.T) {
		s := NewWithDB(&mockDB{queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, errors.New("query failed")
		}})

		_, err := s.ListClients(ctx, nil, 10, 0)
		if err == nil || !strings.Contains(err.Error(), "query failed") {
			t.Fatalf("expected query error, got %v", err)
		}
	})

	t.Run("Client helper methods enforce client contract", func(t *testing.T) {
		client := &Client{
			RedirectURIs:            []string{"https://app.example.com/callback"},
			GrantTypes:              []string{"authorization_code", "refresh_token"},
			Scopes:                  []string{"openid", "profile"},
			TokenEndpointAuthMethod: "none",
		}

		if !client.ValidateRedirectURI("https://app.example.com/callback") {
			t.Fatal("expected redirect URI to be accepted")
		}
		if client.ValidateRedirectURI("https://evil.example.com") {
			t.Fatal("unexpected redirect URI accepted")
		}
		if !client.HasGrantType("refresh_token") {
			t.Fatal("expected grant type to be supported")
		}
		if client.HasGrantType("client_credentials") {
			t.Fatal("unexpected unsupported grant type accepted")
		}
		if !client.HasScope("openid") {
			t.Fatal("expected scope to be supported")
		}
		if client.HasScope("admin") {
			t.Fatal("unexpected unsupported scope accepted")
		}
		if !client.IsPublic() {
			t.Fatal("expected auth method 'none' to be public")
		}
	})
}

func TestAuthCodeStoreBranches(t *testing.T) {
	ctx := context.Background()

	t.Run("CreateAuthCode sets defaults before insert", func(t *testing.T) {
		var gotArgs []any
		s := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if !strings.Contains(sql, "INSERT INTO oa2_auth_codes") {
				t.Fatalf("unexpected SQL: %s", sql)
			}
			gotArgs = args
			return pgconn.NewCommandTag("INSERT 0 1"), nil
		}})

		code := &AuthCode{
			ClientID:    "client-1",
			IdentityID:  "identity-1",
			SessionID:   "session-1",
			RedirectURI: "https://app.example.com/callback",
			Scopes:      []string{"openid"},
			ExpiresAt:   time.Now().UTC().Add(5 * time.Minute),
		}
		err := s.CreateAuthCode(ctx, code)
		if err != nil {
			t.Fatalf("CreateAuthCode returned error: %v", err)
		}
		if code.Code == "" {
			t.Fatal("expected CreateAuthCode to generate a code")
		}
		if code.CreatedAt.IsZero() || code.AuthTime.IsZero() {
			t.Fatal("expected CreateAuthCode to set timestamps")
		}
		if len(gotArgs) == 0 || gotArgs[0] != code.Code {
			t.Fatalf("expected generated code as first SQL argument, got %#v", gotArgs)
		}
	})

	t.Run("CreateAuthCode maps duplicate key to ErrAlreadyExists", func(t *testing.T) {
		s := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, duplicateKeyError()
		}})
		err := s.CreateAuthCode(ctx, &AuthCode{ExpiresAt: time.Now().UTC().Add(time.Minute)})
		if !errors.Is(err, ErrAlreadyExists) {
			t.Fatalf("expected ErrAlreadyExists, got %v", err)
		}
	})

	t.Run("GetAuthCode maps no rows to ErrNotFound", func(t *testing.T) {
		s := NewWithDB(&mockDB{queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return mockRow{err: pgx.ErrNoRows}
		}})
		_, err := s.GetAuthCode(ctx, "missing")
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("MarkAuthCodeUsed returns ErrCodeUsed when already used", func(t *testing.T) {
		s := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}})
		err := s.MarkAuthCodeUsed(ctx, "code-1")
		if !errors.Is(err, ErrCodeUsed) {
			t.Fatalf("expected ErrCodeUsed, got %v", err)
		}
	})

	t.Run("DeleteAuthCode executes delete query", func(t *testing.T) {
		called := false
		s := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			called = true
			if !strings.Contains(sql, "DELETE FROM oa2_auth_codes") {
				t.Fatalf("unexpected SQL: %s", sql)
			}
			return pgconn.NewCommandTag("DELETE 1"), nil
		}})

		if err := s.DeleteAuthCode(ctx, "code-1"); err != nil {
			t.Fatalf("DeleteAuthCode returned error: %v", err)
		}
		if !called {
			t.Fatal("expected DeleteAuthCode to execute SQL")
		}
	})

	t.Run("CleanupExpiredAuthCodes returns deleted row count", func(t *testing.T) {
		s := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("DELETE 3"), nil
		}})
		count, err := s.CleanupExpiredAuthCodes(ctx)
		if err != nil {
			t.Fatalf("CleanupExpiredAuthCodes returned error: %v", err)
		}
		if count != 3 {
			t.Fatalf("expected 3 rows removed, got %d", count)
		}
	})
}

func TestDeviceAndAssertionStoreBranches(t *testing.T) {
	ctx := context.Background()

	t.Run("CreateDeviceCode applies defaults", func(t *testing.T) {
		var gotArgs []any
		s := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			gotArgs = args
			return pgconn.NewCommandTag("INSERT 0 1"), nil
		}})

		dc := &DeviceCode{
			ClientID:        "client-1",
			Scopes:          []string{"openid"},
			VerificationURI: "https://issuer.example.com/device",
			ExpiresAt:       time.Now().UTC().Add(10 * time.Minute),
		}
		err := s.CreateDeviceCode(ctx, dc)
		if err != nil {
			t.Fatalf("CreateDeviceCode returned error: %v", err)
		}
		if dc.DeviceCode == "" || dc.UserCode == "" {
			t.Fatal("expected generated device/user codes")
		}
		if dc.Interval != 5 {
			t.Fatalf("expected default poll interval 5, got %d", dc.Interval)
		}
		if dc.Status != "pending" {
			t.Fatalf("expected default status pending, got %q", dc.Status)
		}
		if dc.CreatedAt.IsZero() {
			t.Fatal("expected CreatedAt to be set")
		}
		if len(gotArgs) < 15 {
			t.Fatalf("expected SQL args for insert, got %#v", gotArgs)
		}
		if gotArgs[0] != dc.DeviceCode || gotArgs[1] != dc.UserCode {
			t.Fatalf("expected generated codes in insert args, got %#v", gotArgs[:2])
		}
	})

	t.Run("CreateDeviceCode maps duplicate key to ErrAlreadyExists", func(t *testing.T) {
		s := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, duplicateKeyError()
		}})
		err := s.CreateDeviceCode(ctx, &DeviceCode{ExpiresAt: time.Now().UTC().Add(time.Minute)})
		if !errors.Is(err, ErrAlreadyExists) {
			t.Fatalf("expected ErrAlreadyExists, got %v", err)
		}
	})

	t.Run("device code lookups map no rows to ErrNotFound", func(t *testing.T) {
		s := NewWithDB(&mockDB{queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return mockRow{err: pgx.ErrNoRows}
		}})

		if _, err := s.GetDeviceCodeByDeviceCode(ctx, "missing"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound for device_code lookup, got %v", err)
		}
		if _, err := s.GetDeviceCodeByUserCode(ctx, "missing"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound for user_code lookup, got %v", err)
		}
	})

	t.Run("device status updates return ErrNotFound on no-op updates", func(t *testing.T) {
		s := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}})

		if err := s.UpdateDeviceCodePoll(ctx, "missing"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound from UpdateDeviceCodePoll, got %v", err)
		}
		if err := s.ApproveDeviceCode(ctx, "missing", "identity", "session"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound from ApproveDeviceCode, got %v", err)
		}
		if err := s.DenyDeviceCode(ctx, "missing"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound from DenyDeviceCode, got %v", err)
		}
	})

	t.Run("DeleteDeviceCode executes delete query", func(t *testing.T) {
		called := false
		s := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			called = true
			if !strings.Contains(sql, "DELETE FROM oa2_device_codes") {
				t.Fatalf("unexpected SQL: %s", sql)
			}
			return pgconn.NewCommandTag("DELETE 1"), nil
		}})

		if err := s.DeleteDeviceCode(ctx, "dc-1"); err != nil {
			t.Fatalf("DeleteDeviceCode returned error: %v", err)
		}
		if !called {
			t.Fatal("expected DeleteDeviceCode to execute SQL")
		}
	})

	t.Run("CleanupExpiredDeviceCodes returns deleted row count", func(t *testing.T) {
		s := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("DELETE 2"), nil
		}})
		count, err := s.CleanupExpiredDeviceCodes(ctx)
		if err != nil {
			t.Fatalf("CleanupExpiredDeviceCodes returned error: %v", err)
		}
		if count != 2 {
			t.Fatalf("expected 2 rows removed, got %d", count)
		}
	})

	t.Run("JWT assertion writes map duplicate key and not-found paths", func(t *testing.T) {
		s := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if strings.Contains(sql, "INSERT INTO oa2_jwt_assertions") {
				return pgconn.CommandTag{}, duplicateKeyError()
			}
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}})

		err := s.CreateJWTAssertion(ctx, &JWTAssertion{JTI: "jti-1", ExpiresAt: time.Now().UTC().Add(time.Minute)})
		if !errors.Is(err, ErrAlreadyExists) {
			t.Fatalf("expected ErrAlreadyExists, got %v", err)
		}

		err = s.MarkJWTAssertionUsed(ctx, "missing")
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("CleanupExpiredJWTAssertions returns deleted row count", func(t *testing.T) {
		s := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("DELETE 4"), nil
		}})
		count, err := s.CleanupExpiredJWTAssertions(ctx)
		if err != nil {
			t.Fatalf("CleanupExpiredJWTAssertions returned error: %v", err)
		}
		if count != 4 {
			t.Fatalf("expected 4 rows removed, got %d", count)
		}
	})
}

func TestConsentStoreBranches(t *testing.T) {
	ctx := context.Background()

	t.Run("CreateConsentSession sets timestamps", func(t *testing.T) {
		s := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("INSERT 0 1"), nil
		}})

		consent := &ConsentSession{ID: "consent-1", ClientID: "client-1", IdentityID: "identity-1"}
		err := s.CreateConsentSession(ctx, consent)
		if err != nil {
			t.Fatalf("CreateConsentSession returned error: %v", err)
		}
		if consent.CreatedAt.IsZero() || consent.UpdatedAt.IsZero() {
			t.Fatal("expected CreateConsentSession to set timestamps")
		}
	})

	t.Run("GetConsentSession maps no rows to ErrNotFound", func(t *testing.T) {
		s := NewWithDB(&mockDB{queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return mockRow{err: pgx.ErrNoRows}
		}})
		_, err := s.GetConsentSession(ctx, "client-1", "identity-1")
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("DeleteConsentSession executes delete query", func(t *testing.T) {
		called := false
		s := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			called = true
			if !strings.Contains(sql, "DELETE FROM oa2_consent_sessions") {
				t.Fatalf("unexpected SQL: %s", sql)
			}
			return pgconn.NewCommandTag("DELETE 1"), nil
		}})

		if err := s.DeleteConsentSession(ctx, "client-1", "identity-1"); err != nil {
			t.Fatalf("DeleteConsentSession returned error: %v", err)
		}
		if !called {
			t.Fatal("expected DeleteConsentSession to execute SQL")
		}
	})

	t.Run("login challenge create/get/accept branches", func(t *testing.T) {
		callCount := 0
		s := NewWithDB(&mockDB{
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				callCount++
				if strings.Contains(sql, "UPDATE oa2_login_challenges") {
					return pgconn.NewCommandTag("UPDATE 0"), nil
				}
				return pgconn.NewCommandTag("INSERT 0 1"), nil
			},
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return mockRow{err: pgx.ErrNoRows}
			},
		})

		challenge := &LoginChallenge{ClientID: "client-1", ExpiresAt: time.Now().UTC().Add(time.Minute)}
		if err := s.CreateLoginChallenge(ctx, challenge); err != nil {
			t.Fatalf("CreateLoginChallenge returned error: %v", err)
		}
		if challenge.ID == "" || challenge.CreatedAt.IsZero() {
			t.Fatal("expected CreateLoginChallenge to set ID and CreatedAt")
		}

		if _, err := s.GetLoginChallenge(ctx, "missing"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound from GetLoginChallenge, got %v", err)
		}
		if err := s.AcceptLoginChallenge(ctx, "missing", "identity", "session"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound from AcceptLoginChallenge, got %v", err)
		}
		if callCount == 0 {
			t.Fatal("expected CreateLoginChallenge to execute insert")
		}
	})

	t.Run("consent challenge create/get/decision branches", func(t *testing.T) {
		s := NewWithDB(&mockDB{
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				if strings.Contains(sql, "UPDATE oa2_consent_challenges") {
					return pgconn.NewCommandTag("UPDATE 0"), nil
				}
				return pgconn.NewCommandTag("INSERT 0 1"), nil
			},
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return mockRow{err: pgx.ErrNoRows}
			},
		})

		challenge := &ConsentChallenge{
			ClientID:         "client-1",
			IdentityID:       "identity-1",
			SessionID:        "session-1",
			LoginChallengeID: "login-1",
			ExpiresAt:        time.Now().UTC().Add(time.Minute),
		}
		if err := s.CreateConsentChallenge(ctx, challenge); err != nil {
			t.Fatalf("CreateConsentChallenge returned error: %v", err)
		}
		if challenge.ID == "" || challenge.CreatedAt.IsZero() {
			t.Fatal("expected CreateConsentChallenge to set ID and CreatedAt")
		}

		if _, err := s.GetConsentChallenge(ctx, "missing"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound from GetConsentChallenge, got %v", err)
		}
		if err := s.AcceptConsentChallenge(ctx, "missing", []string{"openid"}, nil, true, nil); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound from AcceptConsentChallenge, got %v", err)
		}
		if err := s.RejectConsentChallenge(ctx, "missing", "access_denied", "user denied consent"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound from RejectConsentChallenge, got %v", err)
		}
	})

	t.Run("CleanupExpiredChallenges handles partial failures", func(t *testing.T) {
		t.Run("first cleanup query fails", func(t *testing.T) {
			s := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("login cleanup failed")
			}})
			count, err := s.CleanupExpiredChallenges(ctx)
			if count != 0 {
				t.Fatalf("expected 0 deleted rows, got %d", count)
			}
			if err == nil || !strings.Contains(err.Error(), "login cleanup failed") {
				t.Fatalf("expected login cleanup error, got %v", err)
			}
		})

		t.Run("second cleanup query fails after partial success", func(t *testing.T) {
			call := 0
			s := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				call++
				if call == 1 {
					return pgconn.NewCommandTag("DELETE 2"), nil
				}
				return pgconn.CommandTag{}, errors.New("consent cleanup failed")
			}})
			count, err := s.CleanupExpiredChallenges(ctx)
			if count != 2 {
				t.Fatalf("expected 2 deleted rows before failure, got %d", count)
			}
			if err == nil || !strings.Contains(err.Error(), "consent cleanup failed") {
				t.Fatalf("expected consent cleanup error, got %v", err)
			}
		})

		t.Run("both cleanup queries succeed", func(t *testing.T) {
			call := 0
			s := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				call++
				if call == 1 {
					return pgconn.NewCommandTag("DELETE 2"), nil
				}
				return pgconn.NewCommandTag("DELETE 3"), nil
			}})
			count, err := s.CleanupExpiredChallenges(ctx)
			if err != nil {
				t.Fatalf("CleanupExpiredChallenges returned error: %v", err)
			}
			if count != 5 {
				t.Fatalf("expected 5 deleted rows, got %d", count)
			}
		})
	})
}

func TestTokenStoreBranches(t *testing.T) {
	ctx := context.Background()

	t.Run("CreateAccessToken sets defaults", func(t *testing.T) {
		s := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if !strings.Contains(sql, "INSERT INTO oa2_access_tokens") {
				t.Fatalf("unexpected SQL: %s", sql)
			}
			return pgconn.NewCommandTag("INSERT 0 1"), nil
		}})

		token := &AccessToken{ClientID: "client-1", IdentityID: "identity-1", SessionID: "session-1", ExpiresAt: time.Now().UTC().Add(time.Minute)}
		if err := s.CreateAccessToken(ctx, token); err != nil {
			t.Fatalf("CreateAccessToken returned error: %v", err)
		}
		if token.JTI == "" || token.CreatedAt.IsZero() {
			t.Fatal("expected CreateAccessToken to set JTI and CreatedAt")
		}
	})

	t.Run("GetAccessToken maps no rows to ErrNotFound", func(t *testing.T) {
		s := NewWithDB(&mockDB{queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return mockRow{err: pgx.ErrNoRows}
		}})
		if _, err := s.GetAccessToken(ctx, "missing"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("RevokeAccessToken returns ErrNotFound on no-op update", func(t *testing.T) {
		s := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}})
		if err := s.RevokeAccessToken(ctx, "missing"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("session-wide token revocation reports affected rows", func(t *testing.T) {
		s := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("UPDATE 2"), nil
		}})
		count, err := s.RevokeAccessTokensBySession(ctx, "session-1")
		if err != nil {
			t.Fatalf("RevokeAccessTokensBySession returned error: %v", err)
		}
		if count != 2 {
			t.Fatalf("expected 2 tokens revoked, got %d", count)
		}
	})

	t.Run("CreateRefreshToken sets defaults", func(t *testing.T) {
		s := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("INSERT 0 1"), nil
		}})
		token := &RefreshToken{ClientID: "client-1", IdentityID: "identity-1", SessionID: "session-1", ExpiresAt: time.Now().UTC().Add(time.Minute)}
		if err := s.CreateRefreshToken(ctx, token); err != nil {
			t.Fatalf("CreateRefreshToken returned error: %v", err)
		}
		if token.ID == "" || token.FamilyID == "" || token.CreatedAt.IsZero() {
			t.Fatal("expected CreateRefreshToken to set ID, family ID and CreatedAt")
		}
	})

	t.Run("GetRefreshToken maps no rows to ErrNotFound", func(t *testing.T) {
		s := NewWithDB(&mockDB{queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return mockRow{err: pgx.ErrNoRows}
		}})
		if _, err := s.GetRefreshToken(ctx, "missing"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("MarkRefreshTokenUsed handles grace period and inactive tokens", func(t *testing.T) {
		var capturedGrace any
		s := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			capturedGrace = args[3]
			return pgconn.NewCommandTag("UPDATE 1"), nil
		}})

		err := s.MarkRefreshTokenUsed(ctx, "rt-1", "rt-2", 30*time.Second)
		if err != nil {
			t.Fatalf("MarkRefreshTokenUsed returned error: %v", err)
		}
		if capturedGrace == nil {
			t.Fatal("expected grace period expiration to be passed as non-nil value")
		}

		s = NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}})
		err = s.MarkRefreshTokenUsed(ctx, "missing", "rt-next", 0)
		if !errors.Is(err, ErrTokenInactive) {
			t.Fatalf("expected ErrTokenInactive, got %v", err)
		}
	})

	t.Run("family/session refresh revocation reports rows", func(t *testing.T) {
		s := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("UPDATE 4"), nil
		}})

		familyCount, err := s.InvalidateRefreshTokenFamily(ctx, "family-1")
		if err != nil {
			t.Fatalf("InvalidateRefreshTokenFamily returned error: %v", err)
		}
		if familyCount != 4 {
			t.Fatalf("expected 4 family tokens revoked, got %d", familyCount)
		}

		sessionCount, err := s.RevokeRefreshTokensBySession(ctx, "session-1")
		if err != nil {
			t.Fatalf("RevokeRefreshTokensBySession returned error: %v", err)
		}
		if sessionCount != 4 {
			t.Fatalf("expected 4 session tokens revoked, got %d", sessionCount)
		}
	})

	t.Run("CreateIDToken sets defaults", func(t *testing.T) {
		s := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("INSERT 0 1"), nil
		}})
		token := &IDToken{ClientID: "client-1", IdentityID: "identity-1", SessionID: "session-1", ExpiresAt: time.Now().UTC().Add(time.Minute)}
		if err := s.CreateIDToken(ctx, token); err != nil {
			t.Fatalf("CreateIDToken returned error: %v", err)
		}
		if token.JTI == "" || token.CreatedAt.IsZero() {
			t.Fatal("expected CreateIDToken to set JTI and CreatedAt")
		}
	})

	t.Run("CleanupExpiredTokens handles partial failures", func(t *testing.T) {
		t.Run("first query fails", func(t *testing.T) {
			s := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("access cleanup failed")
			}})
			count, err := s.CleanupExpiredTokens(ctx)
			if count != 0 {
				t.Fatalf("expected 0 deleted rows, got %d", count)
			}
			if err == nil || !strings.Contains(err.Error(), "access cleanup failed") {
				t.Fatalf("expected access cleanup error, got %v", err)
			}
		})

		t.Run("second query fails after first success", func(t *testing.T) {
			call := 0
			s := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				call++
				if call == 1 {
					return pgconn.NewCommandTag("DELETE 2"), nil
				}
				return pgconn.CommandTag{}, errors.New("refresh cleanup failed")
			}})
			count, err := s.CleanupExpiredTokens(ctx)
			if count != 2 {
				t.Fatalf("expected 2 deleted rows before failure, got %d", count)
			}
			if err == nil || !strings.Contains(err.Error(), "refresh cleanup failed") {
				t.Fatalf("expected refresh cleanup error, got %v", err)
			}
		})

		t.Run("third query fails after partial success", func(t *testing.T) {
			call := 0
			s := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				call++
				switch call {
				case 1:
					return pgconn.NewCommandTag("DELETE 2"), nil
				case 2:
					return pgconn.NewCommandTag("DELETE 3"), nil
				default:
					return pgconn.CommandTag{}, errors.New("id token cleanup failed")
				}
			}})
			count, err := s.CleanupExpiredTokens(ctx)
			if count != 5 {
				t.Fatalf("expected 5 deleted rows before failure, got %d", count)
			}
			if err == nil || !strings.Contains(err.Error(), "id token cleanup failed") {
				t.Fatalf("expected id token cleanup error, got %v", err)
			}
		})

		t.Run("all cleanup queries succeed", func(t *testing.T) {
			call := 0
			s := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				call++
				switch call {
				case 1:
					return pgconn.NewCommandTag("DELETE 2"), nil
				case 2:
					return pgconn.NewCommandTag("DELETE 3"), nil
				default:
					return pgconn.NewCommandTag("DELETE 4"), nil
				}
			}})
			count, err := s.CleanupExpiredTokens(ctx)
			if err != nil {
				t.Fatalf("CleanupExpiredTokens returned error: %v", err)
			}
			if count != 9 {
				t.Fatalf("expected 9 deleted rows, got %d", count)
			}
		})
	})

	t.Run("token validation returns expected errors", func(t *testing.T) {
		if !errors.Is((&AccessToken{Revoked: true, ExpiresAt: time.Now().UTC().Add(time.Minute)}).IsValid(), ErrTokenRevoked) {
			t.Fatal("revoked access token should return ErrTokenRevoked")
		}
		if !errors.Is((&AccessToken{ExpiresAt: time.Now().UTC().Add(-time.Second)}).IsValid(), ErrTokenExpired) {
			t.Fatal("expired access token should return ErrTokenExpired")
		}
		if err := (&AccessToken{ExpiresAt: time.Now().UTC().Add(time.Minute)}).IsValid(); err != nil {
			t.Fatalf("valid access token should not fail, got %v", err)
		}

		if !errors.Is((&RefreshToken{Active: false, ExpiresAt: time.Now().UTC().Add(time.Minute)}).IsValid(), ErrTokenInactive) {
			t.Fatal("inactive refresh token should return ErrTokenInactive")
		}
		if !errors.Is((&RefreshToken{Active: true, ExpiresAt: time.Now().UTC().Add(-time.Second)}).IsValid(), ErrTokenExpired) {
			t.Fatal("expired refresh token should return ErrTokenExpired")
		}
		if err := (&RefreshToken{Active: true, ExpiresAt: time.Now().UTC().Add(time.Minute)}).IsValid(); err != nil {
			t.Fatalf("valid refresh token should not fail, got %v", err)
		}
	})
}

func TestStoreAdditionalSuccessAndErrorPaths(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Round(0)

	t.Run("getter success paths populate models", func(t *testing.T) {
		secretHash := "secret-hash"
		ownerID := "owner-1"
		reqObj := "request-object"
		state := "state-1"
		identityID := "identity-1"
		sessionID := "session-1"

		s := NewWithDB(&mockDB{queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			switch {
			case strings.Contains(sql, "FROM oa2_auth_codes"):
				return mockRow{values: []any{
					"code-1", "client-1", identityID, sessionID, "https://app.example.com/callback",
					[]string{"openid"}, []string{"aud1"}, nil, nil, nil, &state, "urn:mace:incommon:iap:silver",
					[]string{"pwd"}, now, &reqObj, false, (*time.Time)(nil), now.Add(5 * time.Minute), now,
				}}
			case strings.Contains(sql, "FROM oa2_device_codes WHERE device_code"):
				return mockRow{values: []any{
					"dc-1", "ABCD-EFGH", "client-1", []string{"openid"}, []string{"aud1"}, "https://issuer.example.com/device",
					(*string)(nil), 5, &identityID, &sessionID, "approved", &now, now.Add(5 * time.Minute), &now, now,
				}}
			case strings.Contains(sql, "FROM oa2_device_codes WHERE user_code"):
				return mockRow{values: []any{
					"dc-2", "WXYZ-QRST", "client-1", []string{"openid"}, []string{"aud1"}, "https://issuer.example.com/device",
					(*string)(nil), 5, &identityID, &sessionID, "pending", (*time.Time)(nil), now.Add(5 * time.Minute), (*time.Time)(nil), now,
				}}
			case strings.Contains(sql, "FROM oa2_access_tokens"):
				return mockRow{values: []any{
					"at-1", (*string)(nil), "client-1", identityID, sessionID, []string{"openid"}, []string{"aud1"},
					"https://issuer.example.com", "sub-1", []byte(`{"role":"admin"}`), false, (*time.Time)(nil), now.Add(5 * time.Minute), now,
				}}
			case strings.Contains(sql, "FROM oa2_refresh_tokens"):
				return mockRow{values: []any{
					"rt-1", "family-1", "client-1", identityID, sessionID, []string{"openid"}, []string{"aud1"},
					true, false, (*time.Time)(nil), (*string)(nil), (*time.Time)(nil), (*time.Time)(nil), (*string)(nil),
					[]byte(`{"offline":true}`), now.Add(10 * time.Minute), now,
				}}
			case strings.Contains(sql, "FROM oa2_clients"):
				return mockRow{values: []any{
					"client-1", &secretHash, "Client One", (*string)(nil), (*string)(nil), (*string)(nil), (*string)(nil), (*string)(nil),
					[]string{"https://app.example.com/callback"}, []string{"https://app.example.com/logout"},
					[]string{"authorization_code"}, []string{"code"}, []string{"openid"}, []string{"aud1"},
					"none", (*string)(nil), []byte(`{}`), (*string)(nil), "public", "RS256", "jwt",
					900, 2592000, 3600, 600, true, true, true, []byte(`{"team":"core"}`), &ownerID, now, now,
				}}
			case strings.Contains(sql, "FROM oa2_consent_sessions"):
				rememberFor := 3600
				return mockRow{values: []any{
					"consent-1", "client-1", identityID, []string{"openid"}, []string{"aud1"}, true, &rememberFor,
					map[string]any{"key": "value"}, map[string]any{"id": "claim"}, true, &now, &now, now, now,
				}}
			case strings.Contains(sql, "FROM oa2_login_challenges"):
				return mockRow{values: []any{
					"login-1", "client-1", "https://issuer.example.com/login", "https://app.example.com/cb",
					[]string{"openid"}, []string{"aud1"}, []string{"urn:mace:incommon:iap:silver"},
					(*string)(nil), (*string)(nil), (*string)(nil), (*string)(nil), false, &identityID, &sessionID, &now, now.Add(time.Minute), now,
				}}
			case strings.Contains(sql, "FROM oa2_consent_challenges"):
				return mockRow{values: []any{
					"consent-ch-1", "login-1", "client-1", identityID, sessionID, "https://issuer.example.com/consent",
					"https://app.example.com/cb", []string{"openid"}, []string{"aud1"}, false, []string{"openid"}, []string{"aud1"},
					(*bool)(nil), (*int)(nil), map[string]any{"key": "value"}, map[string]any{"id": "claim"},
					false, (*time.Time)(nil), false, (*string)(nil), (*string)(nil), now.Add(time.Minute), now,
				}}
			default:
				return mockRow{err: errors.New("unexpected query")}
			}
		}})

		if _, err := s.GetAuthCode(ctx, "code-1"); err != nil {
			t.Fatalf("GetAuthCode success path failed: %v", err)
		}
		if _, err := s.GetDeviceCodeByDeviceCode(ctx, "dc-1"); err != nil {
			t.Fatalf("GetDeviceCodeByDeviceCode success path failed: %v", err)
		}
		if _, err := s.GetDeviceCodeByUserCode(ctx, "ABCD-EFGH"); err != nil {
			t.Fatalf("GetDeviceCodeByUserCode success path failed: %v", err)
		}
		if token, err := s.GetAccessToken(ctx, "at-1"); err != nil || token.ExtraClaims["role"] != "admin" {
			t.Fatalf("GetAccessToken success path failed: token=%+v err=%v", token, err)
		}
		if token, err := s.GetRefreshToken(ctx, "rt-1"); err != nil || token.ExtraClaims["offline"] != true {
			t.Fatalf("GetRefreshToken success path failed: token=%+v err=%v", token, err)
		}
		if client, err := s.GetClient(ctx, "client-1"); err != nil || client.Metadata["team"] != "core" {
			t.Fatalf("GetClient success path failed: client=%+v err=%v", client, err)
		}
		if _, err := s.GetConsentSession(ctx, "client-1", identityID); err != nil {
			t.Fatalf("GetConsentSession success path failed: %v", err)
		}
		if _, err := s.GetLoginChallenge(ctx, "login-1"); err != nil {
			t.Fatalf("GetLoginChallenge success path failed: %v", err)
		}
		if _, err := s.GetConsentChallenge(ctx, "consent-ch-1"); err != nil {
			t.Fatalf("GetConsentChallenge success path failed: %v", err)
		}
	})

	t.Run("update success paths and direct exec errors", func(t *testing.T) {
		s := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("UPDATE 1"), nil
		}})

		if err := s.MarkAuthCodeUsed(ctx, "code-1"); err != nil {
			t.Fatalf("MarkAuthCodeUsed success path failed: %v", err)
		}
		if err := s.UpdateClient(ctx, &Client{ID: "client-1"}); err != nil {
			t.Fatalf("UpdateClient success path failed: %v", err)
		}
		if err := s.UpdateClientSecret(ctx, "client-1", "hashed"); err != nil {
			t.Fatalf("UpdateClientSecret success path failed: %v", err)
		}
		if err := s.DeleteClient(ctx, "client-1"); err != nil {
			t.Fatalf("DeleteClient success path failed: %v", err)
		}
		if err := s.AcceptLoginChallenge(ctx, "login-1", "identity-1", "session-1"); err != nil {
			t.Fatalf("AcceptLoginChallenge success path failed: %v", err)
		}
		if err := s.AcceptConsentChallenge(ctx, "consent-1", []string{"openid"}, []string{"aud1"}, true, nil); err != nil {
			t.Fatalf("AcceptConsentChallenge success path failed: %v", err)
		}
		if err := s.RejectConsentChallenge(ctx, "consent-1", "access_denied", "denied"); err != nil {
			t.Fatalf("RejectConsentChallenge success path failed: %v", err)
		}
		if err := s.UpdateDeviceCodePoll(ctx, "dc-1"); err != nil {
			t.Fatalf("UpdateDeviceCodePoll success path failed: %v", err)
		}
		if err := s.ApproveDeviceCode(ctx, "ABCD-EFGH", "identity-1", "session-1"); err != nil {
			t.Fatalf("ApproveDeviceCode success path failed: %v", err)
		}
		if err := s.DenyDeviceCode(ctx, "ABCD-EFGH"); err != nil {
			t.Fatalf("DenyDeviceCode success path failed: %v", err)
		}
		if err := s.MarkJWTAssertionUsed(ctx, "jti-1"); err != nil {
			t.Fatalf("MarkJWTAssertionUsed success path failed: %v", err)
		}
		if err := s.RevokeAccessToken(ctx, "at-1"); err != nil {
			t.Fatalf("RevokeAccessToken success path failed: %v", err)
		}

		errStore := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, errors.New("exec failed")
		}})
		if err := errStore.MarkAuthCodeUsed(ctx, "code-err"); err == nil {
			t.Fatal("expected MarkAuthCodeUsed exec error")
		}
		if err := errStore.UpdateDeviceCodePoll(ctx, "dc-err"); err == nil {
			t.Fatal("expected UpdateDeviceCodePoll exec error")
		}
		if err := errStore.RevokeAccessToken(ctx, "at-err"); err == nil {
			t.Fatal("expected RevokeAccessToken exec error")
		}
	})
}
