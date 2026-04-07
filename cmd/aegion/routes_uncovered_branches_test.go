package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/aegion/aegion/core/flows"
)

type errReadCloser struct{}

func (errReadCloser) Read([]byte) (int, error) { return 0, errors.New("read failed") }
func (errReadCloser) Close() error             { return nil }

func TestSettingsInitHandlersAdditionalErrorBranches(t *testing.T) {
	t.Run("nil session manager returns 500", func(t *testing.T) {
		s := newTestServer(t)
		s.sessionManager = nil

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/self-service/settings/browser", nil)
		s.handleInitSettingsBrowser(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/api/v1/self-service/settings/api", nil)
		s.handleInitSettingsAPI(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
	})

	t.Run("unexpected session manager errors return 500", func(t *testing.T) {
		s := newTestServer(t)
		s.sessionManager = &stubRouteSessionManager{getErr: errors.New("session backend failure")}

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/self-service/settings/browser", nil)
		s.handleInitSettingsBrowser(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/api/v1/self-service/settings/api", nil)
		s.handleInitSettingsAPI(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
	})
}

func TestFlowSubmitAndInternalUIAdditionalBranches(t *testing.T) {
	t.Run("form parse failure surfaces bad request", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/submit", strings.NewReader("flow_id=%zz&csrf_token=x"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if _, _, err := parseFlowSubmitPayload(req); err == nil {
			t.Fatal("expected parse form error")
		}
	})

	t.Run("complete flow error returns 500", func(t *testing.T) {
		s, store := newFlowServer(t)
		created, err := s.flowService.CreateLoginFlow(context.Background(), "http://example.com/login")
		if err != nil {
			t.Fatalf("create login flow: %v", err)
		}
		store.updateErr = errors.New("store update failed")

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/self-service/login", mustJSONBody(t, map[string]any{
			"flow_id":    created.ID.String(),
			"csrf_token": created.CSRFToken,
		}))
		req.Header.Set("Content-Type", "application/json")
		s.handleFlowSubmit(rec, req, flows.TypeLogin)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
	})

	t.Run("internal update flow ui body read and decode failures", func(t *testing.T) {
		s := newTestServer(t)
		flowID := uuid.NewString()

		rec := httptest.NewRecorder()
		req := withURLParam(httptest.NewRequest(http.MethodPatch, "/internal/flows/"+flowID, nil), "id", flowID)
		req.Body = errReadCloser{}
		s.handleInternalUpdateFlowUI(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d for read error, got %d", http.StatusBadRequest, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = withURLParam(httptest.NewRequest(http.MethodPatch, "/internal/flows/"+flowID, strings.NewReader("   ")), "id", flowID)
		s.handleInternalUpdateFlowUI(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d for empty body, got %d", http.StatusBadRequest, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = withURLParam(httptest.NewRequest(http.MethodPatch, "/internal/flows/"+flowID, strings.NewReader(`{"nodes":"invalid"}`)), "id", flowID)
		s.handleInternalUpdateFlowUI(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d for ui decode failure, got %d", http.StatusBadRequest, rec.Code)
		}
	})
}

func TestModuleProxyAndIPExtractionAdditionalBranches(t *testing.T) {
	t.Run("module proxy falls back for non-positive timeout", func(t *testing.T) {
		s := newHookedServer(t)
		s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
			if len(args) == 0 {
				return adminTestRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
			}
			switch args[0] {
			case systemConfigKeyProxy:
				return adminTestRow{scanFn: func(dest ...any) error {
					*(dest[0].(*[]byte)) = []byte(`{"enabled":true,"upstream_timeout":"0s","identity_signature_header":"X-Aegion-Signature","signed_identity_headers":["X-User-ID"]}`)
					return nil
				}}
			case systemConfigKeyPolicy:
				return adminTestRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
			default:
				return adminTestRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
			}
		}

		rec := httptest.NewRecorder()
		req := withURLParam(httptest.NewRequest(http.MethodGet, "/modules/missing", nil), "moduleId", "missing")
		s.handleModuleProxy(rec, req)
		if rec.Code != http.StatusServiceUnavailable && rec.Code != http.StatusNotFound {
			t.Fatalf("expected proxy failure status for missing module target, got %d", rec.Code)
		}
	})

	t.Run("extract request ip returns trimmed addr when host:port parse fails", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "[2001:db8::3]"
		if got := extractRequestIP(req); got != "2001:db8::3" {
			t.Fatalf("expected fallback trimmed remote addr, got %q", got)
		}
	})
}

func TestAdminHandlersAdditionalRouteBranches(t *testing.T) {
	now := time.Now().UTC().Round(0)

	t.Run("handlers requiring db return 503 when unavailable", func(t *testing.T) {
		s := newTestServer(t)
		validID := uuid.NewString()

		cases := []struct {
			name   string
			call   func(*httptest.ResponseRecorder)
			expect int
		}{
			{
				name: "get identity",
				call: func(rec *httptest.ResponseRecorder) {
					req := withURLParam(httptest.NewRequest(http.MethodGet, "/aegion/api/v1/identities/"+validID, nil), "id", validID)
					s.handleAdminGetIdentity(rec, req)
				},
				expect: http.StatusServiceUnavailable,
			},
			{
				name: "update identity",
				call: func(rec *httptest.ResponseRecorder) {
					req := withURLParam(httptest.NewRequest(http.MethodPatch, "/aegion/api/v1/identities/"+validID, mustJSONBody(t, map[string]any{"state": "active"})), "id", validID)
					s.handleAdminUpdateIdentity(rec, req)
				},
				expect: http.StatusServiceUnavailable,
			},
			{
				name: "delete identity",
				call: func(rec *httptest.ResponseRecorder) {
					req := withURLParam(httptest.NewRequest(http.MethodDelete, "/aegion/api/v1/identities/"+validID, nil), "id", validID)
					s.handleAdminDeleteIdentity(rec, req)
				},
				expect: http.StatusServiceUnavailable,
			},
			{
				name: "delete session",
				call: func(rec *httptest.ResponseRecorder) {
					req := withURLParam(httptest.NewRequest(http.MethodDelete, "/aegion/api/v1/sessions/"+validID, nil), "id", validID)
					s.handleAdminDeleteSession(rec, req)
				},
				expect: http.StatusServiceUnavailable,
			},
			{
				name: "delete identity sessions",
				call: func(rec *httptest.ResponseRecorder) {
					req := withURLParam(httptest.NewRequest(http.MethodDelete, "/aegion/api/v1/sessions/identity/"+validID, nil), "identityId", validID)
					s.handleAdminDeleteIdentitySessions(rec, req)
				},
				expect: http.StatusServiceUnavailable,
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				rec := httptest.NewRecorder()
				tc.call(rec)
				if rec.Code != tc.expect {
					t.Fatalf("expected %d, got %d", tc.expect, rec.Code)
				}
			})
		}
	})

	t.Run("list identities query and decode branches", func(t *testing.T) {
		s := newHookedServer(t)
		identityID := uuid.New()
		schemaID := uuid.New()

		s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error {
				*(dest[0].(*int64)) = 1
				return nil
			}}
		}
		s.dbQueryFn = func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, errors.New("query failed")
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/aegion/api/v1/identities", nil)
		s.handleAdminListIdentities(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d for list query error, got %d", http.StatusInternalServerError, rec.Code)
		}

		s.dbQueryFn = func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &scriptedRows{
				scanFns: []func(dest ...any) error{
					func(dest ...any) error {
						*(dest[0].(*uuid.UUID)) = identityID
						*(dest[1].(*uuid.UUID)) = schemaID
						*(dest[2].(*[]byte)) = []byte(`{invalid-json`)
						*(dest[3].(*string)) = "active"
						*(dest[4].(*time.Time)) = now
						*(dest[5].(*time.Time)) = now
						return nil
					},
				},
			}, nil
		}
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/aegion/api/v1/identities", nil)
		s.handleAdminListIdentities(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d for invalid traits fallback, got %d", http.StatusOK, rec.Code)
		}
	})

	t.Run("create identity begin and post-create lookup error branches", func(t *testing.T) {
		s := newHookedServer(t)
		schemaID := uuid.New()
		createdID := uuid.Nil

		s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
			switch {
			case strings.Contains(sql, "FROM core_identity_schemas"):
				return adminTestRow{scanFn: func(dest ...any) error {
					*(dest[0].(*uuid.UUID)) = schemaID
					return nil
				}}
			case strings.Contains(sql, "FROM core_identities"):
				return adminTestRow{scanFn: func(dest ...any) error { return errors.New("lookup failed") }}
			default:
				return adminTestRow{scanFn: func(dest ...any) error { return errors.New("unexpected query") }}
			}
		}

		s.dbBeginFn = func(ctx context.Context) (pgx.Tx, error) {
			return nil, errors.New("begin failed")
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/aegion/api/v1/identities", mustJSONBody(t, map[string]any{
			"schema_id": schemaID.String(),
			"state":     "active",
		}))
		s.handleAdminCreateIdentity(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d for begin failure, got %d", http.StatusInternalServerError, rec.Code)
		}

		s.dbBeginFn = func(ctx context.Context) (pgx.Tx, error) {
			call := 0
			return &adminTestTx{
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					call++
					if call == 1 {
						createdID = args[0].(uuid.UUID)
						return pgconn.NewCommandTag("INSERT 0 1"), nil
					}
					return pgconn.NewCommandTag("INSERT 0 1"), nil
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
		if createdID == uuid.Nil {
			t.Fatal("expected identity insert to run")
		}
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d for post-create lookup failure, got %d", http.StatusInternalServerError, rec.Code)
		}
	})

	t.Run("update identity invalid id, state update, and error branches", func(t *testing.T) {
		s := newHookedServer(t)
		identityID := uuid.New()
		schemaID := uuid.New()

		rec := httptest.NewRecorder()
		req := withURLParam(httptest.NewRequest(http.MethodPatch, "/aegion/api/v1/identities/not-a-uuid", mustJSONBody(t, map[string]any{
			"state": "active",
		})), "id", "not-a-uuid")
		s.handleAdminUpdateIdentity(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d for invalid id, got %d", http.StatusBadRequest, rec.Code)
		}

		s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error {
				*(dest[0].(*uuid.UUID)) = identityID
				*(dest[1].(*uuid.UUID)) = schemaID
				*(dest[2].(*[]byte)) = []byte(`{"name":"demo"}`)
				*(dest[3].(*string)) = "inactive"
				*(dest[4].(*time.Time)) = now
				*(dest[5].(*time.Time)) = now
				return nil
			}}
		}

		sawStateUpdate := false
		s.dbBeginFn = func(ctx context.Context) (pgx.Tx, error) {
			return &adminTestTx{
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					if strings.Contains(sql, "UPDATE core_identities") && strings.Contains(sql, "state = $1") {
						sawStateUpdate = true
					}
					return pgconn.NewCommandTag("UPDATE 1"), nil
				},
			}, nil
		}

		rec = httptest.NewRecorder()
		req = withURLParam(httptest.NewRequest(http.MethodPatch, "/aegion/api/v1/identities/"+identityID.String(), mustJSONBody(t, map[string]any{
			"state": "inactive",
		})), "id", identityID.String())
		s.handleAdminUpdateIdentity(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d for state update path, got %d", http.StatusOK, rec.Code)
		}
		if !sawStateUpdate {
			t.Fatal("expected state update clause to be used")
		}

		s.dbBeginFn = func(ctx context.Context) (pgx.Tx, error) {
			return &adminTestTx{
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					return pgconn.CommandTag{}, errors.New("update failed")
				},
				rollbackFn: func(ctx context.Context) error { return errors.New("rollback failed") },
			}, nil
		}
		rec = httptest.NewRecorder()
		req = withURLParam(httptest.NewRequest(http.MethodPatch, "/aegion/api/v1/identities/"+identityID.String(), mustJSONBody(t, map[string]any{
			"traits": map[string]any{"display_name": "Updated"},
		})), "id", identityID.String())
		s.handleAdminUpdateIdentity(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d for update exec failure, got %d", http.StatusInternalServerError, rec.Code)
		}
	})

	t.Run("delete identity commit and rollback branches", func(t *testing.T) {
		s := newHookedServer(t)
		identityID := uuid.New()

		s.dbBeginFn = func(ctx context.Context) (pgx.Tx, error) {
			return &adminTestTx{
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					return pgconn.CommandTag{}, errors.New("delete failed")
				},
				rollbackFn: func(ctx context.Context) error { return errors.New("rollback failed") },
			}, nil
		}
		rec := httptest.NewRecorder()
		req := withURLParam(httptest.NewRequest(http.MethodDelete, "/aegion/api/v1/identities/"+identityID.String(), nil), "id", identityID.String())
		s.handleAdminDeleteIdentity(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d for delete exec failure, got %d", http.StatusInternalServerError, rec.Code)
		}

		call := 0
		s.dbBeginFn = func(ctx context.Context) (pgx.Tx, error) {
			return &adminTestTx{
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					call++
					if call == 1 {
						return pgconn.NewCommandTag("UPDATE 1"), nil
					}
					return pgconn.NewCommandTag("UPDATE 1"), nil
				},
				commitFn: func(ctx context.Context) error { return errors.New("commit failed") },
			}, nil
		}
		rec = httptest.NewRecorder()
		req = withURLParam(httptest.NewRequest(http.MethodDelete, "/aegion/api/v1/identities/"+identityID.String(), nil), "id", identityID.String())
		s.handleAdminDeleteIdentity(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d for commit failure, got %d", http.StatusInternalServerError, rec.Code)
		}
	})

	t.Run("session handlers query branches", func(t *testing.T) {
		s := newHookedServer(t)
		identityID := uuid.New()
		sessionID := uuid.New()

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
			return nil, errors.New("sessions query failed")
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/aegion/api/v1/sessions?identity_id="+identityID.String(), nil)
		s.handleAdminListSessions(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d for list sessions query error, got %d", http.StatusInternalServerError, rec.Code)
		}

		s.dbQueryFn = func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &scriptedRows{err: errors.New("rows failed")}, nil
		}
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/aegion/api/v1/sessions", nil)
		s.handleAdminListSessions(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d for rows iteration error, got %d", http.StatusInternalServerError, rec.Code)
		}

		s.dbExecFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}
		s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error { return errors.New("exists query failed") }}
		}
		rec = httptest.NewRecorder()
		req = withURLParam(httptest.NewRequest(http.MethodDelete, "/aegion/api/v1/sessions/"+sessionID.String(), nil), "id", sessionID.String())
		s.handleAdminDeleteSession(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d for exists query failure, got %d", http.StatusInternalServerError, rec.Code)
		}

		s.dbExecFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("UPDATE 1"), nil
		}
		rec = httptest.NewRecorder()
		req = withURLParam(httptest.NewRequest(http.MethodDelete, "/aegion/api/v1/sessions/"+sessionID.String(), nil), "id", sessionID.String())
		s.handleAdminDeleteSession(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d for revoke success, got %d", http.StatusOK, rec.Code)
		}

		s.dbExecFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}
		s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error { return errors.New("identity exists query failed") }}
		}
		rec = httptest.NewRecorder()
		req = withURLParam(httptest.NewRequest(http.MethodDelete, "/aegion/api/v1/sessions/identity/"+identityID.String(), nil), "identityId", identityID.String())
		s.handleAdminDeleteIdentitySessions(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d for identity exists query failure, got %d", http.StatusInternalServerError, rec.Code)
		}
	})
}

func TestRuntimeConfigAndMetricsAdditionalBranches(t *testing.T) {
	t.Run("resolve schema by name and get identity traits fallback", func(t *testing.T) {
		s := newHookedServer(t)
		schemaID := uuid.New()
		identityID := uuid.New()

		s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
			switch {
			case strings.Contains(sql, "WHERE name = $1"):
				return adminTestRow{scanFn: func(dest ...any) error {
					*(dest[0].(*uuid.UUID)) = schemaID
					return nil
				}}
			case strings.Contains(sql, "FROM core_identities"):
				return adminTestRow{scanFn: func(dest ...any) error {
					*(dest[0].(*uuid.UUID)) = identityID
					*(dest[1].(*uuid.UUID)) = schemaID
					*(dest[2].(*[]byte)) = []byte(`{`)
					*(dest[3].(*string)) = "active"
					*(dest[4].(*time.Time)) = time.Now().UTC()
					*(dest[5].(*time.Time)) = time.Now().UTC()
					return nil
				}}
			default:
				return adminTestRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
			}
		}

		gotSchemaID, err := s.resolveAdminSchemaID(context.Background(), "default")
		if err != nil {
			t.Fatalf("resolveAdminSchemaID by name failed: %v", err)
		}
		if gotSchemaID != schemaID {
			t.Fatalf("expected schema %s, got %s", schemaID, gotSchemaID)
		}

		identity, found, err := s.getAdminIdentityByID(context.Background(), identityID)
		if err != nil {
			t.Fatalf("getAdminIdentityByID failed: %v", err)
		}
		if !found || identity == nil {
			t.Fatalf("expected identity to be found")
		}
		if len(identity.Traits) != 0 {
			t.Fatalf("expected invalid traits json fallback to empty map, got %#v", identity.Traits)
		}
	})

	t.Run("admin config handlers return expected errors", func(t *testing.T) {
		s := newHookedServer(t)
		s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error { return errors.New("policy load failed") }}
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/aegion/api/v1/system/config", nil)
		s.handleAdminGetConfig(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d when policy config load fails, got %d", http.StatusInternalServerError, rec.Code)
		}

		s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
			if len(args) > 0 && args[0] == systemConfigKeyPolicy {
				return adminTestRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
			}
			return adminTestRow{scanFn: func(dest ...any) error { return errors.New("proxy load failed") }}
		}
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/aegion/api/v1/system/config", nil)
		s.handleAdminGetConfig(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d when proxy config load fails, got %d", http.StatusInternalServerError, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPatch, "/aegion/api/v1/system/config", strings.NewReader(`{"policy":{"enabled":true}}{"proxy":{"enabled":false}}`))
		s.handleAdminUpdateConfig(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d for multi-object payload, got %d", http.StatusBadRequest, rec.Code)
		}
	})

	t.Run("runtime settings validation and metrics query errors", func(t *testing.T) {
		s := newHookedServer(t)
		s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
			if len(args) > 0 && args[0] == systemConfigKeyPolicy {
				return adminTestRow{scanFn: func(dest ...any) error {
					*(dest[0].(*[]byte)) = []byte(`{"enabled":true,"default_model":"abac","rbac":{"enabled":false},"abac":{"enabled":false},"rebac":{"enabled":false}}`)
					return nil
				}}
			}
			return adminTestRow{scanFn: func(dest ...any) error { return errors.New("identity metrics failed") }}
		}

		if _, err := s.loadRuntimePolicySettings(context.Background()); err == nil {
			t.Fatal("expected invalid runtime policy settings error")
		}
		invalidPolicy := runtimePolicySettings{
			Enabled:      true,
			DefaultModel: "rbac",
		}
		if err := validateRuntimePolicySettings(invalidPolicy); err == nil {
			t.Fatal("expected policy model enabled validation error")
		}
		if err := validateRuntimeProxySettings(runtimeProxySettings{
			UpstreamTimeout:         "10s",
			IdentitySignatureHeader: "X-Aegion-Signature",
			SignedIdentityHeaders:   []string{"X-User-ID", "x-user-id", "X-User-Session-ID"},
			IdentitySigningSecret:   "0123456789abcdef",
		}); err != nil {
			t.Fatalf("expected duplicate signed headers to normalize without error, got %v", err)
		}

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/aegion/api/v1/system/metrics", nil)
		s.handleAdminMetrics(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d for identity metrics query failure, got %d", http.StatusInternalServerError, rec.Code)
		}

		s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
			if strings.Contains(sql, "FROM core_identities") {
				return adminTestRow{scanFn: func(dest ...any) error {
					*(dest[0].(*int64)) = 1
					return nil
				}}
			}
			return adminTestRow{scanFn: func(dest ...any) error { return errors.New("session metrics failed") }}
		}
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/aegion/api/v1/system/metrics", nil)
		s.handleAdminMetrics(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d for session metrics query failure, got %d", http.StatusInternalServerError, rec.Code)
		}
	})

	t.Run("handle admin update config runtime load/save error branches", func(t *testing.T) {
		s := newHookedServer(t)

		s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error { return errors.New("load failed") }}
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/aegion/api/v1/system/config", mustJSONBody(t, map[string]any{
			"policy": map[string]any{"enabled": true},
		}))
		s.handleAdminUpdateConfig(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d for policy load failure, got %d", http.StatusInternalServerError, rec.Code)
		}

		s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
			if len(args) > 0 && args[0] == systemConfigKeyProxy {
				return adminTestRow{scanFn: func(dest ...any) error { return errors.New("proxy load failed") }}
			}
			return adminTestRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
		}
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPatch, "/aegion/api/v1/system/config", mustJSONBody(t, map[string]any{
			"proxy": map[string]any{"enabled": true},
		}))
		s.handleAdminUpdateConfig(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d for proxy load failure, got %d", http.StatusInternalServerError, rec.Code)
		}

		s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
		}
		s.dbExecFn = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, errors.New("persist failed")
		}
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPatch, "/aegion/api/v1/system/config", mustJSONBody(t, map[string]any{
			"proxy": map[string]any{"enabled": true},
		}))
		s.handleAdminUpdateConfig(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d for proxy persist failure, got %d", http.StatusInternalServerError, rec.Code)
		}
	})

	t.Run("config update empty payload", func(t *testing.T) {
		s := newHookedServer(t)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/aegion/api/v1/system/config", strings.NewReader(`{}`))
		s.handleAdminUpdateConfig(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d for empty patch payload, got %d", http.StatusBadRequest, rec.Code)
		}
	})

	t.Run("config update malformed body", func(t *testing.T) {
		s := newHookedServer(t)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/aegion/api/v1/system/config", strings.NewReader(`{`))
		s.handleAdminUpdateConfig(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d for malformed payload, got %d", http.StatusBadRequest, rec.Code)
		}
	})

	t.Run("config update unknown field", func(t *testing.T) {
		s := newHookedServer(t)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/aegion/api/v1/system/config", strings.NewReader(`{"unknown":true}`))
		s.handleAdminUpdateConfig(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d for unknown field payload, got %d", http.StatusBadRequest, rec.Code)
		}
	})

	t.Run("admin metrics unavailable db branch", func(t *testing.T) {
		s := newTestServer(t)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/aegion/api/v1/system/metrics", nil)
		s.handleAdminMetrics(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d when db unavailable, got %d", http.StatusOK, rec.Code)
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode metrics response: %v", err)
		}
		if body["database"] != "unavailable" {
			t.Fatalf("expected unavailable db status, got %v", body["database"])
		}
	})

	t.Run("admin get config success identity signing secret flag false", func(t *testing.T) {
		s := newHookedServer(t)
		s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
			switch args[0] {
			case systemConfigKeyPolicy:
				return adminTestRow{scanFn: func(dest ...any) error {
					*(dest[0].(*[]byte)) = []byte(`{"enabled":true,"default_model":"rbac","rbac":{"enabled":true},"abac":{"enabled":false},"rebac":{"enabled":false}}`)
					return nil
				}}
			case systemConfigKeyProxy:
				return adminTestRow{scanFn: func(dest ...any) error {
					*(dest[0].(*[]byte)) = []byte(`{"enabled":true,"upstream_timeout":"30s","preserve_host":false,"strip_inbound_identity_headers":true,"identity_signing_secret":"","identity_signature_header":"X-Aegion-Signature","signed_identity_headers":["X-User-ID"]}`)
					return nil
				}}
			default:
				return adminTestRow{scanFn: func(dest ...any) error { return pgx.ErrNoRows }}
			}
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/aegion/api/v1/system/config", nil)
		s.handleAdminGetConfig(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
	})
}

func TestReadAllFailureInInternalUpdateFlowUI(t *testing.T) {
	s := newTestServer(t)
	flowID := uuid.NewString()

	rec := httptest.NewRecorder()
	req := withURLParam(httptest.NewRequest(http.MethodPatch, "/internal/flows/"+flowID, nil), "id", flowID)
	req.Body = io.NopCloser(errReadCloser{})
	s.handleInternalUpdateFlowUI(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected %d for body read failure, got %d", http.StatusBadRequest, rec.Code)
	}
}
