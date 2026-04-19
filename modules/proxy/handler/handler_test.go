package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aegion/aegion/modules/proxy/service"
	"github.com/aegion/aegion/modules/proxy/store"
)

type stubProxyService struct {
	effectiveConfigFn func(context.Context) (*service.EffectiveConfig, error)
	listUpstreamsFn   func(context.Context) ([]store.Upstream, error)
	getUpstreamFn     func(context.Context, string) (*store.Upstream, error)
	upsertUpstreamFn  func(context.Context, store.Upstream) (*store.Upstream, error)
	deleteUpstreamFn  func(context.Context, string) error
	listRoutesFn      func(context.Context) ([]store.Route, error)
	getRouteFn        func(context.Context, string) (*store.Route, error)
	upsertRouteFn     func(context.Context, store.Route) (*store.Route, error)
	deleteRouteFn     func(context.Context, string) error
	simulateFn        func(context.Context, service.SimulateRequest) (*service.SimulateResponse, error)
}

func (s *stubProxyService) EffectiveConfig(ctx context.Context) (*service.EffectiveConfig, error) {
	if s.effectiveConfigFn != nil {
		return s.effectiveConfigFn(ctx)
	}
	return &service.EffectiveConfig{}, nil
}

func (s *stubProxyService) ListUpstreams(ctx context.Context) ([]store.Upstream, error) {
	if s.listUpstreamsFn != nil {
		return s.listUpstreamsFn(ctx)
	}
	return []store.Upstream{}, nil
}

func (s *stubProxyService) GetUpstream(ctx context.Context, name string) (*store.Upstream, error) {
	if s.getUpstreamFn != nil {
		return s.getUpstreamFn(ctx, name)
	}
	return &store.Upstream{Name: name, URL: "https://app.example.com", Enabled: true}, nil
}

func (s *stubProxyService) UpsertUpstream(ctx context.Context, upstream store.Upstream) (*store.Upstream, error) {
	if s.upsertUpstreamFn != nil {
		return s.upsertUpstreamFn(ctx, upstream)
	}
	return &upstream, nil
}

func (s *stubProxyService) DeleteUpstream(ctx context.Context, name string) error {
	if s.deleteUpstreamFn != nil {
		return s.deleteUpstreamFn(ctx, name)
	}
	return nil
}

func (s *stubProxyService) ListRoutes(ctx context.Context) ([]store.Route, error) {
	if s.listRoutesFn != nil {
		return s.listRoutesFn(ctx)
	}
	return []store.Route{}, nil
}

func (s *stubProxyService) GetRoute(ctx context.Context, id string) (*store.Route, error) {
	if s.getRouteFn != nil {
		return s.getRouteFn(ctx, id)
	}
	return &store.Route{ID: id, Path: "/app/*", Target: "app", Enabled: true}, nil
}

func (s *stubProxyService) UpsertRoute(ctx context.Context, route store.Route) (*store.Route, error) {
	if s.upsertRouteFn != nil {
		return s.upsertRouteFn(ctx, route)
	}
	return &route, nil
}

func (s *stubProxyService) DeleteRoute(ctx context.Context, id string) error {
	if s.deleteRouteFn != nil {
		return s.deleteRouteFn(ctx, id)
	}
	return nil
}

func (s *stubProxyService) Simulate(ctx context.Context, req service.SimulateRequest) (*service.SimulateResponse, error) {
	if s.simulateFn != nil {
		return s.simulateFn(ctx, req)
	}
	return &service.SimulateResponse{Matched: true, Allowed: true}, nil
}

func mustProxyJSON(v interface{}) *bytes.Reader {
	b, _ := json.Marshal(v)
	return bytes.NewReader(b)
}

func TestProxyHandlersAuthAndHelpers(t *testing.T) {
	h := New(&stubProxyService{}, Config{ManagementToken: "secret"})

	t.Run("register routes nil mux", func(t *testing.T) {
		h.RegisterRoutes(nil)
	})

	t.Run("management auth branches", func(t *testing.T) {
		mux := http.NewServeMux()
		h.RegisterRoutes(mux)

		disabled := New(&stubProxyService{})
		disabledMux := http.NewServeMux()
		disabled.RegisterRoutes(disabledMux)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/proxy/config", nil)
		disabledMux.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected disabled management to return 503, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/proxy/config", nil)
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected missing token to return 401, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/proxy/config", nil)
		req.Header.Set("Authorization", "Bearer wrong")
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected invalid token to return 401, got %d", rec.Code)
		}
	})

	t.Run("lastPathSegment", func(t *testing.T) {
		cases := map[string]string{
			"":                        "",
			"   ":                     "",
			"/proxy/upstreams/app":    "app",
			"/proxy/routes/route-123": "route-123",
			"route-1":                 "route-1",
		}
		for in, want := range cases {
			if got := lastPathSegment(in); got != want {
				t.Fatalf("lastPathSegment(%q) = %q, want %q", in, got, want)
			}
		}
	})
}

func TestProxyHandlersConfigAndCollectionRoutes(t *testing.T) {
	boom := errors.New("boom")
	svc := &stubProxyService{
		effectiveConfigFn: func(context.Context) (*service.EffectiveConfig, error) { return nil, boom },
		listUpstreamsFn:   func(context.Context) ([]store.Upstream, error) { return nil, boom },
		upsertUpstreamFn: func(context.Context, store.Upstream) (*store.Upstream, error) {
			return nil, service.ErrInvalidProxyConfig
		},
		listRoutesFn:  func(context.Context) ([]store.Route, error) { return nil, boom },
		upsertRouteFn: func(context.Context, store.Route) (*store.Route, error) { return nil, store.ErrUpstreamNotFound },
		simulateFn:    func(context.Context, service.SimulateRequest) (*service.SimulateResponse, error) { return nil, boom },
	}
	h := New(svc, Config{ManagementToken: "secret"})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	authed := func(method, path string, body io.Reader) *http.Request {
		req := httptest.NewRequest(method, path, body)
		req.Header.Set("Authorization", "Bearer secret")
		return req
	}

	t.Run("config method and service errors", func(t *testing.T) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, authed(http.MethodPost, "/proxy/config", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, authed(http.MethodGet, "/proxy/config", nil))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d", rec.Code)
		}
	})

	t.Run("upstreams branches", func(t *testing.T) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, authed(http.MethodGet, "/proxy/upstreams", nil))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, authed(http.MethodPost, "/proxy/upstreams", bytes.NewReader([]byte(`{"name":"app","unknown":true}`))))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for invalid body, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, authed(http.MethodPost, "/proxy/upstreams", mustProxyJSON(store.Upstream{Name: "app", URL: "https://app.example.com"})))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for validation error, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, authed(http.MethodPut, "/proxy/upstreams", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405 for unsupported method, got %d", rec.Code)
		}
	})

	t.Run("routes branches", func(t *testing.T) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, authed(http.MethodGet, "/proxy/routes", nil))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, authed(http.MethodPost, "/proxy/routes", bytes.NewReader([]byte(`{"path":"/x"}{"extra":1}`))))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for invalid body, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, authed(http.MethodPost, "/proxy/routes", mustProxyJSON(store.Route{Path: "/app/*", Target: "missing"})))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for upstream missing, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, authed(http.MethodPatch, "/proxy/routes", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405 for unsupported method, got %d", rec.Code)
		}
	})

	t.Run("simulate branches", func(t *testing.T) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, authed(http.MethodGet, "/proxy/simulate", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, authed(http.MethodPost, "/proxy/simulate", bytes.NewReader([]byte(`{"path":"/a"}{"x":1}`))))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for invalid body, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, authed(http.MethodPost, "/proxy/simulate", mustProxyJSON(service.SimulateRequest{Path: "/a", Method: http.MethodGet})))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 from simulate failure, got %d", rec.Code)
		}
	})
}

func TestProxyHandlersResourceRoutes(t *testing.T) {
	boom := errors.New("boom")
	svc := &stubProxyService{
		getUpstreamFn: func(context.Context, string) (*store.Upstream, error) { return nil, boom },
		deleteUpstreamFn: func(context.Context, string) error {
			return store.ErrUpstreamInUse
		},
		getRouteFn:    func(context.Context, string) (*store.Route, error) { return nil, boom },
		deleteRouteFn: func(context.Context, string) error { return boom },
	}
	h := New(svc, Config{ManagementToken: "secret"})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := func(method, path string) *http.Request {
		r := httptest.NewRequest(method, path, nil)
		r.Header.Set("Authorization", "Bearer secret")
		return r
	}

	t.Run("upstream item branches", func(t *testing.T) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req(http.MethodGet, "/proxy/upstreams/app"))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, req(http.MethodDelete, "/proxy/upstreams/app"))
		if rec.Code != http.StatusConflict {
			t.Fatalf("expected 409 for in-use upstream, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, req(http.MethodPatch, "/proxy/upstreams/app"))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		emptyReq := httptest.NewRequest(http.MethodGet, "/", nil)
		emptyReq.Header.Set("Authorization", "Bearer secret")
		h.handleUpstream(rec, emptyReq)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for missing upstream name, got %d", rec.Code)
		}
	})

	t.Run("route item branches", func(t *testing.T) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req(http.MethodGet, "/proxy/routes/r1"))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, req(http.MethodDelete, "/proxy/routes/r1"))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for delete error, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, req(http.MethodPatch, "/proxy/routes/r1"))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		emptyReq := httptest.NewRequest(http.MethodGet, "/", nil)
		emptyReq.Header.Set("Authorization", "Bearer secret")
		h.handleRoute(rec, emptyReq)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for missing route id, got %d", rec.Code)
		}
	})
}

func TestProxyHandlersSuccessPaths(t *testing.T) {
	svc := &stubProxyService{
		effectiveConfigFn: func(context.Context) (*service.EffectiveConfig, error) {
			return &service.EffectiveConfig{
				Upstreams: []store.Upstream{{Name: "app", URL: "https://app.example.com", Enabled: true}},
				Routes:    []store.Route{{ID: "route-1", Path: "/app/*", Target: "app", Enabled: true}},
			}, nil
		},
		listUpstreamsFn: func(context.Context) ([]store.Upstream, error) {
			return []store.Upstream{{Name: "app", URL: "https://app.example.com", Enabled: true}}, nil
		},
		upsertUpstreamFn: func(_ context.Context, upstream store.Upstream) (*store.Upstream, error) {
			upstream.Enabled = true
			return &upstream, nil
		},
		listRoutesFn: func(context.Context) ([]store.Route, error) {
			return []store.Route{{ID: "route-1", Path: "/app/*", Target: "app", Enabled: true}}, nil
		},
		upsertRouteFn: func(_ context.Context, route store.Route) (*store.Route, error) {
			route.ID = "route-1"
			route.Enabled = true
			return &route, nil
		},
		getUpstreamFn: func(context.Context, string) (*store.Upstream, error) {
			return &store.Upstream{Name: "app", URL: "https://app.example.com", Enabled: true}, nil
		},
		deleteUpstreamFn: func(context.Context, string) error { return nil },
		getRouteFn: func(context.Context, string) (*store.Route, error) {
			return &store.Route{ID: "route-1", Path: "/app/*", Target: "app", Enabled: true}, nil
		},
		deleteRouteFn: func(context.Context, string) error { return nil },
		simulateFn: func(context.Context, service.SimulateRequest) (*service.SimulateResponse, error) {
			return &service.SimulateResponse{Matched: true, Allowed: true, RewrittenPath: "/internal/home"}, nil
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
		want   int
	}{
		{"config get", http.MethodGet, "/api/v1/proxy/config", nil, http.StatusOK},
		{"upstreams get", http.MethodGet, "/api/v1/proxy/upstreams", nil, http.StatusOK},
		{"upstreams post", http.MethodPost, "/api/v1/proxy/upstreams", mustProxyJSON(store.Upstream{Name: "app", URL: "https://app.example.com"}), http.StatusOK},
		{"upstream get", http.MethodGet, "/api/v1/proxy/upstreams/app", nil, http.StatusOK},
		{"upstream delete", http.MethodDelete, "/api/v1/proxy/upstreams/app", nil, http.StatusNoContent},
		{"routes get", http.MethodGet, "/api/v1/proxy/routes", nil, http.StatusOK},
		{"routes post", http.MethodPost, "/api/v1/proxy/routes", mustProxyJSON(store.Route{Path: "/app/*", Target: "app"}), http.StatusOK},
		{"route get", http.MethodGet, "/api/v1/proxy/routes/route-1", nil, http.StatusOK},
		{"route delete", http.MethodDelete, "/api/v1/proxy/routes/route-1", nil, http.StatusNoContent},
		{"simulate post", http.MethodPost, "/api/v1/proxy/simulate", mustProxyJSON(service.SimulateRequest{Path: "/app/home", Method: http.MethodGet}), http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, authed(tc.method, tc.path, tc.body))
			if rec.Code != tc.want {
				t.Fatalf("expected %d, got %d body=%s", tc.want, rec.Code, rec.Body.String())
			}
		})
	}
}
