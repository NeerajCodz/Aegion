package service

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/aegion/aegion/modules/proxy/store"
)

func TestUpsertUpstreamAndRouteSimulation(t *testing.T) {
	repo := store.New()
	svc := New(repo)
	ctx := context.Background()

	if _, err := svc.UpsertUpstream(ctx, store.Upstream{
		Name:        "app",
		URL:         "https://app.example.com",
		HealthCheck: "ready",
		Enabled:     true,
	}); err != nil {
		t.Fatalf("upsert upstream: %v", err)
	}

	savedRoute, err := svc.UpsertRoute(ctx, store.Route{
		Path:        "/dashboard/*",
		Methods:     []string{"get"},
		RequireAuth: true,
		RequiredAAL: "aal1",
		Target:      "app",
		Enabled:     true,
		Rewrite: &store.Rewrite{
			StripPrefix: "/dashboard",
			AddPrefix:   "/internal",
		},
	})
	if err != nil {
		t.Fatalf("upsert route: %v", err)
	}
	if savedRoute.ID == "" {
		t.Fatal("expected route id to be generated")
	}

	resp, err := svc.Simulate(ctx, SimulateRequest{
		Path:   "/dashboard/home",
		Method: "GET",
	})
	if err != nil {
		t.Fatalf("simulate anonymous request: %v", err)
	}
	if !resp.Matched {
		t.Fatal("expected rule to match")
	}
	if resp.Allowed {
		t.Fatal("expected anonymous request to be denied")
	}
	if resp.RewrittenPath != "/internal/home" {
		t.Fatalf("expected rewritten path /internal/home, got %q", resp.RewrittenPath)
	}

	allowedResp, err := svc.Simulate(ctx, SimulateRequest{
		Path:          "/dashboard/home",
		Method:        "GET",
		Authenticated: true,
		AAL:           "aal1",
	})
	if err != nil {
		t.Fatalf("simulate authenticated request: %v", err)
	}
	if !allowedResp.Allowed {
		t.Fatalf("expected authenticated request to be allowed, got denial %q", allowedResp.DenialReason)
	}
}

func TestUpsertRouteRejectsMissingUpstream(t *testing.T) {
	svc := New(store.New())
	_, err := svc.UpsertRoute(context.Background(), store.Route{
		Path:    "/app/*",
		Target:  "missing",
		Enabled: true,
	})
	if err == nil {
		t.Fatal("expected missing upstream error")
	}
}

func TestServiceCRUDWrappersAndEffectiveConfig(t *testing.T) {
	repo := store.New()
	svc := New(repo)
	ctx := context.Background()

	upstream, err := svc.UpsertUpstream(ctx, store.Upstream{
		Name:    "API",
		URL:     "https://api.example.com/",
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("upsert upstream: %v", err)
	}
	if upstream.Name != "api" {
		t.Fatalf("expected normalized upstream name, got %q", upstream.Name)
	}

	upstreams, err := svc.ListUpstreams(ctx)
	if err != nil {
		t.Fatalf("list upstreams: %v", err)
	}
	if len(upstreams) != 1 {
		t.Fatalf("expected one upstream, got %d", len(upstreams))
	}

	fetchedUpstream, err := svc.GetUpstream(ctx, "API")
	if err != nil {
		t.Fatalf("get upstream: %v", err)
	}
	if fetchedUpstream.URL != "https://api.example.com" {
		t.Fatalf("expected normalized upstream URL, got %q", fetchedUpstream.URL)
	}

	route, err := svc.UpsertRoute(ctx, store.Route{
		Path:    "/v1/*",
		Methods: []string{"get"},
		Target:  "api",
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("upsert route: %v", err)
	}

	routes, err := svc.ListRoutes(ctx)
	if err != nil {
		t.Fatalf("list routes: %v", err)
	}
	if len(routes) != 1 || routes[0].ID != route.ID {
		t.Fatalf("unexpected routes: %+v", routes)
	}

	gotRoute, err := svc.GetRoute(ctx, route.ID)
	if err != nil {
		t.Fatalf("get route: %v", err)
	}
	if gotRoute.Path != "/v1/*" || len(gotRoute.Methods) != 1 || gotRoute.Methods[0] != http.MethodGet {
		t.Fatalf("unexpected normalized route: %+v", gotRoute)
	}

	effective, err := svc.EffectiveConfig(ctx)
	if err != nil {
		t.Fatalf("effective config: %v", err)
	}
	if len(effective.Upstreams) != 1 || len(effective.Routes) != 1 {
		t.Fatalf("unexpected effective config: %+v", effective)
	}

	if err := svc.DeleteRoute(ctx, route.ID); err != nil {
		t.Fatalf("delete route: %v", err)
	}
	if _, err := svc.GetRoute(ctx, route.ID); !errors.Is(err, store.ErrRouteNotFound) {
		t.Fatalf("expected route not found after delete, got %v", err)
	}

	if err := svc.DeleteUpstream(ctx, "api"); err != nil {
		t.Fatalf("delete upstream: %v", err)
	}
	if _, err := svc.GetUpstream(ctx, "api"); !errors.Is(err, store.ErrUpstreamNotFound) {
		t.Fatalf("expected upstream not found after delete, got %v", err)
	}
}

func TestNormalizeHeaderMapAndUpstreamValidation(t *testing.T) {
	normalized := normalizeHeaderMap(map[string]string{
		" x-test ": " one ",
		"X-Test":   "two",
		" ":        "value",
		"drop":     " ",
	})
	// Both " x-test " and "X-Test" normalize to "X-Test", so only one should remain
	// Map iteration order is random, so either "one" or "two" could be the final value
	if len(normalized) != 1 {
		t.Fatalf("unexpected normalized headers count: %d, headers: %+v", len(normalized), normalized)
	}
	val, ok := normalized["X-Test"]
	if !ok {
		t.Fatalf("expected X-Test header key, got %+v", normalized)
	}
	if val != "one" && val != "two" {
		t.Fatalf("expected X-Test value to be 'one' or 'two', got %s", val)
	}
	empty := normalizeHeaderMap(nil)
	if len(empty) != 0 {
		t.Fatalf("expected empty header map for nil input, got %+v", empty)
	}

	if _, err := normalizeUpstream(store.Upstream{Name: "", URL: "https://api.example.com"}); !errors.Is(err, ErrInvalidProxyConfig) {
		t.Fatalf("expected invalid config for empty name, got %v", err)
	}
	if _, err := normalizeUpstream(store.Upstream{Name: "api", URL: "ftp://api.example.com"}); !errors.Is(err, ErrInvalidProxyConfig) {
		t.Fatalf("expected invalid config for unsupported URL scheme, got %v", err)
	}
	if _, err := normalizeUpstream(store.Upstream{Name: "api", URL: "https://api.example.com", Timeout: "nope"}); !errors.Is(err, ErrInvalidProxyConfig) {
		t.Fatalf("expected invalid config for bad timeout, got %v", err)
	}
	if _, err := normalizeUpstream(store.Upstream{
		Name: "api", URL: "https://api.example.com",
		CircuitBreaker: &store.CircuitBreaker{Timeout: "invalid"},
	}); !errors.Is(err, ErrInvalidProxyConfig) {
		t.Fatalf("expected invalid config for bad circuit breaker timeout, got %v", err)
	}

	valid, err := normalizeUpstream(store.Upstream{
		Name:        " API ",
		URL:         "https://api.example.com/",
		HealthCheck: "ready",
		Timeout:     "1s",
		Headers: map[string]string{
			" x-test ": " value ",
		},
		CircuitBreaker: &store.CircuitBreaker{
			FailureThreshold: 5,
			Timeout:          "30s",
			SuccessThreshold: 2,
		},
	})
	if err != nil {
		t.Fatalf("normalize valid upstream: %v", err)
	}
	if valid.Name != "api" || valid.URL != "https://api.example.com" || valid.HealthCheck != "/ready" {
		t.Fatalf("unexpected normalized upstream: %+v", valid)
	}
	if valid.Headers["X-Test"] != "value" {
		t.Fatalf("expected normalized upstream headers, got %+v", valid.Headers)
	}
	if valid.CircuitBreaker == nil || valid.CircuitBreaker.Timeout != "30s" {
		t.Fatalf("expected circuit breaker preserved, got %+v", valid.CircuitBreaker)
	}
}
