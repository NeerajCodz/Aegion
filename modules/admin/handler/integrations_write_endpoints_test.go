package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func jsonBody(t *testing.T, value any) *bytes.Buffer {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json body: %v", err)
	}
	return bytes.NewBuffer(raw)
}

func TestIntegrationsWriteEndpointsCoverage(t *testing.T) {
	t.Run("sso connection create and delete", func(t *testing.T) {
		h := newIntegrationsHandler()

		rec := httptest.NewRecorder()
		req := withOperator(httptest.NewRequest(http.MethodPost, "/admin/integrations/sso/connections", jsonBody(t, map[string]any{
			"slug":         "acme",
			"display_name": "",
			"entity_id":    "urn:acme",
			"sso_url":      "https://idp.example.com/sso",
		})))
		h.UpsertSSOConnection(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		now := time.Now().UTC()
		connID := uuid.New()
		h.db = &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return fakeRow{vals: []any{connID, now, now}}
			},
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("DELETE 1"), nil
			},
		}

		rec = httptest.NewRecorder()
		req = withOperator(httptest.NewRequest(http.MethodPost, "/admin/integrations/sso/connections", jsonBody(t, map[string]any{
			"slug":                "Acme",
			"display_name":        "Acme SSO",
			"entity_id":           "urn:acme",
			"sso_url":             "https://idp.example.com/sso",
			"certificate_pem":     "pem",
			"metadata_url":        "https://idp.example.com/metadata",
			"domains":             []string{"acme.example"},
			"attribute_mapping":   map[string]string{"email": "mail"},
			"jit_provisioning":    true,
			"default_redirect_to": "/app",
			"extra_authn_context": map[string]string{
				"acr": "urn:mfa",
			},
			"enabled": true,
		})))
		h.UpsertSSOConnection(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d body=%s", http.StatusOK, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"slug":"acme"`) {
			t.Fatalf("expected normalized slug in response, got %s", rec.Body.String())
		}

		rec = httptest.NewRecorder()
		req = withOperator(withRouteParam(httptest.NewRequest(http.MethodDelete, "/admin/integrations/sso/connections/acme", nil), "slug", "acme"))
		h.DeleteSSOConnection(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected %d, got %d", http.StatusNoContent, rec.Code)
		}
	})

	t.Run("proxy upstream create and delete", func(t *testing.T) {
		h := newIntegrationsHandler()

		rec := httptest.NewRecorder()
		req := withOperator(httptest.NewRequest(http.MethodPost, "/admin/integrations/proxy/upstreams", jsonBody(t, map[string]any{
			"name": "",
			"url":  "",
		})))
		h.UpsertProxyUpstream(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		now := time.Now().UTC()
		upstreamID := uuid.New()
		h.db = &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return fakeRow{vals: []any{upstreamID, now, now}}
			},
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("DELETE 1"), nil
			},
		}

		rec = httptest.NewRecorder()
		req = withOperator(httptest.NewRequest(http.MethodPost, "/admin/integrations/proxy/upstreams", jsonBody(t, map[string]any{
			"name":            "API",
			"url":             "https://api.example.com",
			"health_check":    "/healthz",
			"timeout":         "5s",
			"max_connections": 64,
			"headers":         map[string]string{"x-env": "prod"},
			"circuit_breaker": map[string]any{"enabled": true},
			"enabled":         true,
		})))
		h.UpsertProxyUpstream(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d body=%s", http.StatusOK, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"name":"api"`) {
			t.Fatalf("expected normalized upstream name in response, got %s", rec.Body.String())
		}

		rec = httptest.NewRecorder()
		req = withOperator(withRouteParam(httptest.NewRequest(http.MethodDelete, "/admin/integrations/proxy/upstreams/api", nil), "name", "api"))
		h.DeleteProxyUpstream(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected %d, got %d", http.StatusNoContent, rec.Code)
		}
	})

	t.Run("proxy route upsert delete and simulation", func(t *testing.T) {
		h := newIntegrationsHandler()
		now := time.Now().UTC()

		rec := httptest.NewRecorder()
		req := withOperator(httptest.NewRequest(http.MethodPost, "/admin/integrations/proxy/routes", jsonBody(t, map[string]any{
			"path":   "",
			"target": "",
		})))
		h.UpsertProxyRoute(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		routeID := "route-1"
		h.db = &fakeDB{
			queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				switch {
				case strings.Contains(sql, "INSERT INTO proxy_routes"):
					return fakeRow{vals: []any{now, now}}
				case strings.Contains(sql, "FROM proxy_upstreams"):
					return fakeRow{vals: []any{"api", "https://api.example.com", "/healthz", "5s", 64, true}}
				default:
					return fakeRow{err: pgx.ErrNoRows}
				}
			},
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &fakeRows{data: [][]any{
					{
						routeID,
						"/v1/profile",
						[]byte(`["GET"]`),
						true,
						"aal2",
						[]byte(`["profile:read"]`),
						[]byte(`{"requests":10}`),
						"api",
						100,
						[]byte(`{"x-test":"1"}`),
						[]byte(`{"strip_prefix":"/v1"}`),
						true,
						"profile route",
						now,
						now,
					},
				}}, nil
			},
			execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("DELETE 1"), nil
			},
		}

		rec = httptest.NewRecorder()
		req = withOperator(httptest.NewRequest(http.MethodPost, "/admin/integrations/proxy/routes", jsonBody(t, map[string]any{
			"id":           "",
			"path":         "/v1/profile",
			"methods":      []string{"GET"},
			"require_auth": true,
			"required_aal": "aal2",
			"capabilities": []string{"profile:read"},
			"rate_limit":   map[string]any{"requests": 10},
			"target":       "API",
			"priority":     100,
			"headers":      map[string]string{"x-test": "1"},
			"rewrite":      map[string]any{"strip_prefix": "/v1"},
			"enabled":      true,
			"description":  "profile route",
		})))
		h.UpsertProxyRoute(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d body=%s", http.StatusOK, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"target":"api"`) {
			t.Fatalf("expected normalized route target in response, got %s", rec.Body.String())
		}

		rec = httptest.NewRecorder()
		req = withOperator(withRouteParam(httptest.NewRequest(http.MethodDelete, "/admin/integrations/proxy/routes/"+routeID, nil), "id", routeID))
		h.DeleteProxyRoute(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected %d, got %d", http.StatusNoContent, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = withOperator(httptest.NewRequest(http.MethodPost, "/admin/integrations/proxy/simulate", jsonBody(t, map[string]any{
			"path":          "/v1/profile",
			"method":        "get",
			"authenticated": false,
		})))
		h.SimulateProxyRoute(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d body=%s", http.StatusOK, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"matched":true`) {
			t.Fatalf("expected matched route simulation response, got %s", rec.Body.String())
		}
	})
}
