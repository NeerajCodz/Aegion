package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestNormalizeAdminPath(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty defaults to aegion", input: "", want: "/aegion"},
		{name: "trim and add leading slash", input: "  admin  ", want: "/admin"},
		{name: "trim trailing slash", input: "/admin/", want: "/admin"},
		{name: "root stays root", input: "/", want: "/"},
		{name: "trim many trailing slashes", input: "/aegion///", want: "/aegion"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeAdminPath(tc.input)
			if got != tc.want {
				t.Fatalf("normalizeAdminPath(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestIsDevMode(t *testing.T) {
	prevEnv := os.Getenv("AEGION_ENV")
	prevEnvironment := os.Getenv("AEGION_ENVIRONMENT")
	defer func() {
		_ = os.Setenv("AEGION_ENV", prevEnv)
		_ = os.Setenv("AEGION_ENVIRONMENT", prevEnvironment)
	}()

	_ = os.Unsetenv("AEGION_ENV")
	_ = os.Unsetenv("AEGION_ENVIRONMENT")
	if isDevMode() {
		t.Fatalf("expected false when env variables are not set")
	}

	_ = os.Setenv("AEGION_ENV", "development")
	if !isDevMode() {
		t.Fatalf("expected true for AEGION_ENV=development")
	}

	_ = os.Setenv("AEGION_ENV", "")
	_ = os.Setenv("AEGION_ENVIRONMENT", "local")
	if !isDevMode() {
		t.Fatalf("expected true for AEGION_ENVIRONMENT=local")
	}
}

func TestHandleDashboardConfigUsesNormalizedPath(t *testing.T) {
	s := &Server{
		Config: &Config{},
	}
	s.Config.Admin.Path = " admin "

	req := httptest.NewRequest(http.MethodGet, "/api/admin/dashboard/config", nil)
	rec := httptest.NewRecorder()
	s.handleDashboardConfig(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.Contains(contentType, "application/json") {
		t.Fatalf("expected JSON response, got %q", contentType)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}
	if body["base_path"] != "/admin" {
		t.Fatalf("expected base_path=/admin, got %q", body["base_path"])
	}
}

func TestRegisterWithCore(t *testing.T) {
	tests := []struct {
		name          string
		serviceURL    string
		address       string
		apiKey        string
		statusCode    int
		wantErr       bool
		wantAuth      string
		wantEndpoint  string
		wantHealthURL string
	}{
		{
			name:       "skips when core URL missing",
			serviceURL: "",
			wantErr:    false,
		},
		{
			name:          "uses localhost for wildcard bind",
			address:       "0.0.0.0",
			apiKey:        "secret",
			statusCode:    http.StatusCreated,
			wantErr:       false,
			wantAuth:      "Bearer secret",
			wantEndpoint:  "http://localhost:8082",
			wantHealthURL: "http://localhost:8082/health",
		},
		{
			name:          "propagates registration failure",
			address:       "127.0.0.1",
			statusCode:    http.StatusBadGateway,
			wantErr:       true,
			wantAuth:      "",
			wantEndpoint:  "http://127.0.0.1:8082",
			wantHealthURL: "http://127.0.0.1:8082/health",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var (
				gotAuth string
				gotBody RegistrationRequest
			)

			serverURL := tc.serviceURL
			if tc.serviceURL == "" && tc.statusCode != 0 {
				core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					gotAuth = r.Header.Get("Authorization")
					if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
						t.Fatalf("failed to decode request body: %v", err)
					}
					w.WriteHeader(tc.statusCode)
				}))
				defer core.Close()
				serverURL = core.URL
			} else if tc.statusCode != 0 {
				core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					gotAuth = r.Header.Get("Authorization")
					if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
						t.Fatalf("failed to decode request body: %v", err)
					}
					w.WriteHeader(tc.statusCode)
				}))
				defer core.Close()
				serverURL = core.URL
			}

			s := &Server{
				Config: &Config{},
			}
			s.Config.Core.ServiceURL = serverURL
			s.Config.Core.APIKey = tc.apiKey
			s.Config.Server.Address = tc.address
			s.Config.Server.Port = 8082
			s.Config.Admin.Path = "/aegion"

			err := s.registerWithCore(context.Background())
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}

			if tc.statusCode != 0 {
				if gotAuth != tc.wantAuth {
					t.Fatalf("expected auth header %q, got %q", tc.wantAuth, gotAuth)
				}
				if gotBody.ID != "admin" {
					t.Fatalf("expected module id admin, got %q", gotBody.ID)
				}
				if len(gotBody.Endpoints) != 1 {
					t.Fatalf("expected 1 endpoint, got %d", len(gotBody.Endpoints))
				}
				if gotBody.Endpoints[0].URL != tc.wantEndpoint {
					t.Fatalf("expected endpoint URL %q, got %q", tc.wantEndpoint, gotBody.Endpoints[0].URL)
				}
				if gotBody.HealthURL != tc.wantHealthURL {
					t.Fatalf("expected health URL %q, got %q", tc.wantHealthURL, gotBody.HealthURL)
				}
				if gotBody.Metadata["spa_path"] != "/aegion" {
					t.Fatalf("expected spa_path metadata /aegion, got %q", gotBody.Metadata["spa_path"])
				}
			}
		})
	}
}

func TestSPAFileServerBehavior(t *testing.T) {
	spa := NewSPAFileServer()

	t.Run("javascript assets are immutable", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/assets/index-DfxWsJt4.js", nil)
		rec := httptest.NewRecorder()
		spa.ServeHTTP(rec, req)

		if cache := rec.Header().Get("Cache-Control"); cache != "public, max-age=31536000, immutable" {
			t.Fatalf("expected immutable cache header, got %q", cache)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200 for existing JS asset, got %d", rec.Code)
		}
	})

	t.Run("missing static asset returns 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/assets/not-found.css", nil)
		rec := httptest.NewRecorder()
		spa.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for missing asset, got %d", rec.Code)
		}
	})

	t.Run("route fallback serves html no-cache", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/operators/123", nil)
		rec := httptest.NewRecorder()
		spa.ServeHTTP(rec, req)

		if rec.Code != http.StatusMovedPermanently {
			t.Fatalf("expected 301 for route fallback, got %d", rec.Code)
		}
		if cache := rec.Header().Get("Cache-Control"); cache != "no-cache, must-revalidate" {
			t.Fatalf("expected HTML cache header, got %q", cache)
		}
		if location := rec.Header().Get("Location"); location != "./" {
			t.Fatalf("expected redirect to ./, got %q", location)
		}
	})
}

func TestSPAFallbackRouting(t *testing.T) {
	s := &Server{
		Config: &Config{},
	}
	s.Config.Admin.Path = "/aegion"

	t.Run("api path remains 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/aegion/api/unknown", nil)
		rec := httptest.NewRecorder()
		s.spaFallback(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for api fallback, got %d", rec.Code)
		}
	})

	t.Run("admin ui path serves index", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/aegion/operators", nil)
		rec := httptest.NewRecorder()
		s.spaFallback(rec, req)
		if rec.Code != http.StatusMovedPermanently {
			t.Fatalf("expected 301 for admin route fallback, got %d", rec.Code)
		}
		if location := rec.Header().Get("Location"); location != "./" {
			t.Fatalf("expected redirect to ./, got %q", location)
		}
	})

	t.Run("non-admin path remains 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/outside", nil)
		rec := httptest.NewRecorder()
		s.spaFallback(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for non-admin path, got %d", rec.Code)
		}
	})
}
