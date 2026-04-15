package service

import (
	"context"
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
