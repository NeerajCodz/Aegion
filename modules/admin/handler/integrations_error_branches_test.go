package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	adminstore "github.com/aegion/aegion/modules/admin/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestIntegrationOverviewAdditionalFailureBranches(t *testing.T) {
	// First query failure is already covered; these target the remaining per-count failures.
	for _, failAt := range []int{2, 3, 4, 5, 6, 7, 8} {
		t.Run("fail_count_query_"+string(rune('0'+failAt)), func(t *testing.T) {
			h := newIntegrationsHandler()
			calls := 0
			h.db = &fakeDB{
				queryRowFn: func(context.Context, string, ...any) pgx.Row {
					calls++
					if calls == failAt {
						return fakeRow{err: errors.New("count failed")}
					}
					return fakeRow{vals: []any{int64(1)}}
				},
			}

			rec := httptest.NewRecorder()
			req := withOperator(httptest.NewRequest(http.MethodGet, "/admin/integrations/overview", nil))
			h.IntegrationOverview(rec, req)
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("expected 500 for failure at query %d, got %d", failAt, rec.Code)
			}
		})
	}
}

func TestSetupStatusAdminOperatorCountFailureBranch(t *testing.T) {
	h := newIntegrationsHandler()
	calls := 0
	h.db = &fakeDB{
		queryRowFn: func(context.Context, string, ...any) pgx.Row {
			calls++
			// Main setup-status query list has 16 COUNT calls before admin-operator count.
			if calls == 17 {
				return fakeRow{err: errors.New("admin operator count failed")}
			}
			return fakeRow{vals: []any{int64(1)}}
		},
	}

	rec := httptest.NewRecorder()
	req := withOperator(httptest.NewRequest(http.MethodGet, "/admin/integrations/setup/status", nil))
	h.SetupStatus(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when admin operator count fails, got %d", rec.Code)
	}
}

func TestRBACSummaryFailureBranches(t *testing.T) {
	operator := &adminstore.Operator{ID: uuid.New(), Role: "admin"}
	req := httptest.NewRequest(http.MethodGet, "/admin/integrations/rbac/summary", nil)
	req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))

	t.Run("list roles failure", func(t *testing.T) {
		h := New(&fakeService{
			store: &fakeStore{},
			listRolesFn: func(context.Context, uuid.UUID, int, int) ([]*adminstore.Role, int64, error) {
				return nil, 0, errors.New("list failed")
			},
		})
		rec := httptest.NewRecorder()
		h.RBACSummary(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 on list roles failure, got %d", rec.Code)
		}
	})

	t.Run("role operator count failure", func(t *testing.T) {
		h := New(&fakeService{
			store: &fakeStore{},
			listRolesFn: func(context.Context, uuid.UUID, int, int) ([]*adminstore.Role, int64, error) {
				return []*adminstore.Role{{ID: uuid.New(), Name: "admin"}}, 1, nil
			},
		})
		h.db = &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{err: errors.New("count failed")}
			},
		}
		rec := httptest.NewRecorder()
		h.RBACSummary(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 on role count failure, got %d", rec.Code)
		}
	})
}

func TestSocialProviderAdditionalBranches(t *testing.T) {
	t.Run("get provider requires slug", func(t *testing.T) {
		h := newIntegrationsHandler()
		h.socialProviders = &fakeSocialProviderManager{}
		rec := httptest.NewRecorder()
		req := withOperator(withRouteParam(httptest.NewRequest(http.MethodGet, "/admin/integrations/social/providers", nil), "slug", " "))
		h.GetSocialProvider(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for missing slug, got %d", rec.Code)
		}
	})

	t.Run("delete provider unavailable manager", func(t *testing.T) {
		h := newIntegrationsHandler()
		rec := httptest.NewRecorder()
		req := withOperator(withRouteParam(httptest.NewRequest(http.MethodDelete, "/admin/integrations/social/providers/google", nil), "slug", "google"))
		h.DeleteSocialProvider(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503 when provider manager unavailable, got %d", rec.Code)
		}
	})
}

func TestSSOIntegrationAdditionalBranches(t *testing.T) {
	t.Run("upsert invalid body and db failure", func(t *testing.T) {
		h := newIntegrationsHandler()

		rec := httptest.NewRecorder()
		req := withOperator(httptest.NewRequest(http.MethodPost, "/admin/integrations/sso/connections", strings.NewReader("{")))
		req.Header.Set("Content-Type", "application/json")
		h.UpsertSSOConnection(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for invalid JSON, got %d", rec.Code)
		}

		h.db = &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{err: errors.New("save failed")}
			},
		}
		rec = httptest.NewRecorder()
		req = withOperator(httptest.NewRequest(http.MethodPost, "/admin/integrations/sso/connections", strings.NewReader(`{"slug":"acme","display_name":"Acme","entity_id":"urn:acme","sso_url":"https://idp.example.com/sso"}`)))
		req.Header.Set("Content-Type", "application/json")
		h.UpsertSSOConnection(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 for save failure, got %d", rec.Code)
		}
	})

	t.Run("delete missing slug and db failures", func(t *testing.T) {
		h := newIntegrationsHandler()

		rec := httptest.NewRecorder()
		req := withOperator(withRouteParam(httptest.NewRequest(http.MethodDelete, "/admin/integrations/sso/connections", nil), "slug", " "))
		h.DeleteSSOConnection(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for empty slug, got %d", rec.Code)
		}

		h.db = &fakeDB{
			execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("delete failed")
			},
		}
		rec = httptest.NewRecorder()
		req = withOperator(withRouteParam(httptest.NewRequest(http.MethodDelete, "/admin/integrations/sso/connections/acme", nil), "slug", "acme"))
		h.DeleteSSOConnection(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 for delete failure, got %d", rec.Code)
		}

		h.db = &fakeDB{
			execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("DELETE 0"), nil
			},
		}
		rec = httptest.NewRecorder()
		h.DeleteSSOConnection(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for missing slug delete, got %d", rec.Code)
		}
	})
}

func TestProxyIntegrationAdditionalBranches(t *testing.T) {
	t.Run("upstream upsert invalid body and save failure", func(t *testing.T) {
		h := newIntegrationsHandler()

		rec := httptest.NewRecorder()
		req := withOperator(httptest.NewRequest(http.MethodPost, "/admin/integrations/proxy/upstreams", strings.NewReader("{")))
		req.Header.Set("Content-Type", "application/json")
		h.UpsertProxyUpstream(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for invalid JSON, got %d", rec.Code)
		}

		h.db = &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{err: errors.New("save failed")}
			},
		}
		rec = httptest.NewRecorder()
		req = withOperator(httptest.NewRequest(http.MethodPost, "/admin/integrations/proxy/upstreams", strings.NewReader(`{"name":"api","url":"https://api.example.com"}`)))
		req.Header.Set("Content-Type", "application/json")
		h.UpsertProxyUpstream(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 for save failure, got %d", rec.Code)
		}
	})

	t.Run("upstream delete branch matrix", func(t *testing.T) {
		h := newIntegrationsHandler()
		rec := httptest.NewRecorder()
		req := withOperator(withRouteParam(httptest.NewRequest(http.MethodDelete, "/admin/integrations/proxy/upstreams", nil), "name", " "))
		h.DeleteProxyUpstream(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for empty upstream name, got %d", rec.Code)
		}

		h.db = &fakeDB{
			execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("delete failed")
			},
		}
		rec = httptest.NewRecorder()
		req = withOperator(withRouteParam(httptest.NewRequest(http.MethodDelete, "/admin/integrations/proxy/upstreams/api", nil), "name", "api"))
		h.DeleteProxyUpstream(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 for delete failure, got %d", rec.Code)
		}

		h.db = &fakeDB{
			execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("DELETE 0"), nil
			},
		}
		rec = httptest.NewRecorder()
		h.DeleteProxyUpstream(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for missing upstream delete, got %d", rec.Code)
		}
	})

	t.Run("route upsert invalid body and save failure", func(t *testing.T) {
		h := newIntegrationsHandler()

		rec := httptest.NewRecorder()
		req := withOperator(httptest.NewRequest(http.MethodPost, "/admin/integrations/proxy/routes", strings.NewReader("{")))
		req.Header.Set("Content-Type", "application/json")
		h.UpsertProxyRoute(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for invalid route JSON, got %d", rec.Code)
		}

		h.db = &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{err: errors.New("save failed")}
			},
		}
		rec = httptest.NewRecorder()
		req = withOperator(httptest.NewRequest(http.MethodPost, "/admin/integrations/proxy/routes", strings.NewReader(`{"path":"/v1/*","target":"api"}`)))
		req.Header.Set("Content-Type", "application/json")
		h.UpsertProxyRoute(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 for route save failure, got %d", rec.Code)
		}
	})

	t.Run("route delete branch matrix", func(t *testing.T) {
		h := newIntegrationsHandler()
		rec := httptest.NewRecorder()
		req := withOperator(withRouteParam(httptest.NewRequest(http.MethodDelete, "/admin/integrations/proxy/routes", nil), "id", " "))
		h.DeleteProxyRoute(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for empty route id, got %d", rec.Code)
		}

		h.db = &fakeDB{
			execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("delete failed")
			},
		}
		rec = httptest.NewRecorder()
		req = withOperator(withRouteParam(httptest.NewRequest(http.MethodDelete, "/admin/integrations/proxy/routes/r1", nil), "id", "r1"))
		h.DeleteProxyRoute(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 for route delete failure, got %d", rec.Code)
		}

		h.db = &fakeDB{
			execFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("DELETE 0"), nil
			},
		}
		rec = httptest.NewRecorder()
		h.DeleteProxyRoute(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for missing route delete, got %d", rec.Code)
		}
	})
}

func TestSimulateProxyRouteAdditionalBranches(t *testing.T) {
	operator := &adminstore.Operator{ID: uuid.New(), Role: "admin", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	newReq := func(body string) *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/admin/integrations/proxy/simulate", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
		return req
	}

	t.Run("invalid json and query failures", func(t *testing.T) {
		h := newIntegrationsHandler()
		rec := httptest.NewRecorder()
		h.SimulateProxyRoute(rec, newReq("{"))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for invalid JSON, got %d", rec.Code)
		}

		h.db = &fakeDB{
			queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
				return nil, errors.New("query failed")
			},
		}
		rec = httptest.NewRecorder()
		h.SimulateProxyRoute(rec, newReq(`{"path":"/x","method":"GET"}`))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 for route query failure, got %d", rec.Code)
		}
	})

	t.Run("scan and rows errors", func(t *testing.T) {
		h := newIntegrationsHandler()
		h.db = &fakeDB{
			queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
				// Wrong column count triggers scan error branch.
				return &fakeRows{data: [][]any{{"only-one-column"}}}, nil
			},
		}
		rec := httptest.NewRecorder()
		h.SimulateProxyRoute(rec, newReq(`{"path":"/x","method":"GET"}`))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 for scan failure, got %d", rec.Code)
		}

		now := time.Now().UTC()
		h.db = &fakeDB{
			queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
				return &fakeRows{
					data: [][]any{{
						"r1", "/ok/*", []byte(`["GET"]`), false, "", []byte(`[]`), []byte(`{}`), "api", 1, []byte(`{}`), []byte(`{}`), true, "", now, now,
					}},
					err: errors.New("rows failed"),
				}, nil
			},
		}
		rec = httptest.NewRecorder()
		h.SimulateProxyRoute(rec, newReq(`{"path":"/ok/p","method":"GET"}`))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 for rows error, got %d", rec.Code)
		}
	})

	t.Run("no-match and authenticated default-aal branches", func(t *testing.T) {
		now := time.Now().UTC()
		h := newIntegrationsHandler()
		h.db = &fakeDB{
			queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &fakeRows{data: [][]any{{
					"r1", "/secure/*", []byte(`["GET"]`), true, "aal1", []byte(`[]`), []byte(`{}`), "api", 10, []byte(`{}`), []byte(`{}`), true, "secure route", now, now,
				}}}, nil
			},
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{err: pgx.ErrNoRows}
			},
		}

		rec := httptest.NewRecorder()
		h.SimulateProxyRoute(rec, newReq(`{"path":"/other/path","method":"GET"}`))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"matched":false`) {
			t.Fatalf("expected no-match response, got code=%d body=%s", rec.Code, rec.Body.String())
		}

		// Empty method and non-leading-slash path exercise defaults and authenticated AAL fallback.
		rec = httptest.NewRecorder()
		h.SimulateProxyRoute(rec, newReq(`{"path":"secure/path","method":"","authenticated":true,"aal":""}`))
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 for authenticated simulation, got %d body=%s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"matched":true`) || !strings.Contains(rec.Body.String(), `"allowed":true`) {
			t.Fatalf("expected matched and allowed simulation result, got %s", rec.Body.String())
		}
	})
}
