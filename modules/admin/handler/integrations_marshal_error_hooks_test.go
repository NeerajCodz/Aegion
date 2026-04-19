package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIntegrationsWriteMarshalErrorBranches(t *testing.T) {
	origMarshal := integrationJSONMarshal
	t.Cleanup(func() {
		integrationJSONMarshal = origMarshal
	})

	setMarshalFailureAt := func(failAt int) {
		calls := 0
		integrationJSONMarshal = func(v interface{}) ([]byte, error) {
			calls++
			if calls == failAt {
				return nil, errors.New("marshal failed")
			}
			return json.Marshal(v)
		}
	}

	t.Run("sso upsert marshal failures", func(t *testing.T) {
		for failAt := 1; failAt <= 3; failAt++ {
			setMarshalFailureAt(failAt)
			h := newIntegrationsHandler()
			rec := httptest.NewRecorder()
			req := withOperator(httptest.NewRequest(http.MethodPost, "/admin/integrations/sso/connections", strings.NewReader(`{
				"slug":"acme",
				"display_name":"Acme",
				"entity_id":"urn:acme",
				"sso_url":"https://idp.example.com/sso",
				"domains":["acme.example"],
				"attribute_mapping":{"email":"mail"},
				"extra_authn_context":{"acr":"urn:mfa"}
			}`)))
			req.Header.Set("Content-Type", "application/json")
			h.UpsertSSOConnection(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for marshal failure #%d, got %d", failAt, rec.Code)
			}
		}
	})

	t.Run("proxy upstream marshal failures", func(t *testing.T) {
		for failAt := 1; failAt <= 2; failAt++ {
			setMarshalFailureAt(failAt)
			h := newIntegrationsHandler()
			rec := httptest.NewRecorder()
			req := withOperator(httptest.NewRequest(http.MethodPost, "/admin/integrations/proxy/upstreams", strings.NewReader(`{
				"name":"api",
				"url":"https://api.example.com",
				"headers":{"x-env":"prod"},
				"circuit_breaker":{"enabled":true}
			}`)))
			req.Header.Set("Content-Type", "application/json")
			h.UpsertProxyUpstream(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for marshal failure #%d, got %d", failAt, rec.Code)
			}
		}
	})

	t.Run("proxy route marshal failures", func(t *testing.T) {
		for failAt := 1; failAt <= 5; failAt++ {
			setMarshalFailureAt(failAt)
			h := newIntegrationsHandler()
			rec := httptest.NewRecorder()
			req := withOperator(httptest.NewRequest(http.MethodPost, "/admin/integrations/proxy/routes", strings.NewReader(`{
				"path":"/v1/profile",
				"methods":["GET"],
				"require_auth":true,
				"required_aal":"aal2",
				"capabilities":["profile:read"],
				"rate_limit":{"requests":10},
				"target":"api",
				"priority":100,
				"headers":{"x-test":"1"},
				"rewrite":{"strip_prefix":"/v1"},
				"enabled":true,
				"description":"profile route"
			}`)))
			req.Header.Set("Content-Type", "application/json")
			h.UpsertProxyRoute(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for marshal failure #%d, got %d", failAt, rec.Code)
			}
		}
	})
}
