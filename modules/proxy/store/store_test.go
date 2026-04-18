package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemoryStoreUpstreamAndRouteLifecycle(t *testing.T) {
	repo := New()
	ctx := context.Background()

	upstream, err := repo.UpsertUpstream(ctx, Upstream{
		Name:    "app",
		URL:     "https://app.example.com",
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("upsert upstream: %v", err)
	}
	if upstream.ID.String() == "" {
		t.Fatal("expected upstream id to be generated")
	}

	route, err := repo.UpsertRoute(ctx, Route{
		Path:    "/app/*",
		Target:  "app",
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("upsert route: %v", err)
	}

	listedRoutes, err := repo.ListRoutes(ctx)
	if err != nil {
		t.Fatalf("list routes: %v", err)
	}
	if len(listedRoutes) != 1 || listedRoutes[0].ID != route.ID {
		t.Fatalf("unexpected routes: %+v", listedRoutes)
	}

	if err := repo.DeleteUpstream(ctx, "app"); !errors.Is(err, ErrUpstreamInUse) {
		t.Fatalf("expected ErrUpstreamInUse, got %v", err)
	}

	if err := repo.DeleteRoute(ctx, route.ID); err != nil {
		t.Fatalf("delete route: %v", err)
	}
	if err := repo.DeleteUpstream(ctx, "app"); err != nil {
		t.Fatalf("delete upstream: %v", err)
	}
}

func TestMemoryStoreListAndUpdateBranches(t *testing.T) {
	repo := New()
	ctx := context.Background()

	if _, err := repo.UpsertUpstream(ctx, Upstream{
		Name:    "b-upstream",
		URL:     "https://b.example.com",
		Headers: map[string]string{"X-Test": "one"},
		CircuitBreaker: &CircuitBreaker{
			FailureThreshold: 2,
		},
		Enabled: true,
	}); err != nil {
		t.Fatalf("upsert b-upstream: %v", err)
	}
	if _, err := repo.UpsertUpstream(ctx, Upstream{
		Name:    "a-upstream",
		URL:     "https://a.example.com",
		Enabled: true,
	}); err != nil {
		t.Fatalf("upsert a-upstream: %v", err)
	}

	upstreams, err := repo.ListUpstreams(ctx)
	if err != nil {
		t.Fatalf("list upstreams: %v", err)
	}
	if len(upstreams) != 2 || upstreams[0].Name != "a-upstream" || upstreams[1].Name != "b-upstream" {
		t.Fatalf("unexpected upstream order: %+v", upstreams)
	}
	upstreams[1].Headers["X-Test"] = "mutated"
	stored, err := repo.GetUpstreamByName(ctx, "b-upstream")
	if err != nil {
		t.Fatalf("get upstream: %v", err)
	}
	if stored.Headers["X-Test"] != "one" {
		t.Fatalf("expected cloned upstream headers to stay unchanged, got %+v", stored.Headers)
	}

	baseCreatedAt := time.Now().UTC().Add(-time.Hour)
	if _, err := repo.UpsertRoute(ctx, Route{
		ID:        "route-b",
		Path:      "/b/*",
		Target:    "b-upstream",
		Priority:  50,
		CreatedAt: baseCreatedAt,
		Enabled:   true,
	}); err != nil {
		t.Fatalf("upsert route-b: %v", err)
	}
	updatedRoute, err := repo.UpsertRoute(ctx, Route{
		ID:        "route-b",
		Path:      "/b-updated/*",
		Target:    "b-upstream",
		Priority:  50,
		CreatedAt: time.Now().UTC(),
		Enabled:   true,
	})
	if err != nil {
		t.Fatalf("update route-b: %v", err)
	}
	if !updatedRoute.CreatedAt.Equal(baseCreatedAt) {
		t.Fatalf("expected created_at to be preserved on update, got %v want %v", updatedRoute.CreatedAt, baseCreatedAt)
	}
	if _, err := repo.UpsertRoute(ctx, Route{
		ID:       "route-a",
		Path:     "/a/*",
		Target:   "a-upstream",
		Priority: 50,
		Enabled:  true,
	}); err != nil {
		t.Fatalf("upsert route-a: %v", err)
	}

	routes, err := repo.ListRoutes(ctx)
	if err != nil {
		t.Fatalf("list routes: %v", err)
	}
	if len(routes) < 2 || routes[0].ID != "route-a" || routes[1].ID != "route-b" {
		t.Fatalf("expected tie-break sort by id asc for equal priority, got %+v", routes)
	}
}
