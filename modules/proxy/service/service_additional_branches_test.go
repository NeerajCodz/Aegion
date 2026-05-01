package service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/aegion/aegion/modules/proxy/store"
)

type proxyRepoStub struct {
	upstreams      []store.Upstream
	routes         []store.Route
	listUpErr      error
	listRoutesErr  error
	getUpstreamErr error
	getRouteErr    error
}

func (s *proxyRepoStub) ListUpstreams(context.Context) ([]store.Upstream, error) {
	if s.listUpErr != nil {
		return nil, s.listUpErr
	}
	return append([]store.Upstream(nil), s.upstreams...), nil
}
func (s *proxyRepoStub) GetUpstreamByName(context.Context, string) (*store.Upstream, error) {
	if s.getUpstreamErr != nil {
		return nil, s.getUpstreamErr
	}
	if len(s.upstreams) == 0 {
		return nil, store.ErrUpstreamNotFound
	}
	up := s.upstreams[0]
	return &up, nil
}
func (s *proxyRepoStub) UpsertUpstream(context.Context, store.Upstream) (*store.Upstream, error) {
	return nil, errors.New("not implemented")
}
func (s *proxyRepoStub) DeleteUpstream(context.Context, string) error { return nil }
func (s *proxyRepoStub) ListRoutes(context.Context) ([]store.Route, error) {
	if s.listRoutesErr != nil {
		return nil, s.listRoutesErr
	}
	return append([]store.Route(nil), s.routes...), nil
}
func (s *proxyRepoStub) GetRouteByID(context.Context, string) (*store.Route, error) {
	if s.getRouteErr != nil {
		return nil, s.getRouteErr
	}
	if len(s.routes) == 0 {
		return nil, store.ErrRouteNotFound
	}
	rt := s.routes[0]
	return &rt, nil
}
func (s *proxyRepoStub) UpsertRoute(context.Context, store.Route) (*store.Route, error) {
	return nil, errors.New("not implemented")
}
func (s *proxyRepoStub) DeleteRoute(context.Context, string) error { return nil }

func TestProxyServiceAdditionalBranches(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("boom")

	if svc := New(nil); svc == nil {
		t.Fatal("New(nil) should return service")
	}

	svc := New(&proxyRepoStub{listUpErr: boom})
	if _, err := svc.EffectiveConfig(ctx); !errors.Is(err, boom) {
		t.Fatalf("EffectiveConfig(upstreams error) = %v", err)
	}
	svc = New(&proxyRepoStub{listRoutesErr: boom})
	if _, err := svc.EffectiveConfig(ctx); !errors.Is(err, boom) {
		t.Fatalf("EffectiveConfig(routes error) = %v", err)
	}

	svc = New(&proxyRepoStub{listUpErr: boom})
	if _, err := svc.Simulate(ctx, SimulateRequest{}); !errors.Is(err, boom) {
		t.Fatalf("Simulate(list upstreams error) = %v", err)
	}
	svc = New(&proxyRepoStub{upstreams: []store.Upstream{{Name: "api", URL: "https://api.example.com", Enabled: true}}, listRoutesErr: boom})
	if _, err := svc.Simulate(ctx, SimulateRequest{}); !errors.Is(err, boom) {
		t.Fatalf("Simulate(list routes error) = %v", err)
	}

	svc = New(&proxyRepoStub{
		upstreams: []store.Upstream{{Name: "api", URL: "https://api.example.com", Enabled: true}},
		routes: []store.Route{{
			ID: "r1", Path: "/v1/*", Methods: []string{http.MethodGet}, Target: "missing", Enabled: true,
		}},
	})
	if _, err := svc.Simulate(ctx, SimulateRequest{Path: "/v1/a", Method: http.MethodGet}); !errors.Is(err, ErrInvalidProxyConfig) {
		t.Fatalf("Simulate(buildRuleEngine error) = %v", err)
	}

	svc = New(&proxyRepoStub{
		upstreams: []store.Upstream{{Name: "api", URL: "https://api.example.com", Enabled: true}},
		routes:    []store.Route{},
	})
	resp, err := svc.Simulate(ctx, SimulateRequest{Path: "", Method: ""})
	if err != nil || resp.Matched || resp.Allowed {
		t.Fatalf("Simulate(unmatched) resp=%#v err=%v", resp, err)
	}

	svc = New(&proxyRepoStub{
		upstreams: []store.Upstream{{Name: "api", URL: "https://api.example.com", Enabled: true}},
		routes: []store.Route{{
			ID: "r1", Path: "/v1/*", Methods: []string{http.MethodGet}, Target: "api", Enabled: true, RequireAuth: true, RequiredAAL: "aal1",
		}},
		getRouteErr: errors.New("no route details"),
	})
	resp, err = svc.Simulate(ctx, SimulateRequest{Path: "v1/home", Method: "get", Authenticated: true, AAL: ""})
	if err != nil || !resp.Matched || !resp.Allowed || resp.Rule != nil {
		t.Fatalf("Simulate(authenticated default aal) resp=%#v err=%v", resp, err)
	}

	svc = New(&proxyRepoStub{getUpstreamErr: boom})
	if _, err := svc.normalizeRoute(ctx, store.Route{Path: "/x", Target: "api"}); !errors.Is(err, boom) {
		t.Fatalf("normalizeRoute(get upstream error) = %v", err)
	}
	if _, err := svc.normalizeRoute(ctx, store.Route{Path: "", Target: "api"}); !errors.Is(err, ErrInvalidProxyConfig) {
		t.Fatalf("normalizeRoute(empty path) = %v", err)
	}
	if _, err := svc.normalizeRoute(ctx, store.Route{Path: "/x", Target: ""}); !errors.Is(err, ErrInvalidProxyConfig) {
		t.Fatalf("normalizeRoute(empty target) = %v", err)
	}
	svc = New(&proxyRepoStub{upstreams: []store.Upstream{{Name: "api", URL: "https://api.example.com", Enabled: true}}})
	if _, err := svc.normalizeRoute(ctx, store.Route{Path: "/x", Target: "api", RequiredAAL: "aal3"}); !errors.Is(err, ErrInvalidProxyConfig) {
		t.Fatalf("normalizeRoute(invalid aal) = %v", err)
	}

	svc = New(&proxyRepoStub{upstreams: []store.Upstream{{Name: "api", URL: "https://api.example.com", Enabled: true}}})
	normalized, err := svc.normalizeRoute(ctx, store.Route{
		ID:      "route-1",
		Path:    "/x/*",
		Target:  "api",
		Methods: []string{"get", "GET"},
		Rewrite: &store.Rewrite{StripPrefix: " ", AddPrefix: " "},
		RateLimit: &store.RateLimit{
			RequestsPerSecond: 0,
			BurstSize:         0,
		},
	})
	if err != nil || normalized.Rewrite != nil || normalized.RateLimit != nil || len(normalized.Methods) != 1 || normalized.Methods[0] != "GET" {
		t.Fatalf("normalizeRoute(trim branches) route=%#v err=%v", normalized, err)
	}
	if _, err := svc.normalizeRoute(ctx, store.Route{
		Path:   "/x/*",
		Target: "api",
		RateLimit: &store.RateLimit{
			RequestsPerSecond: 10,
			BurstSize:         0,
		},
	}); !errors.Is(err, ErrInvalidProxyConfig) {
		t.Fatalf("normalizeRoute(partial rate limit) = %v", err)
	}

	if _, err := toCoreRule(store.Route{Path: "", Target: "api", Enabled: true}); !errors.Is(err, ErrInvalidProxyConfig) {
		t.Fatalf("toCoreRule(invalid) = %v", err)
	}

	up, err := normalizeUpstream(store.Upstream{Name: "api", URL: "https://api.example.com", HealthCheck: "/health", CreatedAt: time.Time{}})
	if err != nil || up.CreatedAt.IsZero() {
		t.Fatalf("normalizeUpstream(createdAt default) upstream=%#v err=%v", up, err)
	}
}
