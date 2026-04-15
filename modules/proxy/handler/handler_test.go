package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aegion/aegion/modules/proxy/service"
	"github.com/aegion/aegion/modules/proxy/store"
)

type stubProxyService struct {
	config       *service.EffectiveConfig
	upstreams    []store.Upstream
	routeList    []store.Route
	upstream     *store.Upstream
	route        *store.Route
	simulateResp *service.SimulateResponse
	deleteErr    error
}

func (s *stubProxyService) EffectiveConfig(context.Context) (*service.EffectiveConfig, error) {
	return s.config, nil
}

func (s *stubProxyService) ListUpstreams(context.Context) ([]store.Upstream, error) {
	return s.upstreams, nil
}

func (s *stubProxyService) GetUpstream(context.Context, string) (*store.Upstream, error) {
	return s.upstream, nil
}

func (s *stubProxyService) UpsertUpstream(context.Context, store.Upstream) (*store.Upstream, error) {
	if s.upstream != nil {
		return s.upstream, nil
	}
	return &store.Upstream{Name: "app", URL: "https://app.example.com", Enabled: true}, nil
}

func (s *stubProxyService) DeleteUpstream(context.Context, string) error {
	return s.deleteErr
}

func (s *stubProxyService) ListRoutes(context.Context) ([]store.Route, error) {
	return s.routeList, nil
}

func (s *stubProxyService) GetRoute(context.Context, string) (*store.Route, error) {
	return s.route, nil
}

func (s *stubProxyService) UpsertRoute(context.Context, store.Route) (*store.Route, error) {
	if s.route != nil {
		return s.route, nil
	}
	return &store.Route{ID: "route-1", Path: "/app/*", Target: "app", Enabled: true}, nil
}

func (s *stubProxyService) DeleteRoute(context.Context, string) error {
	return s.deleteErr
}

func (s *stubProxyService) Simulate(context.Context, service.SimulateRequest) (*service.SimulateResponse, error) {
	return s.simulateResp, nil
}

func TestConfigRequiresManagementToken(t *testing.T) {
	h := New(&stubProxyService{})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/proxy/config", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}
}

func TestProxyRoutesSupportCRUDAndSimulation(t *testing.T) {
	svc := &stubProxyService{
		config: &service.EffectiveConfig{
			Upstreams: []store.Upstream{{Name: "app", URL: "https://app.example.com", Enabled: true}},
			Routes:    []store.Route{{ID: "route-1", Path: "/app/*", Target: "app", Enabled: true}},
		},
		upstreams: []store.Upstream{{Name: "app", URL: "https://app.example.com", Enabled: true}},
		routeList: []store.Route{{ID: "route-1", Path: "/app/*", Target: "app", Enabled: true}},
		upstream:  &store.Upstream{Name: "app", URL: "https://app.example.com", Enabled: true},
		route:     &store.Route{ID: "route-1", Path: "/app/*", Target: "app", Enabled: true},
		simulateResp: &service.SimulateResponse{
			Matched:       true,
			Allowed:       true,
			RewrittenPath: "/internal/home",
		},
	}
	h := New(svc, Config{ManagementToken: "secret"})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	authHeader := http.Header{}
	authHeader.Set("Authorization", "Bearer secret")

	t.Run("list config", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/proxy/config", nil)
		req.Header = authHeader.Clone()
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
	})

	t.Run("upsert upstream", func(t *testing.T) {
		body, _ := json.Marshal(store.Upstream{Name: "app", URL: "https://app.example.com", Enabled: true})
		req := httptest.NewRequest(http.MethodPost, "/proxy/upstreams", bytes.NewReader(body))
		req.Header = authHeader.Clone()
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
	})

	t.Run("upsert route", func(t *testing.T) {
		body, _ := json.Marshal(store.Route{Path: "/app/*", Target: "app", Enabled: true})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/proxy/routes", bytes.NewReader(body))
		req.Header = authHeader.Clone()
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
	})

	t.Run("simulate", func(t *testing.T) {
		body, _ := json.Marshal(service.SimulateRequest{Path: "/app/home", Method: http.MethodGet})
		req := httptest.NewRequest(http.MethodPost, "/proxy/simulate", bytes.NewReader(body))
		req.Header = authHeader.Clone()
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
		}
	})
}
