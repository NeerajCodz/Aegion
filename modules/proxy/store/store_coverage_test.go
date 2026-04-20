package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type testScanProxy struct {
	values []any
	err    error
}

func (s testScanProxy) Scan(dest ...any) error {
	if s.err != nil {
		return s.err
	}
	for i := range dest {
		switch d := dest[i].(type) {
		case *uuid.UUID:
			*d = s.values[i].(uuid.UUID)
		case *string:
			*d = s.values[i].(string)
		case *[]byte:
			*d = s.values[i].([]byte)
		case *bool:
			*d = s.values[i].(bool)
		case *int:
			*d = s.values[i].(int)
		case *time.Time:
			*d = s.values[i].(time.Time)
		default:
			return errors.New("unsupported destination type")
		}
	}
	return nil
}

func TestMemoryStoreAdditionalBranches(t *testing.T) {
	ctx := context.Background()
	s := New()
	now := time.Now().UTC().Round(0)

	if _, err := s.UpsertUpstream(ctx, Upstream{Name: "   "}); !errors.Is(err, ErrUpstreamNotFound) {
		t.Fatalf("UpsertUpstream(empty name) error = %v, want %v", err, ErrUpstreamNotFound)
	}

	up1, err := s.UpsertUpstream(ctx, Upstream{
		Name:      " app ",
		URL:       "https://app.example.com",
		Headers:   map[string]string{"x-a": "1"},
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("UpsertUpstream(create) error = %v", err)
	}
	up2, err := s.UpsertUpstream(ctx, Upstream{
		Name:    "app",
		URL:     "https://app-v2.example.com",
		Headers: map[string]string{"x-b": "2"},
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("UpsertUpstream(update) error = %v", err)
	}
	if up2.ID != up1.ID {
		t.Fatalf("UpsertUpstream(update) ID = %s, want %s", up2.ID, up1.ID)
	}
	if !up2.CreatedAt.Equal(up1.CreatedAt) {
		t.Fatalf("UpsertUpstream(update) CreatedAt = %v, want %v", up2.CreatedAt, up1.CreatedAt)
	}

	gotUp, err := s.GetUpstreamByName(ctx, "APP")
	if err != nil {
		t.Fatalf("GetUpstreamByName() error = %v", err)
	}
	gotUp.Headers["x-b"] = "mutated"
	gotUpAgain, err := s.GetUpstreamByName(ctx, "app")
	if err != nil {
		t.Fatalf("GetUpstreamByName(second) error = %v", err)
	}
	if gotUpAgain.Headers["x-b"] != "2" {
		t.Fatalf("GetUpstreamByName() should return clone, got mutated map")
	}

	if _, err := s.GetUpstreamByName(ctx, "missing"); !errors.Is(err, ErrUpstreamNotFound) {
		t.Fatalf("GetUpstreamByName(missing) error = %v, want %v", err, ErrUpstreamNotFound)
	}

	routeA, err := s.UpsertRoute(ctx, Route{
		ID:       "route-a",
		Path:     "/a/*",
		Target:   "app",
		Priority: 5,
		Enabled:  true,
	})
	if err != nil {
		t.Fatalf("UpsertRoute(route-a) error = %v", err)
	}
	routeB, err := s.UpsertRoute(ctx, Route{
		ID:       "route-b",
		Path:     "/b/*",
		Target:   "app",
		Priority: 10,
		Enabled:  true,
	})
	if err != nil {
		t.Fatalf("UpsertRoute(route-b) error = %v", err)
	}
	routeGenerated, err := s.UpsertRoute(ctx, Route{
		Path:     "/generated/*",
		Target:   "app",
		Priority: 1,
		Enabled:  true,
	})
	if err != nil {
		t.Fatalf("UpsertRoute(generated) error = %v", err)
	}
	if routeGenerated.ID == "" {
		t.Fatalf("UpsertRoute(generated) expected non-empty ID")
	}

	gotRoute, err := s.GetRouteByID(ctx, routeA.ID)
	if err != nil {
		t.Fatalf("GetRouteByID() error = %v", err)
	}
	gotRoute.Methods = append(gotRoute.Methods, "PATCH")
	gotRouteAgain, err := s.GetRouteByID(ctx, routeA.ID)
	if err != nil {
		t.Fatalf("GetRouteByID(second) error = %v", err)
	}
	if len(gotRouteAgain.Methods) != 0 {
		t.Fatalf("GetRouteByID() should return clone, methods mutated to %#v", gotRouteAgain.Methods)
	}

	listedRoutes, err := s.ListRoutes(ctx)
	if err != nil {
		t.Fatalf("ListRoutes() error = %v", err)
	}
	if len(listedRoutes) != 3 || listedRoutes[0].ID != routeB.ID {
		t.Fatalf("ListRoutes() ordering unexpected: %#v", listedRoutes)
	}

	if err := s.DeleteUpstream(ctx, "app"); !errors.Is(err, ErrUpstreamInUse) {
		t.Fatalf("DeleteUpstream(in use) error = %v, want %v", err, ErrUpstreamInUse)
	}
	if err := s.DeleteRoute(ctx, routeA.ID); err != nil {
		t.Fatalf("DeleteRoute(route-a) error = %v", err)
	}
	if err := s.DeleteRoute(ctx, routeB.ID); err != nil {
		t.Fatalf("DeleteRoute(route-b) error = %v", err)
	}
	if err := s.DeleteRoute(ctx, routeGenerated.ID); err != nil {
		t.Fatalf("DeleteRoute(generated) error = %v", err)
	}
	if err := s.DeleteRoute(ctx, routeA.ID); !errors.Is(err, ErrRouteNotFound) {
		t.Fatalf("DeleteRoute(missing) error = %v, want %v", err, ErrRouteNotFound)
	}

	if _, err := s.GetRouteByID(ctx, routeA.ID); !errors.Is(err, ErrRouteNotFound) {
		t.Fatalf("GetRouteByID(missing) error = %v, want %v", err, ErrRouteNotFound)
	}
	if err := s.DeleteUpstream(ctx, "app"); err != nil {
		t.Fatalf("DeleteUpstream(final) error = %v", err)
	}
	if err := s.DeleteUpstream(ctx, "app"); !errors.Is(err, ErrUpstreamNotFound) {
		t.Fatalf("DeleteUpstream(missing) error = %v, want %v", err, ErrUpstreamNotFound)
	}
}

func TestCloneAndNormalizeHelpers(t *testing.T) {
	up := Upstream{
		Name:    "app",
		Headers: map[string]string{"a": "1"},
		CircuitBreaker: &CircuitBreaker{
			FailureThreshold: 5,
			Timeout:          "10s",
			SuccessThreshold: 2,
		},
	}
	upClone := cloneUpstream(up)
	upClone.Headers["a"] = "mutated"
	upClone.CircuitBreaker.Timeout = "99s"
	if up.Headers["a"] != "1" || up.CircuitBreaker.Timeout != "10s" {
		t.Fatalf("cloneUpstream should deep-clone nested references")
	}

	route := Route{
		ID:           "r1",
		Methods:      []string{"GET"},
		Capabilities: []string{"admin:read"},
		Headers:      map[string]string{"h": "v"},
		RateLimit:    &RateLimit{RequestsPerSecond: 10},
		Rewrite:      &Rewrite{StripPrefix: "/x"},
	}
	routeClone := cloneRoute(route)
	routeClone.Methods[0] = "POST"
	routeClone.Capabilities[0] = "admin:write"
	routeClone.Headers["h"] = "mutated"
	routeClone.RateLimit.RequestsPerSecond = 99
	routeClone.Rewrite.StripPrefix = "/y"
	if route.Methods[0] != "GET" || route.Capabilities[0] != "admin:read" || route.Headers["h"] != "v" ||
		route.RateLimit.RequestsPerSecond != 10 || route.Rewrite.StripPrefix != "/x" {
		t.Fatalf("cloneRoute should deep-clone nested references")
	}

	if got := cloneStringMap(nil); len(got) != 0 {
		t.Fatalf("cloneStringMap(nil) len = %d, want 0", len(got))
	}
	if got := normalizeName("  APP  "); got != "app" {
		t.Fatalf("normalizeName() = %q, want app", got)
	}
}

func TestScanUpstreamAndRouteBranches(t *testing.T) {
	now := time.Now().UTC().Round(0)
	id := uuid.New()

	upValues := []any{
		id, "app", "https://app.example.com", "/health", "5s", 100,
		[]byte(`{"x":"1"}`), []byte(`{"failure_threshold":5,"timeout":"10s","success_threshold":2}`),
		true, now, now,
	}

	t.Run("scanUpstream scan error", func(t *testing.T) {
		_, err := scanUpstream(testScanProxy{err: errors.New("scan failed")})
		if err == nil || err.Error() != "scan failed" {
			t.Fatalf("scanUpstream(scan error) = %v, want scan failed", err)
		}
	})
	t.Run("scanUpstream headers json error", func(t *testing.T) {
		values := append([]any(nil), upValues...)
		values[6] = []byte(`{`)
		_, err := scanUpstream(testScanProxy{values: values})
		if err == nil {
			t.Fatalf("scanUpstream(headers json error) expected error")
		}
	})
	t.Run("scanUpstream circuit breaker json error", func(t *testing.T) {
		values := append([]any(nil), upValues...)
		values[7] = []byte(`{`)
		_, err := scanUpstream(testScanProxy{values: values})
		if err == nil {
			t.Fatalf("scanUpstream(circuit breaker json error) expected error")
		}
	})
	t.Run("scanUpstream success with empty maps", func(t *testing.T) {
		values := append([]any(nil), upValues...)
		values[6] = []byte(``)
		values[7] = []byte(`{}`)
		got, err := scanUpstream(testScanProxy{values: values})
		if err != nil {
			t.Fatalf("scanUpstream(success) error = %v", err)
		}
		if got.ID != id || got.CircuitBreaker != nil {
			t.Fatalf("scanUpstream(success) unexpected value: %#v", got)
		}
		if got.Headers == nil || len(got.Headers) != 0 {
			t.Fatalf("scanUpstream(success) expected empty headers map, got %#v", got.Headers)
		}
	})

	routeValues := []any{
		"route-1", "/api/*", []byte(`["GET","POST"]`), true, "aal2", []byte(`["cap:one"]`),
		[]byte(`{"requests_per_second":10,"burst_size":20,"by_ip":true}`), "app", 10,
		[]byte(`{"x":"1"}`), []byte(`{"strip_prefix":"/api"}`), true, "desc", now, now,
	}

	t.Run("scanRoute scan error", func(t *testing.T) {
		_, err := scanRoute(testScanProxy{err: errors.New("scan failed")})
		if err == nil || err.Error() != "scan failed" {
			t.Fatalf("scanRoute(scan error) = %v, want scan failed", err)
		}
	})
	t.Run("scanRoute methods json error", func(t *testing.T) {
		values := append([]any(nil), routeValues...)
		values[2] = []byte(`{`)
		_, err := scanRoute(testScanProxy{values: values})
		if err == nil {
			t.Fatalf("scanRoute(methods json error) expected error")
		}
	})
	t.Run("scanRoute capabilities json error", func(t *testing.T) {
		values := append([]any(nil), routeValues...)
		values[5] = []byte(`{`)
		_, err := scanRoute(testScanProxy{values: values})
		if err == nil {
			t.Fatalf("scanRoute(capabilities json error) expected error")
		}
	})
	t.Run("scanRoute headers json error", func(t *testing.T) {
		values := append([]any(nil), routeValues...)
		values[9] = []byte(`{`)
		_, err := scanRoute(testScanProxy{values: values})
		if err == nil {
			t.Fatalf("scanRoute(headers json error) expected error")
		}
	})
	t.Run("scanRoute rate limit json error", func(t *testing.T) {
		values := append([]any(nil), routeValues...)
		values[6] = []byte(`{`)
		_, err := scanRoute(testScanProxy{values: values})
		if err == nil {
			t.Fatalf("scanRoute(rate json error) expected error")
		}
	})
	t.Run("scanRoute rewrite json error", func(t *testing.T) {
		values := append([]any(nil), routeValues...)
		values[10] = []byte(`{`)
		_, err := scanRoute(testScanProxy{values: values})
		if err == nil {
			t.Fatalf("scanRoute(rewrite json error) expected error")
		}
	})
	t.Run("scanRoute success with empty optional fields", func(t *testing.T) {
		values := append([]any(nil), routeValues...)
		values[2] = []byte(``)
		values[5] = []byte(``)
		values[6] = []byte(`{}`)
		values[9] = []byte(``)
		values[10] = []byte(`{}`)
		got, err := scanRoute(testScanProxy{values: values})
		if err != nil {
			t.Fatalf("scanRoute(success) error = %v", err)
		}
		if got.ID != "route-1" || got.RateLimit != nil || got.Rewrite != nil {
			t.Fatalf("scanRoute(success) unexpected value: %#v", got)
		}
		if len(got.Methods) != 0 || len(got.Capabilities) != 0 || len(got.Headers) != 0 {
			t.Fatalf("scanRoute(success) expected empty collections, got methods=%#v caps=%#v headers=%#v", got.Methods, got.Capabilities, got.Headers)
		}
	})
}

func TestNormalizeWrappersAndUUIDText(t *testing.T) {
	if got, ok := normalizeCircuitBreaker(nil).(map[string]any); !ok || len(got) != 0 {
		t.Fatalf("normalizeCircuitBreaker(nil) unexpected value %#v", normalizeCircuitBreaker(nil))
	}
	cb := &CircuitBreaker{FailureThreshold: 2}
	if normalizeCircuitBreaker(cb) != cb {
		t.Fatalf("normalizeCircuitBreaker(non-nil) should return same pointer")
	}

	if got, ok := normalizeRateLimit(nil).(map[string]any); !ok || len(got) != 0 {
		t.Fatalf("normalizeRateLimit(nil) unexpected value %#v", normalizeRateLimit(nil))
	}
	rate := &RateLimit{RequestsPerSecond: 1}
	if normalizeRateLimit(rate) != rate {
		t.Fatalf("normalizeRateLimit(non-nil) should return same pointer")
	}

	if got, ok := normalizeRewrite(nil).(map[string]any); !ok || len(got) != 0 {
		t.Fatalf("normalizeRewrite(nil) unexpected value %#v", normalizeRewrite(nil))
	}
	rewrite := &Rewrite{StripPrefix: "/api"}
	if normalizeRewrite(rewrite) != rewrite {
		t.Fatalf("normalizeRewrite(non-nil) should return same pointer")
	}

	if got := uuidText(uuid.Nil); got != "" {
		t.Fatalf("uuidText(uuid.Nil) = %q, want empty", got)
	}
	id := uuid.New()
	if got := uuidText(id); got != id.String() {
		t.Fatalf("uuidText(non-nil) = %q, want %q", got, id.String())
	}
}
