package proxy

import (
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aegion/aegion/core/session"
)

func TestRuleEngine_Match(t *testing.T) {
	rules := []Rule{
		{
			ID:       "api-v1",
			Path:     "/api/v1/*",
			Methods:  []string{"GET", "POST"},
			Target:   "api-v1-service",
			Priority: 100,
			Enabled:  true,
		},
		{
			ID:       "api-v2",
			Path:     "/api/v2/*",
			Methods:  []string{"GET", "POST", "PUT", "DELETE"},
			Target:   "api-v2-service",
			Priority: 90,
			Enabled:  true,
		},
		{
			ID:       "static-files",
			Path:     "/static/*",
			Target:   "static-service",
			Priority: 50,
			Enabled:  true,
		},
		{
			ID:       "admin-panel",
			Path:     "/admin/*",
			Methods:  []string{"GET", "POST"},
			Target:   "admin-service",
			Priority: 200,
			Enabled:  false, // Disabled rule
		},
		{
			ID:       "health-check",
			Path:     "/health",
			Methods:  []string{"GET"},
			Target:   "health-service",
			Priority: 300,
			Enabled:  true,
		},
	}

	engine := NewRuleEngine(rules)

	tests := []struct {
		name           string
		method         string
		path           string
		expectedRuleID string
		shouldMatch    bool
	}{
		{
			name:           "exact health check match",
			method:         "GET",
			path:           "/health",
			expectedRuleID: "health-check",
			shouldMatch:    true,
		},
		{
			name:           "api v1 glob match",
			method:         "GET",
			path:           "/api/v1/users",
			expectedRuleID: "api-v1",
			shouldMatch:    true,
		},
		{
			name:           "api v1 POST match",
			method:         "POST",
			path:           "/api/v1/users",
			expectedRuleID: "api-v1",
			shouldMatch:    true,
		},
		{
			name:           "api v1 method not allowed",
			method:         "DELETE",
			path:           "/api/v1/users",
			shouldMatch:    false,
		},
		{
			name:           "api v2 DELETE allowed",
			method:         "DELETE",
			path:           "/api/v2/users",
			expectedRuleID: "api-v2",
			shouldMatch:    true,
		},
		{
			name:           "static files any method",
			method:         "GET",
			path:           "/static/css/app.css",
			expectedRuleID: "static-files",
			shouldMatch:    true,
		},
		{
			name:           "static files POST allowed (no method restriction)",
			method:         "POST",
			path:           "/static/upload",
			expectedRuleID: "static-files",
			shouldMatch:    true,
		},
		{
			name:        "admin panel disabled",
			method:      "GET",
			path:        "/admin/users",
			shouldMatch: false,
		},
		{
			name:        "no pattern match",
			method:      "GET",
			path:        "/unknown/path",
			shouldMatch: false,
		},
		{
			name:        "root path no match",
			method:      "GET",
			path:        "/",
			shouldMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			
			rule, matched := engine.Match(req)
			
			assert.Equal(t, tt.shouldMatch, matched, "match result")
			
			if tt.shouldMatch {
				require.NotNil(t, rule, "rule should not be nil")
				assert.Equal(t, tt.expectedRuleID, rule.ID, "rule ID")
			} else {
				assert.Nil(t, rule, "rule should be nil for no match")
			}
		})
	}
}

func TestRuleEngine_Priority(t *testing.T) {
	// Rules with overlapping patterns, different priorities
	rules := []Rule{
		{
			ID:       "catch-all",
			Path:     "*",
			Target:   "default-service",
			Priority: 1,
			Enabled:  true,
		},
		{
			ID:       "api-specific",
			Path:     "/api/*",
			Target:   "api-service",
			Priority: 100,
			Enabled:  true,
		},
		{
			ID:       "api-v1-specific",
			Path:     "/api/v1/*",
			Target:   "api-v1-service",
			Priority: 200,
			Enabled:  true,
		},
	}

	engine := NewRuleEngine(rules)

	// Should match the highest priority rule
	req := httptest.NewRequest("GET", "/api/v1/users", nil)
	rule, matched := engine.Match(req)
	
	assert.True(t, matched)
	assert.Equal(t, "api-v1-specific", rule.ID)

	// Should match api-specific for v2
	req = httptest.NewRequest("GET", "/api/v2/users", nil)
	rule, matched = engine.Match(req)
	
	assert.True(t, matched)
	assert.Equal(t, "api-specific", rule.ID)

	// Should match catch-all for other paths
	req = httptest.NewRequest("GET", "/other/path", nil)
	rule, matched = engine.Match(req)
	
	assert.True(t, matched)
	assert.Equal(t, "catch-all", rule.ID)
}

func TestRuleEngine_CheckAccess(t *testing.T) {
	engine := NewRuleEngine([]Rule{})

	tests := []struct {
		name        string
		rule        Rule
		session     *session.Session
		expectError bool
		errorType   error
	}{
		{
			name: "no auth required",
			rule: Rule{
				RequireAuth: false,
			},
			session:     nil,
			expectError: false,
		},
		{
			name: "auth required but no session",
			rule: Rule{
				RequireAuth: true,
			},
			session:     nil,
			expectError: true,
			errorType:   ErrAuthenticationRequired,
		},
		{
			name: "auth required with valid session",
			rule: Rule{
				RequireAuth: true,
			},
			session: &session.Session{
				ID:         uuid.New(),
				IdentityID: uuid.New(),
				AAL:        session.AAL1,
				Active:     true,
			},
			expectError: false,
		},
		{
			name: "AAL2 required but session has AAL1",
			rule: Rule{
				RequireAuth: true,
				RequiredAAL: session.AAL2,
			},
			session: &session.Session{
				ID:         uuid.New(),
				IdentityID: uuid.New(),
				AAL:        session.AAL1,
				Active:     true,
			},
			expectError: true,
			errorType:   ErrInsufficientPrivileges,
		},
		{
			name: "AAL2 required with valid AAL2 session",
			rule: Rule{
				RequireAuth: true,
				RequiredAAL: session.AAL2,
			},
			session: &session.Session{
				ID:         uuid.New(),
				IdentityID: uuid.New(),
				AAL:        session.AAL2,
				Active:     true,
			},
			expectError: false,
		},
		{
			name: "capabilities required (placeholder test)",
			rule: Rule{
				RequireAuth:  true,
				Capabilities: []string{"read:users", "write:users"},
			},
			session: &session.Session{
				ID:         uuid.New(),
				IdentityID: uuid.New(),
				AAL:        session.AAL1,
				Active:     true,
			},
			expectError: false, // checkCapabilities is a placeholder that returns nil
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			
			err := engine.CheckAccess(req, &tt.rule, tt.session)
			
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorType != nil {
					assert.Equal(t, tt.errorType, err)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRule_ApplyRewrite(t *testing.T) {
	tests := []struct {
		name     string
		rule     Rule
		path     string
		expected string
	}{
		{
			name: "no rewrite config",
			rule: Rule{
				Rewrite: nil,
			},
			path:     "/api/v1/users",
			expected: "/api/v1/users",
		},
		{
			name: "strip prefix only",
			rule: Rule{
				Rewrite: &RewriteConfig{
					StripPrefix: "/api/v1",
				},
			},
			path:     "/api/v1/users",
			expected: "/users",
		},
		{
			name: "add prefix only",
			rule: Rule{
				Rewrite: &RewriteConfig{
					AddPrefix: "/v2",
				},
			},
			path:     "/users",
			expected: "/v2/users",
		},
		{
			name: "strip and add prefix",
			rule: Rule{
				Rewrite: &RewriteConfig{
					StripPrefix: "/api/v1",
					AddPrefix:   "/v2",
				},
			},
			path:     "/api/v1/users",
			expected: "/v2/users",
		},
		{
			name: "strip prefix from root",
			rule: Rule{
				Rewrite: &RewriteConfig{
					StripPrefix: "/api",
				},
			},
			path:     "/api",
			expected: "/",
		},
		{
			name: "strip prefix not matching",
			rule: Rule{
				Rewrite: &RewriteConfig{
					StripPrefix: "/api/v2",
				},
			},
			path:     "/api/v1/users",
			expected: "/api/v1/users",
		},
		{
			name: "add prefix to root",
			rule: Rule{
				Rewrite: &RewriteConfig{
					AddPrefix: "/api",
				},
			},
			path:     "/",
			expected: "/api/",
		},
		{
			name: "complex rewrite",
			rule: Rule{
				Rewrite: &RewriteConfig{
					StripPrefix: "/public/api/v1",
					AddPrefix:   "/internal/v2",
				},
			},
			path:     "/public/api/v1/users/123",
			expected: "/internal/v2/users/123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.rule.ApplyRewrite(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRule_Validate(t *testing.T) {
	tests := []struct {
		name    string
		rule    Rule
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid rule",
			rule: Rule{
				ID:     "valid-rule",
				Path:   "/api/*",
				Target: "api-service",
			},
			wantErr: false,
		},
		{
			name: "empty ID",
			rule: Rule{
				Path:   "/api/*",
				Target: "api-service",
			},
			wantErr: true,
			errMsg:  "rule ID cannot be empty",
		},
		{
			name: "empty path",
			rule: Rule{
				ID:     "test-rule",
				Target: "api-service",
			},
			wantErr: true,
			errMsg:  "rule path cannot be empty",
		},
		{
			name: "empty target",
			rule: Rule{
				ID:   "test-rule",
				Path: "/api/*",
			},
			wantErr: true,
			errMsg:  "rule target cannot be empty",
		},
		{
			name: "invalid glob pattern",
			rule: Rule{
				ID:     "test-rule",
				Path:   "/api/[invalid",
				Target: "api-service",
			},
			wantErr: true,
			errMsg:  "invalid glob pattern",
		},
		{
			name: "valid AAL requirement",
			rule: Rule{
				ID:          "test-rule",
				Path:        "/api/*",
				Target:      "api-service",
				RequireAuth: true,
				RequiredAAL: session.AAL2,
			},
			wantErr: false,
		},
		{
			name: "invalid AAL requirement",
			rule: Rule{
				ID:          "test-rule",
				Path:        "/api/*",
				Target:      "api-service",
				RequireAuth: true,
				RequiredAAL: session.AAL("invalid"),
			},
			wantErr: true,
			errMsg:  "invalid required AAL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.rule.Validate()
			
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRuleEngine_CRUD(t *testing.T) {
	engine := NewRuleEngine([]Rule{})

	// Test AddRule
	rule1 := Rule{
		ID:       "rule1",
		Path:     "/api/*",
		Target:   "service1",
		Priority: 100,
		Enabled:  true,
	}
	engine.AddRule(rule1)

	rules := engine.GetRules()
	assert.Len(t, rules, 1)
	assert.Equal(t, "rule1", rules[0].ID)

	// Test GetRule
	retrieved, found := engine.GetRule("rule1")
	assert.True(t, found)
	assert.Equal(t, "rule1", retrieved.ID)

	// Test GetRule not found
	_, found = engine.GetRule("nonexistent")
	assert.False(t, found)

	// Test UpdateRule
	rule1.Path = "/api/v2/*"
	updated := engine.UpdateRule(rule1)
	assert.True(t, updated)

	retrieved, found = engine.GetRule("rule1")
	assert.True(t, found)
	assert.Equal(t, "/api/v2/*", retrieved.Path)

	// Test UpdateRule not found
	nonExistentRule := Rule{ID: "nonexistent", Path: "/test", Target: "test"}
	updated = engine.UpdateRule(nonExistentRule)
	assert.False(t, updated)

	// Test RemoveRule
	removed := engine.RemoveRule("rule1")
	assert.True(t, removed)

	rules = engine.GetRules()
	assert.Len(t, rules, 0)

	// Test RemoveRule not found
	removed = engine.RemoveRule("nonexistent")
	assert.False(t, removed)
}

func TestMatchesPattern(t *testing.T) {
	tests := []struct {
		path    string
		pattern string
		matches bool
	}{
		{"/api/users", "/api/users", true},
		{"/api/users", "/api/*", true},
		{"/api/v1/users", "/api/v1/*", true},
		{"/api/v1/users", "/api/v2/*", false},
		{"api/users", "api/*", true}, // No leading slash
		{"/api/users/123", "/api/users/*", true},
		{"/api/users", "/api/users/*", false}, // Exact match vs glob
		{"/", "/*", true},
		{"/health", "/health", true},
		{"/health/check", "/health", false},
		{"/static/css/app.css", "/static/*/*", true},
		{"/static/css", "/static/*/*", false},
		{"/api", "/api", true},
		{"/api/", "/api/*", true},
	}

	for _, tt := range tests {
		t.Run(tt.path+" vs "+tt.pattern, func(t *testing.T) {
			result := matchesPattern(tt.path, tt.pattern)
			assert.Equal(t, tt.matches, result)
		})
	}
}

func BenchmarkRuleEngine_Match(b *testing.B) {
	// Create rules with various patterns
	rules := make([]Rule, 100)
	for i := 0; i < 100; i++ {
		rules[i] = Rule{
			ID:       string(rune('A' + i%26)) + string(rune('0' + i/26)),
			Path:     "/api/v" + string(rune('0'+i%10)) + "/*",
			Target:   "service" + string(rune('0'+i%10)),
			Priority: 100 - i,
			Enabled:  true,
		}
	}
	
	engine := NewRuleEngine(rules)
	req := httptest.NewRequest("GET", "/api/v5/users", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		engine.Match(req)
	}
}