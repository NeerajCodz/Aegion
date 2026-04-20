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
	socialservice "github.com/aegion/aegion/modules/social/service"
	socialstore "github.com/aegion/aegion/modules/social/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestIntegrationOverviewAndSetupStatusCoverage(t *testing.T) {
	t.Run("integration overview success and error", func(t *testing.T) {
		h := newIntegrationsHandler()

		count := int64(0)
		h.db = &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				count++
				return fakeRow{vals: []any{count}}
			},
		}

		rec := httptest.NewRecorder()
		req := withOperator(httptest.NewRequest(http.MethodGet, "/admin/integrations/overview", nil))
		h.IntegrationOverview(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d body=%s", http.StatusOK, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"oauth2_tokens":8`) {
			t.Fatalf("expected overview counts in response, got %s", rec.Body.String())
		}

		h = newIntegrationsHandler()
		h.db = &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{err: errors.New("count failed")}
			},
		}
		rec = httptest.NewRecorder()
		req = withOperator(httptest.NewRequest(http.MethodGet, "/admin/integrations/overview", nil))
		h.IntegrationOverview(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
	})

	t.Run("setup status success and error", func(t *testing.T) {
		h := newIntegrationsHandler()
		h.db = &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{vals: []any{int64(1)}}
			},
		}

		rec := httptest.NewRecorder()
		req := withOperator(httptest.NewRequest(http.MethodGet, "/admin/integrations/setup/status", nil))
		h.SetupStatus(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d body=%s", http.StatusOK, rec.Code, rec.Body.String())
		}
		for _, expected := range []string{
			`"has_admin_operator":true`,
			`"has_social_provider":true`,
			`"has_sso_connection":true`,
			`"has_proxy_route":true`,
			`"has_scim_token":true`,
			`"has_oauth2_client":true`,
			`"has_ip_ban":true`,
		} {
			if !strings.Contains(rec.Body.String(), expected) {
				t.Fatalf("expected %s in setup status response, got %s", expected, rec.Body.String())
			}
		}

		h = newIntegrationsHandler()
		h.db = &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{err: errors.New("count failed")}
			},
		}
		rec = httptest.NewRecorder()
		req = withOperator(httptest.NewRequest(http.MethodGet, "/admin/integrations/setup/status", nil))
		h.SetupStatus(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
	})
}

func TestSocialProviderEndpointBranches(t *testing.T) {
	operatorReq := func(method, target string, body string) *http.Request {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		return withOperator(req)
	}

	t.Run("get provider branches", func(t *testing.T) {
		h := newIntegrationsHandler()

		rec := httptest.NewRecorder()
		req := withOperator(withRouteParam(httptest.NewRequest(http.MethodGet, "/admin/integrations/social/providers/google", nil), "slug", "google"))
		h.GetSocialProvider(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected %d, got %d", http.StatusServiceUnavailable, rec.Code)
		}

		h.socialProviders = &fakeSocialProviderManager{
			getProviderFn: func(context.Context, string) (*socialstore.Provider, error) {
				return nil, socialstore.ErrProviderNotFound
			},
		}
		rec = httptest.NewRecorder()
		h.GetSocialProvider(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected %d, got %d", http.StatusNotFound, rec.Code)
		}

		h.socialProviders = &fakeSocialProviderManager{
			getProviderFn: func(context.Context, string) (*socialstore.Provider, error) {
				return nil, errors.New("boom")
			},
		}
		rec = httptest.NewRecorder()
		h.GetSocialProvider(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		now := time.Now().UTC()
		h.socialProviders = &fakeSocialProviderManager{
			getProviderFn: func(context.Context, string) (*socialstore.Provider, error) {
				return &socialstore.Provider{
					ID:          uuid.New(),
					Slug:        "google",
					DisplayName: "Google",
					Protocol:    socialstore.ProtocolOIDC,
					Enabled:     true,
					CreatedAt:   now,
					UpdatedAt:   now,
				}, nil
			},
		}
		rec = httptest.NewRecorder()
		h.GetSocialProvider(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d body=%s", http.StatusOK, rec.Code, rec.Body.String())
		}
	})

	t.Run("upsert provider branches", func(t *testing.T) {
		h := newIntegrationsHandler()
		h.socialProviders = &fakeSocialProviderManager{}

		rec := httptest.NewRecorder()
		req := operatorReq(http.MethodPost, "/admin/integrations/social/providers", "{")
		h.UpsertSocialProvider(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		h.socialProviders = &fakeSocialProviderManager{
			upsertProviderFn: func(context.Context, socialservice.ProviderUpsertRequest) (*socialstore.Provider, error) {
				return nil, errors.New("save failed")
			},
		}
		rec = httptest.NewRecorder()
		req = operatorReq(http.MethodPost, "/admin/integrations/social/providers", `{"slug":"google","display_name":"Google","protocol":"oidc"}`)
		h.UpsertSocialProvider(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		now := time.Now().UTC()
		h.socialProviders = &fakeSocialProviderManager{
			upsertProviderFn: func(_ context.Context, req socialservice.ProviderUpsertRequest) (*socialstore.Provider, error) {
				return &socialstore.Provider{
					ID:          uuid.New(),
					Slug:        req.Slug,
					DisplayName: req.DisplayName,
					Protocol:    req.Protocol,
					Enabled:     req.Enabled,
					CreatedAt:   now,
					UpdatedAt:   now,
				}, nil
			},
		}
		rec = httptest.NewRecorder()
		req = operatorReq(http.MethodPost, "/admin/integrations/social/providers", `{"slug":" GOOGLE ","display_name":" Google ","protocol":"oidc","enabled":true}`)
		h.UpsertSocialProvider(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d body=%s", http.StatusOK, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"slug":"google"`) {
			t.Fatalf("expected normalized slug in response, got %s", rec.Body.String())
		}
	})

	t.Run("delete provider branches", func(t *testing.T) {
		h := newIntegrationsHandler()
		h.socialProviders = &fakeSocialProviderManager{}

		rec := httptest.NewRecorder()
		req := withOperator(withRouteParam(httptest.NewRequest(http.MethodDelete, "/admin/integrations/social/providers", nil), "slug", ""))
		h.DeleteSocialProvider(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
		}

		req = withOperator(withRouteParam(httptest.NewRequest(http.MethodDelete, "/admin/integrations/social/providers/google", nil), "slug", "google"))
		h.socialProviders = &fakeSocialProviderManager{
			deleteProviderFn: func(context.Context, string) error {
				return socialstore.ErrProviderNotFound
			},
		}
		rec = httptest.NewRecorder()
		h.DeleteSocialProvider(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected %d, got %d", http.StatusNotFound, rec.Code)
		}

		h.socialProviders = &fakeSocialProviderManager{
			deleteProviderFn: func(context.Context, string) error {
				return errors.New("delete failed")
			},
		}
		rec = httptest.NewRecorder()
		h.DeleteSocialProvider(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		h.socialProviders = &fakeSocialProviderManager{
			deleteProviderFn: func(context.Context, string) error {
				return nil
			},
		}
		rec = httptest.NewRecorder()
		h.DeleteSocialProvider(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected %d, got %d", http.StatusNoContent, rec.Code)
		}
	})
}

func TestRBACSummaryAndActivityFeedAdditionalBranches(t *testing.T) {
	operator := &adminstore.Operator{
		ID:         uuid.New(),
		IdentityID: uuid.New(),
		Role:       "admin",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	baseReq := httptest.NewRequest(http.MethodGet, "/admin/integrations/activity", nil)
	baseReq = baseReq.WithContext(context.WithValue(baseReq.Context(), contextKeyOperator, operator))

	t.Run("rbac summary success", func(t *testing.T) {
		role := &adminstore.Role{
			ID:          uuid.New(),
			Name:        "admin",
			Description: "Administrator",
			Permissions: []string{"operators:write", "operators:read"},
			IsSystem:    true,
		}

		h := New(&fakeService{
			store: &fakeStore{},
			listRolesFn: func(context.Context, uuid.UUID, int, int) ([]*adminstore.Role, int64, error) {
				return []*adminstore.Role{role}, 1, nil
			},
			availablePermissionsFn: func() []string {
				return []string{"roles:read", "operators:read"}
			},
		})
		h.db = &fakeDB{
			queryRowFn: func(context.Context, string, ...any) pgx.Row {
				return fakeRow{vals: []any{int64(3)}}
			},
		}

		rec := httptest.NewRecorder()
		h.RBACSummary(rec, baseReq)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d body=%s", http.StatusOK, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"operator_ids":3`) {
			t.Fatalf("expected role operator count in response, got %s", rec.Body.String())
		}
	})

	t.Run("activity feed error and success", func(t *testing.T) {
		h := New(&fakeService{
			store: &fakeStore{},
			listAuditLogsFn: func(context.Context, uuid.UUID, adminstore.AuditFilter, int, int) ([]*adminstore.AuditLogEntry, int64, error) {
				return nil, 0, errors.New("load failed")
			},
		})

		rec := httptest.NewRecorder()
		h.ActivityFeed(rec, baseReq)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}

		operatorID := uuid.New()
		h = New(&fakeService{
			store: &fakeStore{},
			listAuditLogsFn: func(context.Context, uuid.UUID, adminstore.AuditFilter, int, int) ([]*adminstore.AuditLogEntry, int64, error) {
				return []*adminstore.AuditLogEntry{
					{
						ID:           uuid.New(),
						OperatorID:   &operatorID,
						Action:       "update",
						ResourceType: "oauth2_client",
						ResourceID:   "client-1",
						Details:      map[string]any{"name": "Example"},
						IPAddress:    "127.0.0.1",
						CreatedAt:    time.Now().UTC(),
					},
				}, 1, nil
			},
		})

		rec = httptest.NewRecorder()
		h.ActivityFeed(rec, baseReq)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d body=%s", http.StatusOK, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"resource_type":"oauth2_client"`) {
			t.Fatalf("expected activity item in response, got %s", rec.Body.String())
		}
	})
}
