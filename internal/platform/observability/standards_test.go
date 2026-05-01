package observability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

func TestRoutePattern(t *testing.T) {
	r := chi.NewRouter()
	var routePattern string

	r.Get("/api/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		routePattern = RoutePattern(req)
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/users/123", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, "/api/users/{id}", routePattern)
}

func TestRoutePattern_EmptyForNilAndMissingContext(t *testing.T) {
	assert.Empty(t, RoutePattern(nil))

	req := httptest.NewRequest(http.MethodGet, "/api/users/123", nil)
	assert.Empty(t, RoutePattern(req))
}

func TestHTTPRouteLabel(t *testing.T) {
	tests := []struct {
		name     string
		route    string
		path     string
		expected string
	}{
		{
			name:     "prefers route pattern",
			route:    "/api/v1/users/{id}",
			path:     "/api/v1/users/123",
			expected: "/api/v1/users/{id}",
		},
		{
			name:     "normalizes numeric ids",
			path:     "/api/v1/users/123/profile",
			expected: "/api/v1/users/{id}/profile",
		},
		{
			name:     "normalizes uuid ids",
			path:     "/api/v1/users/550e8400-e29b-41d4-a716-446655440000/profile",
			expected: "/api/v1/users/{id}/profile",
		},
		{
			name:     "normalizes long token-like segments",
			path:     "/api/recovery/eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
			expected: "/api/recovery/{id}",
		},
		{
			name:     "drops query and fragment",
			path:     "/api/v1/users/123?include=details#section",
			expected: "/api/v1/users/{id}",
		},
		{
			name:     "defaults to root for empty value",
			expected: "/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, HTTPRouteLabel(tt.route, tt.path))
		})
	}
}

func TestNormalizeHTTPMethod(t *testing.T) {
	assert.Equal(t, "GET", NormalizeHTTPMethod("get"))
	assert.Equal(t, "PATCH", NormalizeHTTPMethod(" PATCH "))
	assert.Equal(t, "UNKNOWN", NormalizeHTTPMethod(""))
}

func TestNormalizeDBLabels(t *testing.T) {
	assert.Equal(t, "SELECT", NormalizeDBOperation("select"))
	assert.Equal(t, "UNKNOWN", NormalizeDBOperation(" "))
	assert.Equal(t, "ml_codes", NormalizeDBResource("ML_CODES"))
	assert.Equal(t, "unknown", NormalizeDBResource(" "))
	assert.Equal(t, "quoted_name", NormalizeDBResource(`"quoted name"`))
}

func TestHTTPRouteLabel_UsesPathWhenContextCancelled(t *testing.T) {
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	req := httptest.NewRequest("GET", "/api/v1/users/123", nil).WithContext(cancelledCtx)
	assert.Equal(t, "/api/v1/users/{id}", HTTPRouteLabel(RoutePattern(req), req.URL.Path))
}
