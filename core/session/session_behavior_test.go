package session

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/aegion/aegion/internal/platform/secrettoken"
)

type stubSessionRow struct {
	values []interface{}
	err    error
}

func (r stubSessionRow) Scan(dest ...interface{}) error {
	if r.err != nil {
		return r.err
	}
	for i := range dest {
		switch d := dest[i].(type) {
		case *uuid.UUID:
			if r.values[i] == nil {
				*d = uuid.Nil
			} else {
				*d = r.values[i].(uuid.UUID)
			}
		case *string:
			*d = r.values[i].(string)
		case *AAL:
			*d = r.values[i].(AAL)
		case *time.Time:
			*d = r.values[i].(time.Time)
		case *[]DeviceInfo:
			*d = r.values[i].([]DeviceInfo)
		case *bool:
			*d = r.values[i].(bool)
		case **uuid.UUID:
			if r.values[i] == nil {
				*d = nil
			} else {
				id := r.values[i].(uuid.UUID)
				*d = &id
			}
		case *int64:
			*d = r.values[i].(int64)
		default:
			return errors.New("unsupported scan destination")
		}
	}
	return nil
}

type stubSessionRows struct {
	rows    [][]interface{}
	scanErr map[int]error
	err     error
	idx     int
	closed  bool
}

func (s *stubSessionRows) Close()                                       { s.closed = true }
func (s *stubSessionRows) Err() error                                   { return s.err }
func (s *stubSessionRows) CommandTag() pgconn.CommandTag                { return pgconn.NewCommandTag("SELECT 0") }
func (s *stubSessionRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (s *stubSessionRows) Next() bool {
	return s.idx < len(s.rows)
}
func (s *stubSessionRows) Values() ([]interface{}, error) { return nil, errors.New("not implemented") }
func (s *stubSessionRows) RawValues() [][]byte            { return nil }
func (s *stubSessionRows) Conn() *pgx.Conn                { return nil }

func (s *stubSessionRows) Scan(dest ...interface{}) error {
	rowIdx := s.idx
	s.idx++
	if err, ok := s.scanErr[rowIdx]; ok {
		return err
	}
	row := s.rows[rowIdx]
	for i := range dest {
		switch d := dest[i].(type) {
		case *AuthMethod:
			*d = row[i].(AuthMethod)
		case *AAL:
			*d = row[i].(AAL)
		case *time.Time:
			*d = row[i].(time.Time)
		default:
			return errors.New("unsupported scan destination")
		}
	}
	return nil
}

type stubSessionTx struct {
	execErrOn   int
	execErr     error
	queryRowErr error
	queryRowFn  func(ctx context.Context, sql string, args ...interface{}) pgx.Row
	commitErr   error
	rollbackErr error

	execCalls  int
	execSQL    []string
	execArgs   [][]interface{}
	committed  bool
	rolledBack bool
}

func (s *stubSessionTx) Exec(_ context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	s.execCalls++
	s.execSQL = append(s.execSQL, sql)
	s.execArgs = append(s.execArgs, args)
	if s.execErrOn > 0 && s.execCalls == s.execErrOn {
		if s.execErr != nil {
			return pgconn.CommandTag{}, s.execErr
		}
		return pgconn.CommandTag{}, errors.New("exec failed")
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (s *stubSessionTx) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	if s.queryRowFn != nil {
		return s.queryRowFn(ctx, sql, args...)
	}
	if s.queryRowErr != nil {
		return stubSessionRow{err: s.queryRowErr}
	}
	return stubSessionRow{values: []interface{}{AAL1}}
}

func (s *stubSessionTx) Commit(_ context.Context) error {
	s.committed = true
	return s.commitErr
}

func (s *stubSessionTx) Rollback(_ context.Context) error {
	s.rolledBack = true
	return s.rollbackErr
}

func newBehaviorManager() *Manager {
	m := NewManager(ManagerConfig{
		CookieSecret: []byte("session-behavior-secret"),
		CookieConfig: CookieConfig{
			Name:     "aegion_session",
			Path:     "/",
			Domain:   "test.local",
			SameSite: http.SameSiteLaxMode,
			Secure:   false,
			HTTPOnly: true,
		},
		Lifespan:    time.Hour,
		IdleTimeout: 30 * time.Minute,
	})
	m.now = func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	return m
}

func TestManager_DefaultDBSentinels(t *testing.T) {
	m := NewManager(ManagerConfig{})

	if _, err := m.execStmt(context.Background(), "select 1"); !errors.Is(err, errSessionDBUnavailable) {
		t.Fatalf("expected errSessionDBUnavailable from execStmt, got %v", err)
	}
	if err := m.queryRowFn(context.Background(), "select 1").Scan(new(int)); !errors.Is(err, errSessionDBUnavailable) {
		t.Fatalf("expected errSessionDBUnavailable from queryRowFn, got %v", err)
	}
	if _, err := m.queryRows(context.Background(), "select 1"); !errors.Is(err, errSessionDBUnavailable) {
		t.Fatalf("expected errSessionDBUnavailable from queryRows, got %v", err)
	}
	if _, err := m.beginTx(context.Background()); !errors.Is(err, errSessionDBUnavailable) {
		t.Fatalf("expected errSessionDBUnavailable from beginTx, got %v", err)
	}
}

func TestManager_Create_WithSeams(t *testing.T) {
	m := newBehaviorManager()
	identityID := uuid.New()
	device := DeviceInfo{UserAgent: "ua", IPAddress: "127.0.0.1"}

	t.Run("first insert failure", func(t *testing.T) {
		calls := 0
		m.execStmt = func(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
			calls++
			return pgconn.CommandTag{}, errors.New("insert session failed")
		}
		_, err := m.Create(context.Background(), identityID, AuthMethodPassword, device)
		if err == nil || err.Error() != "insert session failed" {
			t.Fatalf("expected insert session failure, got %v", err)
		}
		if calls != 1 {
			t.Fatalf("expected one exec call, got %d", calls)
		}
	})

	t.Run("auth method insert failure", func(t *testing.T) {
		calls := 0
		m.execStmt = func(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
			calls++
			if calls == 2 {
				return pgconn.CommandTag{}, errors.New("insert auth method failed")
			}
			return pgconn.NewCommandTag("INSERT 0 1"), nil
		}
		_, err := m.Create(context.Background(), identityID, AuthMethodPassword, device)
		if err == nil || err.Error() != "insert auth method failed" {
			t.Fatalf("expected second insert failure, got %v", err)
		}
		if calls != 2 {
			t.Fatalf("expected two exec calls, got %d", calls)
		}
	})

	t.Run("success", func(t *testing.T) {
		var captured []string
		m.execStmt = func(_ context.Context, sql string, _ ...interface{}) (pgconn.CommandTag, error) {
			captured = append(captured, sql)
			return pgconn.NewCommandTag("INSERT 0 1"), nil
		}
		s, err := m.Create(context.Background(), identityID, AuthMethodTOTP, device)
		if err != nil {
			t.Fatalf("Create returned error: %v", err)
		}
		if s.IdentityID != identityID || s.AAL != AAL2 {
			t.Fatalf("unexpected created session fields")
		}
		if !s.ExpiresAt.Equal(m.now().Add(m.lifespan)) {
			t.Fatalf("expected expires_at based on seam time")
		}
		if len(s.AuthMethods) != 1 || s.AuthMethods[0].Method != AuthMethodTOTP {
			t.Fatalf("expected initial auth method to be recorded")
		}
		if len(captured) != 2 {
			t.Fatalf("expected 2 inserts, got %d", len(captured))
		}
	})
}

func TestManager_Get_WithSeams(t *testing.T) {
	m := newBehaviorManager()
	sid := uuid.New()
	iid := uuid.New()
	now := m.now()

	sessionValues := []interface{}{
		sid,
		"token-123",
		iid,
		AAL1,
		now,
		now.Add(10 * time.Minute),
		now,
		"logout-123",
		[]DeviceInfo{{UserAgent: "ua", IPAddress: "1.2.3.4"}},
		true,
		false,
		nil,
		now,
		now,
	}

	t.Run("session not found maps error", func(t *testing.T) {
		m.queryRowFn = func(context.Context, string, ...interface{}) pgx.Row {
			return stubSessionRow{err: pgx.ErrNoRows}
		}
		if _, err := m.Get(context.Background(), "missing"); !errors.Is(err, ErrSessionNotFound) {
			t.Fatalf("expected ErrSessionNotFound, got %v", err)
		}
	})

	t.Run("query row hard failure", func(t *testing.T) {
		m.queryRowFn = func(context.Context, string, ...interface{}) pgx.Row {
			return stubSessionRow{err: errors.New("query failed")}
		}
		if _, err := m.Get(context.Background(), "token"); err == nil || err.Error() != "query failed" {
			t.Fatalf("expected query failed, got %v", err)
		}
	})

	t.Run("expired session", func(t *testing.T) {
		expired := make([]interface{}, len(sessionValues))
		copy(expired, sessionValues)
		expired[5] = now.Add(-time.Minute)
		m.queryRowFn = func(context.Context, string, ...interface{}) pgx.Row {
			return stubSessionRow{values: expired}
		}
		if _, err := m.Get(context.Background(), "token"); !errors.Is(err, ErrSessionExpired) {
			t.Fatalf("expected ErrSessionExpired, got %v", err)
		}
	})

	t.Run("auth methods query failure", func(t *testing.T) {
		m.queryRowFn = func(context.Context, string, ...interface{}) pgx.Row {
			return stubSessionRow{values: sessionValues}
		}
		m.queryRows = func(context.Context, string, ...interface{}) (pgx.Rows, error) {
			return nil, errors.New("auth query failed")
		}
		if _, err := m.Get(context.Background(), "token"); err == nil || err.Error() != "auth query failed" {
			t.Fatalf("expected auth query failure, got %v", err)
		}
	})

	t.Run("auth methods scan error", func(t *testing.T) {
		m.queryRowFn = func(context.Context, string, ...interface{}) pgx.Row {
			return stubSessionRow{values: sessionValues}
		}
		rows := &stubSessionRows{
			rows:    [][]interface{}{{AuthMethodPassword, AAL1, now}},
			scanErr: map[int]error{0: errors.New("scan failed")},
		}
		m.queryRows = func(context.Context, string, ...interface{}) (pgx.Rows, error) {
			return rows, nil
		}
		if _, err := m.Get(context.Background(), "token"); err == nil || err.Error() != "scan failed" {
			t.Fatalf("expected scan failed, got %v", err)
		}
		if !rows.closed {
			t.Fatalf("expected rows to be closed on scan error")
		}
	})

	t.Run("rows err after iteration", func(t *testing.T) {
		m.queryRowFn = func(context.Context, string, ...interface{}) pgx.Row {
			return stubSessionRow{values: sessionValues}
		}
		rows := &stubSessionRows{
			rows: [][]interface{}{{AuthMethodPassword, AAL1, now}},
			err:  errors.New("rows failed"),
		}
		m.queryRows = func(context.Context, string, ...interface{}) (pgx.Rows, error) {
			return rows, nil
		}
		if _, err := m.Get(context.Background(), "token"); err == nil || err.Error() != "rows failed" {
			t.Fatalf("expected rows failed, got %v", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		m.queryRowFn = func(context.Context, string, ...interface{}) pgx.Row {
			return stubSessionRow{values: sessionValues}
		}
		rows := &stubSessionRows{
			rows: [][]interface{}{
				{AuthMethodPassword, AAL1, now},
				{AuthMethodTOTP, AAL2, now.Add(time.Minute)},
			},
		}
		m.queryRows = func(context.Context, string, ...interface{}) (pgx.Rows, error) {
			return rows, nil
		}

		s, err := m.Get(context.Background(), "token-123")
		if err != nil {
			t.Fatalf("Get returned error: %v", err)
		}
		if s.ID != sid || s.IdentityID != iid || len(s.AuthMethods) != 2 {
			t.Fatalf("unexpected session returned")
		}
		if !rows.closed {
			t.Fatalf("expected rows closed on success")
		}
	})
}

func TestManager_WriteOps_WithSeams(t *testing.T) {
	m := newBehaviorManager()
	sessionID := uuid.New()
	identityID := uuid.New()
	now := m.now()

	t.Run("revoke and revoke all and extend errors", func(t *testing.T) {
		m.execStmt = func(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, errors.New("write failed")
		}
		if err := m.Revoke(context.Background(), sessionID); err == nil {
			t.Fatalf("expected revoke error")
		}
		if err := m.RevokeAllForIdentity(context.Background(), identityID); err == nil {
			t.Fatalf("expected revoke all error")
		}
		if err := m.Extend(context.Background(), sessionID); err == nil {
			t.Fatalf("expected extend error")
		}
	})

	t.Run("extend success captures expiry", func(t *testing.T) {
		var args []interface{}
		m.execStmt = func(_ context.Context, _ string, in ...interface{}) (pgconn.CommandTag, error) {
			args = in
			return pgconn.NewCommandTag("UPDATE 1"), nil
		}
		if err := m.Extend(context.Background(), sessionID); err != nil {
			t.Fatalf("Extend returned error: %v", err)
		}
		if len(args) != 2 {
			t.Fatalf("expected expiry and session ID args")
		}
		expiry := args[0].(time.Time)
		if !expiry.Equal(now.Add(m.lifespan)) {
			t.Fatalf("expected seam-based expiry")
		}
	})

	t.Run("cleanup error and success", func(t *testing.T) {
		m.execStmt = func(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, errors.New("cleanup failed")
		}
		if _, err := m.Cleanup(context.Background()); err == nil {
			t.Fatalf("expected cleanup error")
		}

		var args []interface{}
		m.execStmt = func(_ context.Context, _ string, in ...interface{}) (pgconn.CommandTag, error) {
			args = in
			return pgconn.NewCommandTag("DELETE 5"), nil
		}
		deleted, err := m.Cleanup(context.Background())
		if err != nil {
			t.Fatalf("cleanup returned error: %v", err)
		}
		if deleted != 5 {
			t.Fatalf("expected 5 rows deleted, got %d", deleted)
		}
		if len(args) != 2 {
			t.Fatalf("expected 2 cleanup args, got %d", len(args))
		}
		if args[0].(float64) != m.cleanupExpiredAfter.Seconds() {
			t.Fatalf("expected cleanup expired seconds arg")
		}
		if args[1].(float64) != m.cleanupInactiveAfter.Seconds() {
			t.Fatalf("expected cleanup inactive seconds arg")
		}
	})
}

func TestManager_AddAuthMethod_WithSeams(t *testing.T) {
	m := newBehaviorManager()
	sessionID := uuid.New()

	t.Run("begin tx error", func(t *testing.T) {
		m.beginTx = func(context.Context) (sessionTx, error) {
			return nil, errors.New("begin failed")
		}
		if err := m.AddAuthMethod(context.Background(), sessionID, AuthMethodTOTP); err == nil || err.Error() != "begin failed" {
			t.Fatalf("expected begin failed, got %v", err)
		}
	})

	t.Run("insert auth method error", func(t *testing.T) {
		tx := &stubSessionTx{execErrOn: 1, execErr: errors.New("insert failed")}
		m.beginTx = func(context.Context) (sessionTx, error) { return tx, nil }
		err := m.AddAuthMethod(context.Background(), sessionID, AuthMethodTOTP)
		if err == nil || err.Error() != "insert failed" {
			t.Fatalf("expected insert failed, got %v", err)
		}
		if !tx.rolledBack {
			t.Fatalf("expected rollback on insert failure")
		}
	})

	t.Run("current aal query error", func(t *testing.T) {
		tx := &stubSessionTx{queryRowErr: errors.New("query failed")}
		m.beginTx = func(context.Context) (sessionTx, error) { return tx, nil }
		err := m.AddAuthMethod(context.Background(), sessionID, AuthMethodTOTP)
		if err == nil || err.Error() != "query failed" {
			t.Fatalf("expected query failed, got %v", err)
		}
	})

	t.Run("aal upgrade update error", func(t *testing.T) {
		tx := &stubSessionTx{execErrOn: 2, execErr: errors.New("update failed")}
		m.beginTx = func(context.Context) (sessionTx, error) { return tx, nil }
		err := m.AddAuthMethod(context.Background(), sessionID, AuthMethodTOTP)
		if err == nil || err.Error() != "update failed" {
			t.Fatalf("expected update failed, got %v", err)
		}
	})

	t.Run("commit error", func(t *testing.T) {
		tx := &stubSessionTx{
			commitErr:   errors.New("commit failed"),
			rollbackErr: pgx.ErrTxClosed,
		}
		m.beginTx = func(context.Context) (sessionTx, error) { return tx, nil }
		err := m.AddAuthMethod(context.Background(), sessionID, AuthMethodTOTP)
		if err == nil || err.Error() != "commit failed" {
			t.Fatalf("expected commit failed, got %v", err)
		}
		if !tx.committed {
			t.Fatalf("expected commit attempted")
		}
	})

	t.Run("success with aal upgrade", func(t *testing.T) {
		tx := &stubSessionTx{rollbackErr: pgx.ErrTxClosed}
		m.beginTx = func(context.Context) (sessionTx, error) { return tx, nil }
		if err := m.AddAuthMethod(context.Background(), sessionID, AuthMethodTOTP); err != nil {
			t.Fatalf("AddAuthMethod returned error: %v", err)
		}
		if len(tx.execSQL) != 2 {
			t.Fatalf("expected insert + update for AAL upgrade, got %d", len(tx.execSQL))
		}
	})

	t.Run("success without aal upgrade", func(t *testing.T) {
		tx := &stubSessionTx{rollbackErr: pgx.ErrTxClosed}
		tx.queryRowFn = func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
			return stubSessionRow{values: []interface{}{AAL2}}
		}
		m.beginTx = func(context.Context) (sessionTx, error) { return tx, nil }
		if err := m.AddAuthMethod(context.Background(), sessionID, AuthMethodPassword); err != nil {
			t.Fatalf("AddAuthMethod returned error: %v", err)
		}
		if len(tx.execSQL) != 1 {
			t.Fatalf("expected only insert when no AAL upgrade needed, got %d", len(tx.execSQL))
		}
	})
}

func TestManager_GetFromRequest_WithSeams(t *testing.T) {
	m := newBehaviorManager()
	now := m.now()
	sid := uuid.New()
	iid := uuid.New()

	m.queryRows = func(context.Context, string, ...interface{}) (pgx.Rows, error) {
		return &stubSessionRows{}, nil
	}
	m.queryRowFn = func(_ context.Context, _ string, args ...interface{}) pgx.Row {
		if len(args) != 2 {
			return stubSessionRow{err: pgx.ErrNoRows}
		}
		tokenHash, _ := args[0].(string)
		switch tokenHash {
		case secrettoken.Hash("cookie-token"), secrettoken.Hash("bearer-token"), secrettoken.Hash("header-token"):
			return stubSessionRow{values: []interface{}{
				sid, "", iid, AAL1, now, now.Add(time.Hour), now, "", []DeviceInfo{}, true, false, nil, now, now,
			}}
		default:
			return stubSessionRow{err: pgx.ErrNoRows}
		}
	}

	t.Run("cookie token has priority", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: m.cookieConfig.Name, Value: m.signToken("cookie-token")})
		req.Header.Set("Authorization", "Bearer bearer-token")
		req.Header.Set("X-Session-Token", "header-token")

		s, err := m.GetFromRequest(context.Background(), req)
		if err != nil {
			t.Fatalf("GetFromRequest returned error: %v", err)
		}
		if s.Token != "cookie-token" {
			t.Fatalf("expected cookie token to win, got %s", s.Token)
		}
	})

	t.Run("invalid cookie falls back to bearer", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: m.cookieConfig.Name, Value: "invalid.signature"})
		req.Header.Set("Authorization", "Bearer bearer-token")

		s, err := m.GetFromRequest(context.Background(), req)
		if err != nil {
			t.Fatalf("expected bearer fallback success, got %v", err)
		}
		if s.Token != "bearer-token" {
			t.Fatalf("expected bearer token, got %s", s.Token)
		}
	})

	t.Run("fallback to custom header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Session-Token", "header-token")

		s, err := m.GetFromRequest(context.Background(), req)
		if err != nil {
			t.Fatalf("expected custom header success, got %v", err)
		}
		if s.Token != "header-token" {
			t.Fatalf("expected header token, got %s", s.Token)
		}
	})

	t.Run("missing all sources", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if _, err := m.GetFromRequest(context.Background(), req); !errors.Is(err, ErrSessionNotFound) {
			t.Fatalf("expected ErrSessionNotFound, got %v", err)
		}
	})
}
