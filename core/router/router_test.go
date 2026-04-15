package router

import (
	"errors"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
)

func TestSingleJoiningSlash(t *testing.T) {
	tests := []struct {
		name     string
		a        string
		b        string
		expected string
	}{
		{"both with slashes", "http://example.com/", "/path", "http://example.com/path"},
		{"both without slashes", "http://example.com", "path", "http://example.com/path"},
		{"a with slash, b without", "http://example.com/", "path", "http://example.com/path"},
		{"a without slash, b with", "http://example.com", "/path", "http://example.com/path"},
		{"empty a", "", "/path", "/path"},
		{"empty b", "http://example.com/", "", "http://example.com/"},
		{"both empty", "", "", "/"},
		{"nested paths with slashes", "/api/v1/", "/users/", "/api/v1/users/"},
		{"nested paths without slashes", "/api/v1", "users", "/api/v1/users"},
		{"multiple slashes in b", "http://example.com/", "//path", "http://example.com//path"},
		{"root path", "/", "/", "/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := singleJoiningSlash(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("singleJoiningSlash(%q, %q) = %q, want %q", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

func TestIsConnectionRefused(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"connection refused", errors.New("connection refused"), true},
		{"no such host", errors.New("no such host"), true},
		{"dial tcp", errors.New("dial tcp: connection failed"), true},
		{"mixed case connection refused", errors.New("Connection Refused"), false}, // case sensitive
		{"wrapped connection refused", errors.New("failed to connect: connection refused"), true},
		{"timeout error", errors.New("timeout"), false},
		{"other error", errors.New("some other error"), false},
		{"empty error", errors.New(""), false},
		{"all keywords", errors.New("dial tcp: no such host or connection refused"), true},
		{"partial match", errors.New("refuse connection"), false}, // doesn't contain exact phrase
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isConnectionRefused(tt.err)
			if result != tt.expected {
				t.Errorf("isConnectionRefused(%v) = %t, want %t", tt.err, result, tt.expected)
			}
		})
	}
}

func TestGetClientIP(t *testing.T) {
	tests := []struct {
		name       string
		headers    map[string]string
		remoteAddr string
		expected   string
	}{
		{
			name:       "x-forwarded-for single ip",
			headers:    map[string]string{"X-Forwarded-For": "192.168.1.100"},
			remoteAddr: "10.0.0.1:8080",
			expected:   "192.168.1.100",
		},
		{
			name:       "x-forwarded-for multiple ips",
			headers:    map[string]string{"X-Forwarded-For": "203.0.113.1, 192.168.1.100, 10.0.0.1"},
			remoteAddr: "10.0.0.1:8080",
			expected:   "203.0.113.1", // should return first IP
		},
		{
			name:       "x-forwarded-for with spaces",
			headers:    map[string]string{"X-Forwarded-For": "  203.0.113.1  , 192.168.1.100"},
			remoteAddr: "10.0.0.1:8080",
			expected:   "203.0.113.1", // should trim spaces
		},
		{
			name:       "x-real-ip header",
			headers:    map[string]string{"X-Real-IP": "203.0.113.1"},
			remoteAddr: "10.0.0.1:8080",
			expected:   "203.0.113.1",
		},
		{
			name:       "x-forwarded-for takes precedence",
			headers:    map[string]string{"X-Forwarded-For": "203.0.113.1", "X-Real-IP": "192.168.1.100"},
			remoteAddr: "10.0.0.1:8080",
			expected:   "203.0.113.1",
		},
		{
			name:       "remote addr with port",
			headers:    map[string]string{},
			remoteAddr: "192.168.1.100:8080",
			expected:   "192.168.1.100",
		},
		{
			name:       "remote addr without port",
			headers:    map[string]string{},
			remoteAddr: "192.168.1.100",
			expected:   "192.168.1.100",
		},
		{
			name:       "ipv6 remote addr",
			headers:    map[string]string{},
			remoteAddr: "[::1]:8080",
			expected:   "::1",
		},
		{
			name:       "empty x-forwarded-for falls back to x-real-ip",
			headers:    map[string]string{"X-Forwarded-For": "", "X-Real-IP": "203.0.113.1"},
			remoteAddr: "10.0.0.1:8080",
			expected:   "203.0.113.1",
		},
		{
			name:       "empty headers fall back to remote addr",
			headers:    map[string]string{"X-Forwarded-For": "", "X-Real-IP": ""},
			remoteAddr: "192.168.1.100:8080",
			expected:   "192.168.1.100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest("GET", "http://example.com", nil)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}

			// Set headers
			for key, value := range tt.headers {
				req.Header.Set(key, value)
			}
			if req.Header.Get("X-Forwarded-For") != "" || req.Header.Get("X-Real-IP") != "" {
				t.Setenv("AEGION_TRUSTED_PROXY_CIDRS", "10.0.0.0/8")
			}

			// Set remote address
			req.RemoteAddr = tt.remoteAddr

			result := getClientIP(req)
			if result != tt.expected {
				t.Errorf("getClientIP() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestTimeToSeconds(t *testing.T) {
	tests := []struct {
		name     string
		seconds  int
		expected string
	}{
		{"zero", 0, "0"},
		{"positive", 60, "60"},
		{"negative", -30, "-30"},
		{"large number", 3600, "3600"},
		{"small negative", -1, "-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := timeToSeconds(tt.seconds)
			if result != tt.expected {
				t.Errorf("timeToSeconds(%d) = %q, want %q", tt.seconds, result, tt.expected)
			}
		})
	}
}

// TestRouterWrapperMethods tests all the chi.Router wrapper methods
func TestRouterWrapperMethods(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RateLimit.Enabled = false // Disable rate limiting for tests
	logger := zerolog.Nop()
	r := New(cfg, logger, nil)

	// Test Get
	t.Run("Get", func(t *testing.T) {
		r.Get("/test-get", func(w http.ResponseWriter, req *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	})

	// Test Post
	t.Run("Post", func(t *testing.T) {
		r.Post("/test-post", func(w http.ResponseWriter, req *http.Request) {
			w.WriteHeader(http.StatusCreated)
		})
	})

	// Test Put
	t.Run("Put", func(t *testing.T) {
		r.Put("/test-put", func(w http.ResponseWriter, req *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	})

	// Test Patch
	t.Run("Patch", func(t *testing.T) {
		r.Patch("/test-patch", func(w http.ResponseWriter, req *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	})

	// Test Delete
	t.Run("Delete", func(t *testing.T) {
		r.Delete("/test-delete", func(w http.ResponseWriter, req *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
	})

	// Test Handle
	t.Run("Handle", func(t *testing.T) {
		r.Handle("/test-handle", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
	})

	// Test HandleFunc
	t.Run("HandleFunc", func(t *testing.T) {
		r.HandleFunc("/test-handlefunc", func(w http.ResponseWriter, req *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	})

	// Test Mount
	t.Run("Mount", func(t *testing.T) {
		subHandler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		r.Mount("/mounted", subHandler)
	})

	// Test Route
	t.Run("Route", func(t *testing.T) {
		r.Route("/route-group", func(chi chi.Router) {
			chi.Get("/nested", func(w http.ResponseWriter, req *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
		})
	})

	// Test Group
	t.Run("Group", func(t *testing.T) {
		r.Group(func(chi chi.Router) {
			chi.Get("/grouped", func(w http.ResponseWriter, req *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
		})
	})

	// Test With
	t.Run("With", func(t *testing.T) {
		middleware := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				next.ServeHTTP(w, req)
			})
		}
		subRouter := r.With(middleware)
		if subRouter == nil {
			t.Error("With() returned nil")
		}
	})

	// Test NotFound
	t.Run("NotFound", func(t *testing.T) {
		r.NotFound(func(w http.ResponseWriter, req *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		})
	})

	// Test MethodNotAllowed
	t.Run("MethodNotAllowed", func(t *testing.T) {
		r.MethodNotAllowed(func(w http.ResponseWriter, req *http.Request) {
			w.WriteHeader(http.StatusMethodNotAllowed)
		})
	})

	// Test Chi
	t.Run("Chi", func(t *testing.T) {
		mux := r.Chi()
		if mux == nil {
			t.Error("Chi() returned nil")
		}
	})
}

// TestRouterModuleProxy tests ProxyToModule and MountModule
func TestRouterModuleProxy(t *testing.T) {
	cfg := DefaultConfig()
	cfg.InternalToken = "test-token"
	cfg.SessionSecret = []byte("test-secret")
	cfg.RateLimit.Enabled = false
	logger := zerolog.Nop()

	// Use nil registry (acceptable for this test)
	r := New(cfg, logger, nil)

	t.Run("ProxyToModule", func(t *testing.T) {
		handler := r.ProxyToModule("test-module")
		if handler == nil {
			t.Error("ProxyToModule() returned nil")
		}
	})

	t.Run("MountModule", func(t *testing.T) {
		// This should not panic
		r.MountModule("/module", "another-module")
	})
}
