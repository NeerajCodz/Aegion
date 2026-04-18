package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aegion/aegion/modules/sso/service"
	"github.com/aegion/aegion/modules/sso/store"
)

type stubSSOService struct {
	listConnectionsFn           func(context.Context) ([]store.Connection, error)
	listConfiguredConnectionsFn func(context.Context, bool) ([]store.Connection, error)
	getConnectionFn             func(context.Context, string) (*store.Connection, error)
	getConnectionForDomainFn    func(context.Context, string) (*store.Connection, error)
	upsertConnectionFn          func(context.Context, service.ConnectionUpsertRequest) (*store.Connection, error)
	deleteConnectionFn          func(context.Context, string) error
	startAuthFn                 func(context.Context, string, string) (*service.StartResponse, error)
	completeAuthFn              func(context.Context, string, string, string, string, string, map[string]interface{}) (*service.CallbackResult, error)
}

func (s *stubSSOService) ListConnections(ctx context.Context) ([]store.Connection, error) {
	if s.listConnectionsFn != nil {
		return s.listConnectionsFn(ctx)
	}
	return []store.Connection{{Slug: "acme", DisplayName: "Acme", Enabled: true}}, nil
}

func (s *stubSSOService) ListConfiguredConnections(ctx context.Context, includeDisabled bool) ([]store.Connection, error) {
	if s.listConfiguredConnectionsFn != nil {
		return s.listConfiguredConnectionsFn(ctx, includeDisabled)
	}
	return []store.Connection{{Slug: "acme", DisplayName: "Acme", Enabled: true}}, nil
}

func (s *stubSSOService) GetConnection(ctx context.Context, slug string) (*store.Connection, error) {
	if s.getConnectionFn != nil {
		return s.getConnectionFn(ctx, slug)
	}
	return &store.Connection{Slug: slug, DisplayName: "Acme", Enabled: true}, nil
}

func (s *stubSSOService) GetConnectionForDomain(ctx context.Context, domain string) (*store.Connection, error) {
	if s.getConnectionForDomainFn != nil {
		return s.getConnectionForDomainFn(ctx, domain)
	}
	return &store.Connection{Slug: "acme", DisplayName: "Acme", Enabled: true}, nil
}

func (s *stubSSOService) UpsertConnection(ctx context.Context, req service.ConnectionUpsertRequest) (*store.Connection, error) {
	if s.upsertConnectionFn != nil {
		return s.upsertConnectionFn(ctx, req)
	}
	return &store.Connection{Slug: req.Slug, DisplayName: req.DisplayName, Enabled: req.Enabled}, nil
}

func (s *stubSSOService) DeleteConnection(ctx context.Context, slug string) error {
	if s.deleteConnectionFn != nil {
		return s.deleteConnectionFn(ctx, slug)
	}
	return nil
}

func (s *stubSSOService) StartAuth(ctx context.Context, slug, redirectTo string) (*service.StartResponse, error) {
	if s.startAuthFn != nil {
		return s.startAuthFn(ctx, slug, redirectTo)
	}
	return &service.StartResponse{Connection: slug, RedirectURL: "https://idp.example.com", RelayState: "relay"}, nil
}

func (s *stubSSOService) CompleteAuth(ctx context.Context, slug, relayState, subject, email, displayName string, attrs map[string]interface{}) (*service.CallbackResult, error) {
	if s.completeAuthFn != nil {
		return s.completeAuthFn(ctx, slug, relayState, subject, email, displayName, attrs)
	}
	return &service.CallbackResult{Connection: slug, Subject: subject, Email: email, RedirectTo: "/after"}, nil
}

func mustSSOJSON(v interface{}) *bytes.Reader {
	b, _ := json.Marshal(v)
	return bytes.NewReader(b)
}

func TestSSOHelpersAndAuthBranches(t *testing.T) {
	h := New(&stubSSOService{}, Config{ManagementToken: "secret"})

	t.Run("register routes nil mux", func(t *testing.T) {
		h.RegisterRoutes(nil)
	})

	t.Run("helper functions", func(t *testing.T) {
		if !acceptsJSON(httptest.NewRequest(http.MethodGet, "/", nil)) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Accept", "application/json")
			if !acceptsJSON(req) {
				t.Fatal("expected acceptsJSON to detect json accept header")
			}
		}
		if got := withQuery("://bad-url", map[string]string{"a": "b"}); got != "://bad-url" {
			t.Fatalf("expected invalid URL target passthrough, got %q", got)
		}
		if got := withQuery("https://app.example.com/callback?existing=1", map[string]string{"empty": " ", "state": "ok"}); !strings.Contains(got, "state=ok") || !strings.Contains(got, "existing=1") {
			t.Fatalf("expected merged query params, got %q", got)
		}
		if got := firstNonEmpty(" ", "", "value", "fallback"); got != "value" {
			t.Fatalf("firstNonEmpty returned %q", got)
		}
		if got := firstNonEmpty(" ", ""); got != "" {
			t.Fatalf("firstNonEmpty empty result = %q", got)
		}
	})

	t.Run("management auth branches", func(t *testing.T) {
		mux := http.NewServeMux()
		h.RegisterRoutes(mux)

		disabled := New(&stubSSOService{})
		disabledMux := http.NewServeMux()
		disabled.RegisterRoutes(disabledMux)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sso/admin/connections", nil)
		disabledMux.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected disabled management to return 503, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/api/v1/sso/admin/connections", nil)
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected missing token to return 401, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/api/v1/sso/admin/connections", nil)
		req.Header.Set("Authorization", "Bearer wrong")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected invalid token to return 401, got %d", rec.Code)
		}
	})
}

func TestSSOPublicRoutes(t *testing.T) {
	boom := errors.New("boom")
	var captured struct {
		slug     string
		relay    string
		subject  string
		email    string
		display  string
		attrs    map[string]interface{}
		redirect string
	}
	svc := &stubSSOService{
		listConnectionsFn: func(context.Context) ([]store.Connection, error) { return nil, boom },
		getConnectionForDomainFn: func(context.Context, string) (*store.Connection, error) {
			return nil, boom
		},
		startAuthFn: func(context.Context, string, string) (*service.StartResponse, error) {
			return nil, boom
		},
		completeAuthFn: func(_ context.Context, slug, relayState, subject, email, displayName string, attrs map[string]interface{}) (*service.CallbackResult, error) {
			captured.slug = slug
			captured.relay = relayState
			captured.subject = subject
			captured.email = email
			captured.display = displayName
			captured.attrs = attrs
			return &service.CallbackResult{Connection: slug, Subject: subject, Email: email, RedirectTo: captured.redirect}, nil
		},
	}
	h := New(svc, Config{ManagementToken: "secret"})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	t.Run("connections and domain resolution failures", func(t *testing.T) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/sso/connections", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/sso/connections", nil))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/sso/resolve-domain", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/sso/resolve-domain", nil))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/sso/resolve-domain?domain=example.com", nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", rec.Code)
		}
	})

	t.Run("path and start validation", func(t *testing.T) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/sso/acme/unknown", nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/self-service/sso/acme/start", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/self-service/sso/acme/start", bytes.NewReader([]byte(`{"redirect_to":"/x"}{"extra":1}`))))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/self-service/sso/acme/start", mustSSOJSON(map[string]string{"redirect_to": "/x"})))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 from service failure, got %d", rec.Code)
		}
	})

	t.Run("callback method and parse failures", func(t *testing.T) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/self-service/sso/acme/callback", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/self-service/sso/acme/callback", strings.NewReader("x=%zz"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for parse form failure, got %d", rec.Code)
		}
	})

	t.Run("callback json and redirect behavior", func(t *testing.T) {
		svc.completeAuthFn = func(_ context.Context, slug, relayState, subject, email, displayName string, attrs map[string]interface{}) (*service.CallbackResult, error) {
			captured.slug = slug
			captured.relay = relayState
			captured.subject = subject
			captured.email = email
			captured.display = displayName
			captured.attrs = attrs
			return nil, boom
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/self-service/sso/acme/callback?RelayState=relay&subject=sub&email=user%40example.com", nil)
		req.Header.Set("Accept", "application/json")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for callback service error, got %d", rec.Code)
		}

		captured.redirect = "/after"
		svc.completeAuthFn = func(_ context.Context, slug, relayState, subject, email, displayName string, attrs map[string]interface{}) (*service.CallbackResult, error) {
			captured.slug = slug
			captured.relay = relayState
			captured.subject = subject
			captured.email = email
			captured.display = displayName
			captured.attrs = attrs
			return &service.CallbackResult{Connection: slug, Subject: subject, Email: email, RedirectTo: captured.redirect}, nil
		}
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/self-service/sso/acme/callback?RelayState=relay123&subject=sub123&email=user%40example.com&display_name=User&attributes={\"department\":\"eng\"}&SAMLResponse=saml-token", nil)
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("expected 303 redirect, got %d", rec.Code)
		}
		if got := rec.Header().Get("Location"); !strings.Contains(got, "sso_status=authenticated") || !strings.Contains(got, "sso_connection=acme") {
			t.Fatalf("expected redirect with sso query params, got %q", got)
		}
		if captured.slug != "acme" || captured.relay != "relay123" || captured.subject != "sub123" || captured.email != "user@example.com" || captured.display != "User" {
			t.Fatalf("unexpected callback args: %+v", captured)
		}
		if captured.attrs["_saml_response"] != "saml-token" {
			t.Fatalf("expected saml response attribute to be forwarded, got %#v", captured.attrs)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/self-service/sso/acme/callback?RelayState=relay456&name_id=name-id&attributes=not-json", nil)
		req.Header.Set("Accept", "application/json")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 JSON response, got %d", rec.Code)
		}
		if captured.subject != "name-id" {
			t.Fatalf("expected name_id fallback, got %q", captured.subject)
		}
	})
}

func TestSSOAdminRoutes(t *testing.T) {
	boom := errors.New("boom")
	svc := &stubSSOService{
		listConfiguredConnectionsFn: func(context.Context, bool) ([]store.Connection, error) { return nil, boom },
		upsertConnectionFn:          func(context.Context, service.ConnectionUpsertRequest) (*store.Connection, error) { return nil, boom },
		getConnectionFn:             func(context.Context, string) (*store.Connection, error) { return nil, boom },
		deleteConnectionFn:          func(context.Context, string) error { return boom },
	}
	h := New(svc, Config{ManagementToken: "secret"})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	authed := func(method, path string, body io.Reader) *http.Request {
		req := httptest.NewRequest(method, path, body)
		req.Header.Set("Authorization", "Bearer secret")
		return req
	}

	t.Run("admin connections collection", func(t *testing.T) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, authed(http.MethodGet, "/api/v1/sso/admin/connections", nil))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, authed(http.MethodPost, "/api/v1/sso/admin/connections", bytes.NewReader([]byte(`{"slug":"acme"}{"extra":1}`))))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for invalid body, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, authed(http.MethodPost, "/api/v1/sso/admin/connections", mustSSOJSON(service.ConnectionUpsertRequest{
			Slug:        "acme",
			DisplayName: "Acme",
			EntityID:    "urn:acme",
			SSOURL:      "https://idp.example.com",
		})))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for upsert failure, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, authed(http.MethodPatch, "/api/v1/sso/admin/connections", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405 for unsupported method, got %d", rec.Code)
		}
	})

	t.Run("admin connection item", func(t *testing.T) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, authed(http.MethodGet, "/api/v1/sso/admin/connections/acme", nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, authed(http.MethodDelete, "/api/v1/sso/admin/connections/acme", nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for delete error, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, authed(http.MethodPatch, "/api/v1/sso/admin/connections/acme", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		emptyReq := httptest.NewRequest(http.MethodGet, "/api/v1/sso/admin/connections/", nil)
		emptyReq.Header.Set("Authorization", "Bearer secret")
		h.handleAdminConnection(rec, emptyReq)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for empty slug, got %d", rec.Code)
		}
	})
}

func TestSSOSuccessRoutes(t *testing.T) {
	svc := &stubSSOService{
		listConnectionsFn: func(context.Context) ([]store.Connection, error) {
			return []store.Connection{{Slug: "acme", DisplayName: "Acme", Enabled: true}}, nil
		},
		getConnectionForDomainFn: func(context.Context, string) (*store.Connection, error) {
			return &store.Connection{Slug: "acme", DisplayName: "Acme", Enabled: true}, nil
		},
		startAuthFn: func(context.Context, string, string) (*service.StartResponse, error) {
			return &service.StartResponse{Connection: "acme", RedirectURL: "https://idp.example.com", RelayState: "relay"}, nil
		},
		listConfiguredConnectionsFn: func(context.Context, bool) ([]store.Connection, error) {
			return []store.Connection{{Slug: "acme", DisplayName: "Acme", Enabled: true}}, nil
		},
		upsertConnectionFn: func(context.Context, service.ConnectionUpsertRequest) (*store.Connection, error) {
			return &store.Connection{Slug: "acme", DisplayName: "Acme", Enabled: true}, nil
		},
		getConnectionFn: func(context.Context, string) (*store.Connection, error) {
			return &store.Connection{Slug: "acme", DisplayName: "Acme", Enabled: true}, nil
		},
		deleteConnectionFn: func(context.Context, string) error { return nil },
		completeAuthFn: func(_ context.Context, slug, relayState, subject, email, displayName string, attrs map[string]interface{}) (*service.CallbackResult, error) {
			return &service.CallbackResult{
				Connection:  slug,
				Subject:     subject,
				Email:       email,
				DisplayName: displayName,
				RedirectTo:  "",
				Attributes:  attrs,
			}, nil
		},
	}
	h := New(svc, Config{ManagementToken: "secret"})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	authed := func(method, path string, body io.Reader) *http.Request {
		req := httptest.NewRequest(method, path, body)
		req.Header.Set("Authorization", "Bearer secret")
		return req
	}

	cases := []struct {
		name   string
		method string
		path   string
		body   io.Reader
		auth   bool
		want   int
	}{
		{"public list", http.MethodGet, "/api/v1/sso/connections", nil, false, http.StatusOK},
		{"resolve domain", http.MethodGet, "/api/v1/sso/resolve-domain?domain=example.com", nil, false, http.StatusOK},
		{"start", http.MethodPost, "/self-service/sso/acme/start", mustSSOJSON(map[string]string{"redirect_to": "/after"}), false, http.StatusOK},
		{"callback json", http.MethodGet, "/self-service/sso/acme/callback?RelayState=relay&subject=sub&email=user%40example.com&display_name=User", nil, false, http.StatusOK},
		{"admin list", http.MethodGet, "/api/v1/sso/admin/connections", nil, true, http.StatusOK},
		{"admin upsert", http.MethodPost, "/api/v1/sso/admin/connections", mustSSOJSON(service.ConnectionUpsertRequest{
			Slug:        "acme",
			DisplayName: "Acme",
			EntityID:    "urn:acme",
			SSOURL:      "https://idp.example.com",
		}), true, http.StatusOK},
		{"admin get", http.MethodGet, "/api/v1/sso/admin/connections/acme", nil, true, http.StatusOK},
		{"admin delete", http.MethodDelete, "/api/v1/sso/admin/connections/acme", nil, true, http.StatusNoContent},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			if tc.auth {
				mux.ServeHTTP(rec, authed(tc.method, tc.path, tc.body))
			} else {
				mux.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, tc.body))
			}
			if rec.Code != tc.want {
				t.Fatalf("expected %d, got %d body=%s", tc.want, rec.Code, rec.Body.String())
			}
		})
	}
}
