package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/aegion/aegion/internal/platform/database"
)

type scriptedRows struct {
	scanFns []func(dest ...any) error
	index   int
	err     error
}

func (r *scriptedRows) Close() {}

func (r *scriptedRows) Err() error {
	return r.err
}

func (r *scriptedRows) CommandTag() pgconn.CommandTag {
	return pgconn.CommandTag{}
}

func (r *scriptedRows) FieldDescriptions() []pgconn.FieldDescription {
	return nil
}

func (r *scriptedRows) Next() bool {
	return r.index < len(r.scanFns)
}

func (r *scriptedRows) Scan(dest ...any) error {
	if r.index >= len(r.scanFns) {
		return errors.New("no scripted row")
	}
	fn := r.scanFns[r.index]
	r.index++
	if fn != nil {
		return fn(dest...)
	}
	return nil
}

func (r *scriptedRows) Values() ([]any, error) {
	return nil, nil
}

func (r *scriptedRows) RawValues() [][]byte {
	return nil
}

func (r *scriptedRows) Conn() *pgx.Conn {
	return nil
}

func newHookedServer(t *testing.T) *Server {
	t.Helper()
	s := newTestServer(t)
	s.db = &database.DB{}
	return s
}

func TestRuntimeSettingsLoadAndPersistWithHooks(t *testing.T) {
	t.Run("load policy settings from runtime config table", func(t *testing.T) {
		s := newHookedServer(t)
		s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error {
				*(dest[0].(*[]byte)) = []byte(`{"enabled":true,"default_model":" ABAC ","rbac":{"enabled":false},"abac":{"enabled":true},"rebac":{"enabled":false}}`)
				return nil
			}}
		}

		settings, err := s.loadRuntimePolicySettings(context.Background())
		if err != nil {
			t.Fatalf("loadRuntimePolicySettings failed: %v", err)
		}
		if !settings.Enabled || settings.DefaultModel != "abac" || settings.RBAC.Enabled || !settings.ABAC.Enabled {
			t.Fatalf("unexpected policy settings: %+v", settings)
		}
	})

	t.Run("load proxy settings applies defaults for blank optional values", func(t *testing.T) {
		s := newHookedServer(t)
		s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error {
				*(dest[0].(*[]byte)) = []byte(`{"enabled":true,"upstream_timeout":" ","identity_signature_header":" ","signed_identity_headers":[]}`)
				return nil
			}}
		}

		settings, err := s.loadRuntimeProxySettings(context.Background())
		if err != nil {
			t.Fatalf("loadRuntimeProxySettings failed: %v", err)
		}
		if settings.UpstreamTimeout != "30s" {
			t.Fatalf("expected default upstream timeout, got %q", settings.UpstreamTimeout)
		}
		if settings.IdentitySignatureHeader != "X-Aegion-Signature" {
			t.Fatalf("expected default signature header, got %q", settings.IdentitySignatureHeader)
		}
		if len(settings.SignedIdentityHeaders) != 3 {
			t.Fatalf("expected default signed identity headers, got %v", settings.SignedIdentityHeaders)
		}
	})

	t.Run("runtime setting decoders return errors for invalid json", func(t *testing.T) {
		s := newHookedServer(t)
		s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error {
				*(dest[0].(*[]byte)) = []byte("{")
				return nil
			}}
		}

		if _, err := s.loadRuntimePolicySettings(context.Background()); err == nil {
			t.Fatal("expected policy runtime json decode error")
		}
		if _, err := s.loadRuntimeProxySettings(context.Background()); err == nil {
			t.Fatal("expected proxy runtime json decode error")
		}
	})

	t.Run("save runtime config handles success and failures", func(t *testing.T) {
		s := newHookedServer(t)
		execCalls := 0
		s.dbExecFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			execCalls++
			if len(args) != 2 || args[0] != systemConfigKeyPolicy {
				t.Fatalf("unexpected save args: %#v", args)
			}
			if _, ok := args[1].(string); !ok {
				t.Fatalf("expected marshaled json string payload, got %#v", args[1])
			}
			return pgconn.NewCommandTag("INSERT 0 1"), nil
		}

		if err := s.saveRuntimeConfig(context.Background(), systemConfigKeyPolicy, map[string]any{"enabled": true}); err != nil {
			t.Fatalf("saveRuntimeConfig failed: %v", err)
		}
		if execCalls != 1 {
			t.Fatalf("expected one exec call, got %d", execCalls)
		}

		if err := s.saveRuntimeConfig(context.Background(), systemConfigKeyPolicy, map[string]any{"bad": func() {}}); err == nil {
			t.Fatal("expected json marshal error")
		}

		s.dbExecFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, errors.New("exec failed")
		}
		if err := s.saveRuntimeConfig(context.Background(), systemConfigKeyPolicy, map[string]any{"enabled": false}); err == nil {
			t.Fatal("expected exec failure")
		}
	})
}

func TestHandleAdminUpdateConfig_WithDatabaseHooks(t *testing.T) {
	t.Run("successful policy and proxy patch persistence", func(t *testing.T) {
		s := newHookedServer(t)
		configRows := map[string][]byte{}
		s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
			key := args[0].(string)
			raw, ok := configRows[key]
			if !ok {
				return adminTestRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
			}
			return adminTestRow{scanFn: func(dest ...any) error {
				*(dest[0].(*[]byte)) = raw
				return nil
			}}
		}
		s.dbExecFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			key := args[0].(string)
			configRows[key] = []byte(args[1].(string))
			return pgconn.NewCommandTag("INSERT 0 1"), nil
		}

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/aegion/api/v1/system/config", mustJSONBody(t, map[string]any{
			"policy": map[string]any{
				"enabled":       true,
				"default_model": "abac",
				"rbac":          map[string]any{"enabled": false},
				"abac":          map[string]any{"enabled": true},
				"rebac":         map[string]any{"enabled": false},
			},
			"proxy": map[string]any{
				"enabled":                        true,
				"upstream_timeout":               "45s",
				"preserve_host":                  true,
				"strip_inbound_identity_headers": true,
				"identity_signature_header":      "X-Proxy-Sig",
				"signed_identity_headers":        []string{"X-User-ID", "X-User-Session-ID"},
			},
		}))
		s.handleAdminUpdateConfig(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d body=%s", http.StatusOK, rec.Code, rec.Body.String())
		}

		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		policy := body["policy"].(map[string]any)
		if policy["default_model"] != "abac" {
			t.Fatalf("expected patched policy model abac, got %v", policy["default_model"])
		}
		proxy := body["proxy"].(map[string]any)
		if proxy["upstream_timeout"] != "45s" {
			t.Fatalf("expected patched upstream timeout 45s, got %v", proxy["upstream_timeout"])
		}
		if proxy["identity_signing_secret_set"] != false {
			t.Fatalf("expected runtime config response to avoid persisted signing secrets, got %v", proxy["identity_signing_secret_set"])
		}
		if strings.Contains(string(configRows[systemConfigKeyProxy]), "identity_signing_secret") {
			t.Fatalf("expected persisted proxy runtime config to exclude identity signing secret, got %s", string(configRows[systemConfigKeyProxy]))
		}
	})

	t.Run("persistence failure returns 500", func(t *testing.T) {
		s := newHookedServer(t)
		s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
		}
		s.dbExecFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, errors.New("persist failed")
		}

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/aegion/api/v1/system/config", mustJSONBody(t, map[string]any{
			"policy": map[string]any{"enabled": true, "rbac": map[string]any{"enabled": true}},
		}))
		s.handleAdminUpdateConfig(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
	})
}

func TestHandleAdminMetrics_WithDatabaseHooks(t *testing.T) {
	s := newHookedServer(t)
	s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
		switch {
		case strings.Contains(sql, "FROM core_identities"):
			return adminTestRow{scanFn: func(dest ...any) error {
				*(dest[0].(*int64)) = 12
				return nil
			}}
		case strings.Contains(sql, "FROM core_sessions"):
			return adminTestRow{scanFn: func(dest ...any) error {
				*(dest[0].(*int64)) = 5
				return nil
			}}
		default:
			t.Fatalf("unexpected metrics query: %s", sql)
			return adminTestRow{scanFn: func(dest ...any) error { return errors.New("unexpected query") }}
		}
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/aegion/api/v1/system/metrics", nil)
	s.handleAdminMetrics(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["database"] != "connected" {
		t.Fatalf("expected connected database status, got %v", body["database"])
	}
}

func TestAdminHandlersSuccessWithDatabaseHooks(t *testing.T) {
	now := time.Now().UTC().Round(0)

	t.Run("list identities success", func(t *testing.T) {
		s := newHookedServer(t)
		identityID := uuid.New()
		schemaID := uuid.New()
		s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
			if strings.Contains(sql, "FROM core_identities ci") {
				return adminTestRow{scanFn: func(dest ...any) error {
					*(dest[0].(*int64)) = 1
					return nil
				}}
			}
			return adminTestRow{scanFn: func(dest ...any) error { return errors.New("unexpected query") }}
		}
		s.dbQueryFn = func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			rows := &scriptedRows{
				scanFns: []func(dest ...any) error{
					func(dest ...any) error {
						*(dest[0].(*uuid.UUID)) = identityID
						*(dest[1].(*uuid.UUID)) = schemaID
						*(dest[2].(*[]byte)) = []byte(`{"email":"admin@example.com"}`)
						*(dest[3].(*string)) = "active"
						*(dest[4].(*time.Time)) = now
						*(dest[5].(*time.Time)) = now
						return nil
					},
				},
			}
			return rows, nil
		}

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/aegion/api/v1/identities?page=1&per_page=25", nil)
		s.handleAdminListIdentities(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
	})

	t.Run("create identity success", func(t *testing.T) {
		s := newHookedServer(t)
		schemaID := uuid.New()
		createdIdentityID := uuid.Nil

		s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
			switch {
			case strings.Contains(sql, "FROM core_identity_schemas"):
				return adminTestRow{scanFn: func(dest ...any) error {
					*(dest[0].(*uuid.UUID)) = schemaID
					return nil
				}}
			case strings.Contains(sql, "FROM core_identities"):
				return adminTestRow{scanFn: func(dest ...any) error {
					*(dest[0].(*uuid.UUID)) = createdIdentityID
					*(dest[1].(*uuid.UUID)) = schemaID
					*(dest[2].(*[]byte)) = []byte(`{"display_name":"Demo"}`)
					*(dest[3].(*string)) = "active"
					*(dest[4].(*time.Time)) = now
					*(dest[5].(*time.Time)) = now
					return nil
				}}
			default:
				return adminTestRow{scanFn: func(dest ...any) error { return errors.New("unexpected query") }}
			}
		}

		s.dbBeginFn = func(ctx context.Context) (pgx.Tx, error) {
			return &adminTestTx{
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					if strings.Contains(sql, "INSERT INTO core_identities") {
						createdIdentityID = args[0].(uuid.UUID)
						return pgconn.NewCommandTag("INSERT 0 1"), nil
					}
					return pgconn.CommandTag{}, errors.New("unexpected exec")
				},
			}, nil
		}

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/aegion/api/v1/identities", mustJSONBody(t, map[string]any{
			"schema_id": schemaID.String(),
			"traits":    map[string]any{"display_name": "Demo"},
			"state":     "active",
		}))
		s.handleAdminCreateIdentity(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected %d, got %d body=%s", http.StatusCreated, rec.Code, rec.Body.String())
		}
	})

	t.Run("get identity not found", func(t *testing.T) {
		s := newHookedServer(t)
		s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
		}
		rec := httptest.NewRecorder()
		req := withURLParam(httptest.NewRequest(http.MethodGet, "/aegion/api/v1/identities/x", nil), "id", uuid.NewString())
		s.handleAdminGetIdentity(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected %d, got %d", http.StatusNotFound, rec.Code)
		}
	})

	t.Run("update identity success", func(t *testing.T) {
		s := newHookedServer(t)
		identityID := uuid.New()
		schemaID := uuid.New()

		s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error {
				*(dest[0].(*uuid.UUID)) = identityID
				*(dest[1].(*uuid.UUID)) = schemaID
				*(dest[2].(*[]byte)) = []byte(`{"display_name":"Updated"}`)
				*(dest[3].(*string)) = "active"
				*(dest[4].(*time.Time)) = now
				*(dest[5].(*time.Time)) = now
				return nil
			}}
		}
		s.dbBeginFn = func(ctx context.Context) (pgx.Tx, error) {
			return &adminTestTx{
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					if strings.Contains(sql, "UPDATE core_identities") {
						return pgconn.NewCommandTag("UPDATE 1"), nil
					}
					return pgconn.CommandTag{}, errors.New("unexpected exec")
				},
			}, nil
		}

		rec := httptest.NewRecorder()
		req := withURLParam(httptest.NewRequest(http.MethodPatch, "/aegion/api/v1/identities/"+identityID.String(), mustJSONBody(t, map[string]any{
			"traits": map[string]any{"display_name": "Updated"},
		})), "id", identityID.String())
		s.handleAdminUpdateIdentity(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d body=%s", http.StatusOK, rec.Code, rec.Body.String())
		}
	})

	t.Run("delete identity success", func(t *testing.T) {
		s := newHookedServer(t)
		s.dbBeginFn = func(ctx context.Context) (pgx.Tx, error) {
			return &adminTestTx{
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					switch {
					case strings.Contains(sql, "UPDATE core_identities"):
						return pgconn.NewCommandTag("UPDATE 1"), nil
					case strings.Contains(sql, "UPDATE core_sessions"):
						return pgconn.NewCommandTag("UPDATE 2"), nil
					default:
						return pgconn.CommandTag{}, errors.New("unexpected exec")
					}
				},
			}, nil
		}

		rec := httptest.NewRecorder()
		req := withURLParam(httptest.NewRequest(http.MethodDelete, "/aegion/api/v1/identities/x", nil), "id", uuid.NewString())
		s.handleAdminDeleteIdentity(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected %d, got %d", http.StatusNoContent, rec.Code)
		}
	})

	t.Run("list sessions success", func(t *testing.T) {
		s := newHookedServer(t)
		sessionID := uuid.New()
		identityID := uuid.New()
		s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
			if strings.Contains(sql, "SELECT COUNT(*) FROM core_sessions cs") {
				return adminTestRow{scanFn: func(dest ...any) error {
					*(dest[0].(*int64)) = 1
					return nil
				}}
			}
			return adminTestRow{scanFn: func(dest ...any) error { return errors.New("unexpected query") }}
		}
		s.dbQueryFn = func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			rows := &scriptedRows{
				scanFns: []func(dest ...any) error{
					func(dest ...any) error {
						*(dest[0].(*uuid.UUID)) = sessionID
						*(dest[1].(*uuid.UUID)) = identityID
						*(dest[2].(*string)) = "aal1"
						*(dest[3].(*bool)) = true
						*(dest[4].(*time.Time)) = now
						*(dest[5].(*time.Time)) = now.Add(30 * time.Minute)
						*(dest[6].(*time.Time)) = now
						*(dest[7].(*string)) = "203.0.113.1"
						*(dest[8].(*string)) = "test-agent"
						return nil
					},
				},
			}
			return rows, nil
		}

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/aegion/api/v1/sessions?page=1&per_page=10", nil)
		s.handleAdminListSessions(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d body=%s", http.StatusOK, rec.Code, rec.Body.String())
		}
	})

	t.Run("delete session already revoked and not found paths", func(t *testing.T) {
		s := newHookedServer(t)
		sessionID := uuid.New()

		s.dbExecFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}

		s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error {
				*(dest[0].(*bool)) = true
				return nil
			}}
		}

		rec := httptest.NewRecorder()
		req := withURLParam(httptest.NewRequest(http.MethodDelete, "/aegion/api/v1/sessions/"+sessionID.String(), nil), "id", sessionID.String())
		s.handleAdminDeleteSession(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected already revoked %d, got %d", http.StatusOK, rec.Code)
		}

		s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error {
				*(dest[0].(*bool)) = false
				return nil
			}}
		}
		rec = httptest.NewRecorder()
		req = withURLParam(httptest.NewRequest(http.MethodDelete, "/aegion/api/v1/sessions/"+sessionID.String(), nil), "id", sessionID.String())
		s.handleAdminDeleteSession(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected not found %d, got %d", http.StatusNotFound, rec.Code)
		}
	})

	t.Run("delete identity sessions success and not found", func(t *testing.T) {
		s := newHookedServer(t)
		identityID := uuid.New()

		s.dbExecFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("UPDATE 2"), nil
		}
		rec := httptest.NewRecorder()
		req := withURLParam(httptest.NewRequest(http.MethodDelete, "/aegion/api/v1/sessions/identity/"+identityID.String(), nil), "identityId", identityID.String())
		s.handleAdminDeleteIdentitySessions(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected success %d, got %d", http.StatusOK, rec.Code)
		}

		s.dbExecFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}
		s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error {
				*(dest[0].(*bool)) = false
				return nil
			}}
		}
		rec = httptest.NewRecorder()
		req = withURLParam(httptest.NewRequest(http.MethodDelete, "/aegion/api/v1/sessions/identity/"+identityID.String(), nil), "identityId", identityID.String())
		s.handleAdminDeleteIdentitySessions(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected not found %d, got %d", http.StatusNotFound, rec.Code)
		}
	})
}

func TestAdminHandlersAdditionalDatabaseBranchCoverage(t *testing.T) {
	now := time.Now().UTC().Round(0)

	t.Run("create identity branch errors", func(t *testing.T) {
		s := newHookedServer(t)
		schemaID := uuid.New()
		s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
			switch {
			case strings.Contains(sql, "FROM core_identity_schemas"):
				return adminTestRow{scanFn: func(dest ...any) error {
					*(dest[0].(*uuid.UUID)) = schemaID
					return nil
				}}
			case strings.Contains(sql, "FROM core_identities"):
				return adminTestRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
			default:
				return adminTestRow{scanFn: func(dest ...any) error { return errors.New("unexpected query") }}
			}
		}

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/aegion/api/v1/identities", mustJSONBody(t, map[string]any{
			"traits": map[string]any{"display_name": "Demo"},
			"state":  "disabled",
		}))
		s.handleAdminCreateIdentity(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected invalid state %d, got %d", http.StatusBadRequest, rec.Code)
		}

		s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
		}
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/aegion/api/v1/identities", mustJSONBody(t, map[string]any{
			"traits": map[string]any{"display_name": "Demo"},
			"state":  "active",
		}))
		s.handleAdminCreateIdentity(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected invalid schema %d, got %d", http.StatusBadRequest, rec.Code)
		}

		s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
			switch {
			case strings.Contains(sql, "FROM core_identity_schemas"):
				return adminTestRow{scanFn: func(dest ...any) error {
					*(dest[0].(*uuid.UUID)) = schemaID
					return nil
				}}
			case strings.Contains(sql, "FROM core_identities"):
				return adminTestRow{scanFn: func(dest ...any) error {
					*(dest[0].(*uuid.UUID)) = uuid.New()
					*(dest[1].(*uuid.UUID)) = schemaID
					*(dest[2].(*[]byte)) = []byte(`{}`)
					*(dest[3].(*string)) = "active"
					*(dest[4].(*time.Time)) = now
					*(dest[5].(*time.Time)) = now
					return nil
				}}
			default:
				return adminTestRow{scanFn: func(dest ...any) error { return errors.New("unexpected query") }}
			}
		}

		s.dbBeginFn = func(ctx context.Context) (pgx.Tx, error) {
			return &adminTestTx{
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					return pgconn.CommandTag{}, errors.New("insert failed")
				},
			}, nil
		}
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/aegion/api/v1/identities", mustJSONBody(t, map[string]any{
			"traits": map[string]any{"display_name": "Demo"},
			"state":  "active",
		}))
		s.handleAdminCreateIdentity(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected insert failure %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		s.dbBeginFn = func(ctx context.Context) (pgx.Tx, error) {
			return &adminTestTx{
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					return pgconn.NewCommandTag("INSERT 0 1"), nil
				},
				commitFn: func(ctx context.Context) error { return errors.New("commit failed") },
			}, nil
		}
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/aegion/api/v1/identities", mustJSONBody(t, map[string]any{
			"traits": map[string]any{"display_name": "Demo"},
			"state":  "active",
		}))
		s.handleAdminCreateIdentity(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected commit failure %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		// Email trait path executes upsert branch and then fails to load created identity.
		createdIdentityID := uuid.Nil
		s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
			switch {
			case strings.Contains(sql, "FROM core_identity_schemas"):
				return adminTestRow{scanFn: func(dest ...any) error {
					*(dest[0].(*uuid.UUID)) = schemaID
					return nil
				}}
			case strings.Contains(sql, "FROM core_identities"):
				return adminTestRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
			default:
				return adminTestRow{scanFn: func(dest ...any) error { return errors.New("unexpected query") }}
			}
		}
		s.dbBeginFn = func(ctx context.Context) (pgx.Tx, error) {
			call := 0
			return &adminTestTx{
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					call++
					switch call {
					case 1:
						createdIdentityID = args[0].(uuid.UUID)
						return pgconn.NewCommandTag("INSERT 0 1"), nil
					case 2:
						return pgconn.NewCommandTag("UPDATE 0"), nil
					default:
						return pgconn.NewCommandTag("INSERT 0 1"), nil
					}
				},
			}, nil
		}
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/aegion/api/v1/identities", mustJSONBody(t, map[string]any{
			"schema_id": schemaID.String(),
			"traits": map[string]any{
				"email": "owner@example.com",
			},
			"state": "active",
		}))
		s.handleAdminCreateIdentity(rec, req)
		if createdIdentityID == uuid.Nil {
			t.Fatal("expected created identity id to be assigned")
		}
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected created identity lookup failure %d, got %d", http.StatusInternalServerError, rec.Code)
		}
	})

	t.Run("update identity additional branches", func(t *testing.T) {
		s := newHookedServer(t)
		identityID := uuid.New()
		schemaID := uuid.New()
		s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error {
				*(dest[0].(*uuid.UUID)) = identityID
				*(dest[1].(*uuid.UUID)) = schemaID
				*(dest[2].(*[]byte)) = []byte(`{}`)
				*(dest[3].(*string)) = "active"
				*(dest[4].(*time.Time)) = now
				*(dest[5].(*time.Time)) = now
				return nil
			}}
		}

		rec := httptest.NewRecorder()
		req := withURLParam(httptest.NewRequest(http.MethodPatch, "/aegion/api/v1/identities/"+identityID.String(), mustJSONBody(t, map[string]any{
			"state": " ",
		})), "id", identityID.String())
		s.handleAdminUpdateIdentity(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected empty state %d, got %d", http.StatusBadRequest, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = withURLParam(httptest.NewRequest(http.MethodPatch, "/aegion/api/v1/identities/"+identityID.String(), mustJSONBody(t, map[string]any{
			"state": "disabled",
		})), "id", identityID.String())
		s.handleAdminUpdateIdentity(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected invalid state %d, got %d", http.StatusBadRequest, rec.Code)
		}

		s.dbBeginFn = func(ctx context.Context) (pgx.Tx, error) {
			return &adminTestTx{
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					return pgconn.NewCommandTag("UPDATE 0"), nil
				},
			}, nil
		}
		rec = httptest.NewRecorder()
		req = withURLParam(httptest.NewRequest(http.MethodPatch, "/aegion/api/v1/identities/"+identityID.String(), mustJSONBody(t, map[string]any{
			"traits": map[string]any{"display_name": "Updated"},
		})), "id", identityID.String())
		s.handleAdminUpdateIdentity(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected not found %d, got %d", http.StatusNotFound, rec.Code)
		}

		s.dbBeginFn = func(ctx context.Context) (pgx.Tx, error) {
			return &adminTestTx{
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					return pgconn.NewCommandTag("UPDATE 1"), nil
				},
				commitFn: func(ctx context.Context) error { return errors.New("commit failed") },
			}, nil
		}
		rec = httptest.NewRecorder()
		req = withURLParam(httptest.NewRequest(http.MethodPatch, "/aegion/api/v1/identities/"+identityID.String(), mustJSONBody(t, map[string]any{
			"traits": map[string]any{"display_name": "Updated"},
		})), "id", identityID.String())
		s.handleAdminUpdateIdentity(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected commit failure %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
		}
		s.dbBeginFn = func(ctx context.Context) (pgx.Tx, error) {
			return &adminTestTx{
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					return pgconn.NewCommandTag("UPDATE 1"), nil
				},
			}, nil
		}
		rec = httptest.NewRecorder()
		req = withURLParam(httptest.NewRequest(http.MethodPatch, "/aegion/api/v1/identities/"+identityID.String(), mustJSONBody(t, map[string]any{
			"traits": map[string]any{"display_name": "Updated"},
		})), "id", identityID.String())
		s.handleAdminUpdateIdentity(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected not found after update %d, got %d", http.StatusNotFound, rec.Code)
		}

		// Email trait path with upsert failure.
		s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error {
				*(dest[0].(*uuid.UUID)) = identityID
				*(dest[1].(*uuid.UUID)) = schemaID
				*(dest[2].(*[]byte)) = []byte(`{}`)
				*(dest[3].(*string)) = "active"
				*(dest[4].(*time.Time)) = now
				*(dest[5].(*time.Time)) = now
				return nil
			}}
		}
		s.dbBeginFn = func(ctx context.Context) (pgx.Tx, error) {
			call := 0
			return &adminTestTx{
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					call++
					if call == 1 {
						return pgconn.NewCommandTag("UPDATE 1"), nil
					}
					return pgconn.CommandTag{}, errors.New("upsert failed")
				},
			}, nil
		}
		rec = httptest.NewRecorder()
		req = withURLParam(httptest.NewRequest(http.MethodPatch, "/aegion/api/v1/identities/"+identityID.String(), mustJSONBody(t, map[string]any{
			"traits": map[string]any{"email": "new@example.com"},
		})), "id", identityID.String())
		s.handleAdminUpdateIdentity(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected upsert failure %d, got %d", http.StatusInternalServerError, rec.Code)
		}
	})

	t.Run("delete identity additional branches", func(t *testing.T) {
		s := newHookedServer(t)
		identityID := uuid.New()

		s.dbBeginFn = func(ctx context.Context) (pgx.Tx, error) {
			return &adminTestTx{
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					return pgconn.CommandTag{}, errors.New("exec failed")
				},
			}, nil
		}
		rec := httptest.NewRecorder()
		req := withURLParam(httptest.NewRequest(http.MethodDelete, "/aegion/api/v1/identities/"+identityID.String(), nil), "id", identityID.String())
		s.handleAdminDeleteIdentity(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected exec failure %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		s.dbBeginFn = func(ctx context.Context) (pgx.Tx, error) {
			return &adminTestTx{
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					return pgconn.NewCommandTag("UPDATE 0"), nil
				},
			}, nil
		}
		rec = httptest.NewRecorder()
		req = withURLParam(httptest.NewRequest(http.MethodDelete, "/aegion/api/v1/identities/"+identityID.String(), nil), "id", identityID.String())
		s.handleAdminDeleteIdentity(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected not found %d, got %d", http.StatusNotFound, rec.Code)
		}

		callCount := 0
		s.dbBeginFn = func(ctx context.Context) (pgx.Tx, error) {
			return &adminTestTx{
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					callCount++
					if callCount == 1 {
						return pgconn.NewCommandTag("UPDATE 1"), nil
					}
					return pgconn.CommandTag{}, errors.New("session revoke failed")
				},
			}, nil
		}
		rec = httptest.NewRecorder()
		req = withURLParam(httptest.NewRequest(http.MethodDelete, "/aegion/api/v1/identities/"+identityID.String(), nil), "id", identityID.String())
		s.handleAdminDeleteIdentity(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected session revoke failure %d, got %d", http.StatusInternalServerError, rec.Code)
		}
	})

	t.Run("list identities and sessions row processing failures", func(t *testing.T) {
		s := newHookedServer(t)
		s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error {
				*(dest[0].(*int64)) = 1
				return nil
			}}
		}
		s.dbQueryFn = func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &scriptedRows{
				scanFns: []func(dest ...any) error{
					func(dest ...any) error { return errors.New("scan failed") },
				},
			}, nil
		}

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/aegion/api/v1/identities", nil)
		s.handleAdminListIdentities(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected identity scan failure %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/aegion/api/v1/sessions", nil)
		s.handleAdminListSessions(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected session scan failure %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/aegion/api/v1/sessions?identity_id=not-a-uuid", nil)
		s.handleAdminListSessions(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected invalid identity filter %d, got %d", http.StatusBadRequest, rec.Code)
		}

		s.dbQueryFn = func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &scriptedRows{err: errors.New("rows iteration failed")}, nil
		}
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/aegion/api/v1/identities", nil)
		s.handleAdminListIdentities(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected identity rows err %d, got %d", http.StatusInternalServerError, rec.Code)
		}
	})
}

func TestDatabaseAccessHelpers(t *testing.T) {
	s := newTestServer(t)
	if s.hasDatabaseAccess() {
		t.Fatal("expected no database access by default")
	}

	row := s.queryRow(context.Background(), "SELECT 1")
	if err := row.Scan(new(int)); err == nil {
		t.Fatal("expected queryRow scan error without database access")
	}

	if _, err := s.query(context.Background(), "SELECT 1"); err == nil {
		t.Fatal("expected query error without database access")
	}
	if _, err := s.exec(context.Background(), "SELECT 1"); err == nil {
		t.Fatal("expected exec error without database access")
	}
	if _, err := s.begin(context.Background()); err == nil {
		t.Fatal("expected begin error without database access")
	}
}

func TestRuntimePolicyModelHelperBranches(t *testing.T) {
	settings := runtimePolicySettings{}
	settings.RBAC.Enabled = true
	settings.ABAC.Enabled = false
	settings.ReBAC.Enabled = false

	if !runtimePolicyModelEnabled(settings, "rbac") {
		t.Fatal("expected rbac to be enabled")
	}
	if runtimePolicyModelEnabled(settings, "abac") {
		t.Fatal("expected abac to be disabled")
	}
	if runtimePolicyModelEnabled(settings, "rebac") {
		t.Fatal("expected rebac to be disabled")
	}
	if runtimePolicyModelEnabled(settings, "unknown") {
		t.Fatal("expected unknown model to be disabled")
	}

	if got := firstEnabledRuntimePolicyModel(settings); got != "rbac" {
		t.Fatalf("expected first enabled model rbac, got %q", got)
	}
	settings.RBAC.Enabled = false
	settings.ABAC.Enabled = true
	if got := firstEnabledRuntimePolicyModel(settings); got != "abac" {
		t.Fatalf("expected first enabled model abac, got %q", got)
	}
	settings.ABAC.Enabled = false
	settings.ReBAC.Enabled = true
	if got := firstEnabledRuntimePolicyModel(settings); got != "rebac" {
		t.Fatalf("expected first enabled model rebac, got %q", got)
	}
	settings.ReBAC.Enabled = false
	if got := firstEnabledRuntimePolicyModel(settings); got != "rbac" {
		t.Fatalf("expected default model fallback rbac, got %q", got)
	}
}
