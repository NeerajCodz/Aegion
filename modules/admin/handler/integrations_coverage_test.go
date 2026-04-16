package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	adminstore "github.com/aegion/aegion/modules/admin/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func newIntegrationsHandler() *Handler {
	return New(&fakeService{store: &fakeStore{}})
}

func withOperator(req *http.Request) *http.Request {
	operator := &adminstore.Operator{
		ID:         uuid.New(),
		IdentityID: uuid.New(),
		Role:       "admin",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	return req.WithContext(context.WithValue(req.Context(), contextKeyOperator, operator))
}

func TestIntegrationHandlersUnauthorizedBranches(t *testing.T) {
	h := newIntegrationsHandler()

	cases := []struct {
		name string
		run  func(http.ResponseWriter, *http.Request)
		path string
	}{
		{"overview", h.IntegrationOverview, "/admin/integrations/overview"},
		{"setup status", h.SetupStatus, "/admin/integrations/setup/status"},
		{"social presets", h.ListSocialPresets, "/admin/integrations/social/presets"},
		{"rbac summary", h.RBACSummary, "/admin/integrations/rbac/summary"},
		{"activity feed", h.ActivityFeed, "/admin/integrations/activity"},
		{"list social providers", h.ListSocialProviders, "/admin/integrations/social/providers"},
		{"get social provider", h.GetSocialProvider, "/admin/integrations/social/providers/google"},
		{"upsert social provider", h.UpsertSocialProvider, "/admin/integrations/social/providers"},
		{"delete social provider", h.DeleteSocialProvider, "/admin/integrations/social/providers/google"},
		{"list sso connections", h.ListSSOConnections, "/admin/integrations/sso/connections"},
		{"upsert sso connection", h.UpsertSSOConnection, "/admin/integrations/sso/connections"},
		{"delete sso connection", h.DeleteSSOConnection, "/admin/integrations/sso/connections/acme"},
		{"list proxy upstreams", h.ListProxyUpstreams, "/admin/integrations/proxy/upstreams"},
		{"upsert proxy upstream", h.UpsertProxyUpstream, "/admin/integrations/proxy/upstreams"},
		{"delete proxy upstream", h.DeleteProxyUpstream, "/admin/integrations/proxy/upstreams/app"},
		{"list proxy routes", h.ListProxyRoutes, "/admin/integrations/proxy/routes"},
		{"upsert proxy route", h.UpsertProxyRoute, "/admin/integrations/proxy/routes"},
		{"delete proxy route", h.DeleteProxyRoute, "/admin/integrations/proxy/routes/r1"},
		{"simulate proxy route", h.SimulateProxyRoute, "/admin/integrations/proxy/simulate"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			tc.run(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401 unauthorized, got %d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestIntegrationHelperDecoders(t *testing.T) {
	if got := decodeStringSlice(nil); got != nil {
		t.Fatalf("decodeStringSlice(nil) = %#v", got)
	}
	got := decodeStringSlice([]byte(`[" one ","","two"]`))
	if len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("decodeStringSlice(valid) = %#v", got)
	}
	if got := decodeStringSlice([]byte(`{`)); got != nil {
		t.Fatalf("decodeStringSlice(invalid) = %#v", got)
	}

	if got := decodeStringMap(nil); got != nil {
		t.Fatalf("decodeStringMap(nil) = %#v", got)
	}
	gotMap := decodeStringMap([]byte(`{"k":"v"}`))
	if len(gotMap) != 1 || gotMap["k"] != "v" {
		t.Fatalf("decodeStringMap(valid) = %#v", gotMap)
	}
	if got := decodeStringMap([]byte(`{`)); got != nil {
		t.Fatalf("decodeStringMap(invalid) = %#v", got)
	}

	if got := decodeRewriteConfig(nil); got != nil {
		t.Fatalf("decodeRewriteConfig(nil) = %#v", got)
	}
	if got := decodeRewriteConfig([]byte(`{"strip_prefix":"","add_prefix":"","regex":"","replacement":""}`)); got != nil {
		t.Fatalf("decodeRewriteConfig(empty) = %#v", got)
	}
	if got := decodeRewriteConfig([]byte(`{"strip_prefix":"/api"}`)); got == nil || got.StripPrefix != "/api" {
		t.Fatalf("decodeRewriteConfig(valid) = %#v", got)
	}
	if got := decodeRewriteConfig([]byte(`{`)); got != nil {
		t.Fatalf("decodeRewriteConfig(invalid) = %#v", got)
	}
}

func TestListGenericRowsAndCountValueCoverage(t *testing.T) {
	h := newIntegrationsHandler()
	boom := errors.New("boom")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/integrations/generic", nil)
	h.listGenericRows(rec, req, "SELECT 1", []string{"id"}, "items")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 unauthorized, got %d", rec.Code)
	}

	h.db = &fakeDB{queryFn: func(context.Context, string, ...any) (pgx.Rows, error) { return nil, boom }}
	rec = httptest.NewRecorder()
	req = withOperator(httptest.NewRequest(http.MethodGet, "/admin/integrations/generic", nil))
	h.listGenericRows(rec, req, "SELECT 1", []string{"id"}, "items")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 query error, got %d", rec.Code)
	}

	h.db = &fakeDB{queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
		return &fakeRows{err: boom}, nil
	}}
	rec = httptest.NewRecorder()
	req = withOperator(httptest.NewRequest(http.MethodGet, "/admin/integrations/generic", nil))
	h.listGenericRows(rec, req, "SELECT 1", []string{"id"}, "items")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 rows error, got %d", rec.Code)
	}

	h.db = &fakeDB{queryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
		return &fakeRows{data: [][]any{{"r1", "x"}, {"r2"}}}, nil
	}}
	rec = httptest.NewRecorder()
	req = withOperator(httptest.NewRequest(http.MethodGet, "/admin/integrations/generic", nil))
	h.listGenericRows(rec, req, "SELECT 1", []string{"id", "extra"}, "items")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 success, got %d body=%s", rec.Code, rec.Body.String())
	}

	var count int64
	h.db = &fakeDB{queryRowFn: func(context.Context, string, ...any) pgx.Row {
		return fakeRow{vals: []any{int64(7)}}
	}}
	if err := h.countValue(req, "SELECT 7", &count); err != nil || count != 7 {
		t.Fatalf("countValue(success) count=%d err=%v", count, err)
	}

	h.db = &fakeDB{queryRowFn: func(context.Context, string, ...any) pgx.Row {
		return fakeRow{err: boom}
	}}
	if err := h.countValue(req, "SELECT 7", &count); !errors.Is(err, boom) {
		t.Fatalf("countValue(error) = %v", err)
	}
}
