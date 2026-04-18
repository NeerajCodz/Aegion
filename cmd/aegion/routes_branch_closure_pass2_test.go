package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestRoutesBranchClosurePass2(t *testing.T) {
	t.Run("self-service rate limit supports nil config", func(t *testing.T) {
		cfg, ok := selfServiceRateLimitConfig(nil)
		if !ok {
			t.Fatal("expected nil config to return default rate limit config")
		}
		if cfg.Burst != defaultFlowRateLimit {
			t.Fatalf("expected default burst %d, got %d", defaultFlowRateLimit, cfg.Burst)
		}
	})

	t.Run("flow submit returns execution error when auth module fails", func(t *testing.T) {
		s, _ := newFlowServer(t)
		s.passwordAuth = &stubPasswordFlowService{verifyErr: errors.New("verify failed")}

		flow, err := s.flowService.CreateLoginFlow(context.Background(), "http://example.com/login")
		if err != nil {
			t.Fatalf("create login flow: %v", err)
		}

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/self-service/login", mustJSONBody(t, map[string]any{
			"flow_id":    flow.ID.String(),
			"csrf_token": flow.CSRFToken,
			"identifier": "user@example.com",
			"password":   "Password1!",
		}))
		req.Header.Set("Content-Type", "application/json")

		s.handleFlowSubmit(rec, req, flow.Type)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
	})

	t.Run("module deregister requires module identity context", func(t *testing.T) {
		s := newTestServer(t)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/internal/registry/deregister", mustJSONBody(t, map[string]any{
			"module_id": "password",
		}))
		s.handleModuleDeregister(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rec.Code)
		}
	})

	t.Run("admin get identity returns success payload", func(t *testing.T) {
		s := newHookedServer(t)
		identityID := uuid.New()
		schemaID := uuid.New()
		now := time.Now().UTC().Round(0)

		s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error {
				*(dest[0].(*uuid.UUID)) = identityID
				*(dest[1].(*uuid.UUID)) = schemaID
				*(dest[2].(*[]byte)) = []byte(`{"email":"person@example.com"}`)
				*(dest[3].(*string)) = "active"
				*(dest[4].(*time.Time)) = now
				*(dest[5].(*time.Time)) = now
				return nil
			}}
		}

		rec := httptest.NewRecorder()
		req := withURLParam(httptest.NewRequest(http.MethodGet, "/aegion/api/v1/identities/"+identityID.String(), nil), "id", identityID.String())
		s.handleAdminGetIdentity(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d body=%s", http.StatusOK, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), identityID.String()) {
			t.Fatalf("expected identity id in response body, got %s", rec.Body.String())
		}
	})

	t.Run("admin update identity surfaces post-commit load error", func(t *testing.T) {
		s := newHookedServer(t)
		identityID := uuid.New()

		s.dbBeginFn = func(ctx context.Context) (pgx.Tx, error) {
			return &adminTestTx{
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					return pgconn.NewCommandTag("UPDATE 1"), nil
				},
			}, nil
		}
		s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
			return adminTestRow{scanFn: func(dest ...any) error { return errors.New("load failed") }}
		}

		rec := httptest.NewRecorder()
		req := withURLParam(httptest.NewRequest(http.MethodPatch, "/aegion/api/v1/identities/"+identityID.String(), mustJSONBody(t, map[string]any{
			"traits": map[string]any{"display_name": "updated"},
		})), "id", identityID.String())
		s.handleAdminUpdateIdentity(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
	})

	t.Run("admin create identity returns email upsert failure", func(t *testing.T) {
		s := newHookedServer(t)
		schemaID := uuid.New()

		s.dbQueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
			if strings.Contains(sql, "FROM core_identity_schemas") {
				return adminTestRow{scanFn: func(dest ...any) error {
					*(dest[0].(*uuid.UUID)) = schemaID
					return nil
				}}
			}
			return adminTestRow{scanFn: func(dest ...any) error { return errors.New("unexpected query") }}
		}
		s.dbBeginFn = func(ctx context.Context) (pgx.Tx, error) {
			return &adminTestTx{
				execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					switch {
					case strings.Contains(sql, "INSERT INTO core_identities"):
						return pgconn.NewCommandTag("INSERT 0 1"), nil
					case strings.Contains(sql, "UPDATE core_identity_addresses"):
						return pgconn.CommandTag{}, errors.New("email upsert failed")
					default:
						return pgconn.NewCommandTag("UPDATE 1"), nil
					}
				},
				rollbackFn: func(context.Context) error { return errors.New("rollback failed") },
			}, nil
		}

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/aegion/api/v1/identities", mustJSONBody(t, map[string]any{
			"schema_id": schemaID.String(),
			"state":     "active",
			"traits": map[string]any{
				"email": "user@example.com",
			},
		}))
		s.handleAdminCreateIdentity(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
	})
}
