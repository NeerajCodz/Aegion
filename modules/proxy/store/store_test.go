package store

import (
	"context"
	"errors"
	"testing"
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
