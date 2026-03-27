package proxy

import (
	"errors"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/aegion/aegion/core/session"
)

var (
	ErrAccessDenied          = errors.New("access denied")
	ErrAuthenticationRequired = errors.New("authentication required")
	ErrInsufficientPrivileges = errors.New("insufficient privileges")
)

// Rule represents an access control rule for routing and authorization.
type Rule struct {
	// ID uniquely identifies the rule
	ID string `json:"id" yaml:"id"`

	// Path is the glob pattern to match against request paths
	Path string `json:"path" yaml:"path"`

	// Methods specifies allowed HTTP methods (empty means all methods)
	Methods []string `json:"methods,omitempty" yaml:"methods,omitempty"`

	// RequireAuth indicates if authentication is required
	RequireAuth bool `json:"require_auth" yaml:"require_auth"`

	// RequiredAAL specifies the minimum Authentication Assurance Level required
	RequiredAAL session.AAL `json:"required_aal,omitempty" yaml:"required_aal,omitempty"`

	// Capabilities lists required user capabilities/permissions
	Capabilities []string `json:"capabilities,omitempty" yaml:"capabilities,omitempty"`

	// RateLimit configuration for this rule (overrides global settings)
	RateLimit *RateLimitConfig `json:"rate_limit,omitempty" yaml:"rate_limit,omitempty"`

	// Target specifies the upstream name to route to
	Target string `json:"target" yaml:"target"`

	// Priority determines rule evaluation order (higher priority = evaluated first)
	Priority int `json:"priority" yaml:"priority"`

	// Headers to add when this rule matches
	Headers map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`

	// Rewrite configuration for path rewriting
	Rewrite *RewriteConfig `json:"rewrite,omitempty" yaml:"rewrite,omitempty"`

	// Enabled controls if this rule is active
	Enabled bool `json:"enabled" yaml:"enabled"`

	// Description for documentation purposes
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// RewriteConfig defines path rewriting rules.
type RewriteConfig struct {
	// StripPrefix removes this prefix from the path before forwarding
	StripPrefix string `json:"strip_prefix,omitempty" yaml:"strip_prefix,omitempty"`

	// AddPrefix adds this prefix to the path before forwarding
	AddPrefix string `json:"add_prefix,omitempty" yaml:"add_prefix,omitempty"`

	// Regex pattern for complex rewriting (future extension)
	Regex string `json:"regex,omitempty" yaml:"regex,omitempty"`
	
	// Replacement for regex pattern (future extension)
	Replacement string `json:"replacement,omitempty" yaml:"replacement,omitempty"`
}

// RuleEngine manages access control rules and routing decisions.
type RuleEngine struct {
	rules []Rule
}

// NewRuleEngine creates a new rule engine with the given rules.
func NewRuleEngine(rules []Rule) *RuleEngine {
	// Sort rules by priority (highest first)
	sortedRules := make([]Rule, len(rules))
	copy(sortedRules, rules)
	
	for i := 0; i < len(sortedRules)-1; i++ {
		for j := i + 1; j < len(sortedRules); j++ {
			if sortedRules[i].Priority < sortedRules[j].Priority {
				sortedRules[i], sortedRules[j] = sortedRules[j], sortedRules[i]
			}
		}
	}

	return &RuleEngine{
		rules: sortedRules,
	}
}

// Match finds the first rule that matches the request.
// Returns the matching rule and true, or nil and false if no rule matches.
func (e *RuleEngine) Match(r *http.Request) (*Rule, bool) {
	path := r.URL.Path
	method := r.Method

	for i := range e.rules {
		rule := &e.rules[i]
		
		// Skip disabled rules
		if !rule.Enabled {
			continue
		}

		// Check path pattern
		if !matchesPattern(path, rule.Path) {
			continue
		}

		// Check HTTP method
		if !rule.matchesMethod(method) {
			continue
		}

		return rule, true
	}

	return nil, false
}

// CheckAccess validates if the request should be allowed based on the matched rule.
func (e *RuleEngine) CheckAccess(r *http.Request, rule *Rule, sess *session.Session) error {
	// If no authentication required, allow request
	if !rule.RequireAuth {
		return nil
	}

	// Authentication required but no session
	if sess == nil {
		return ErrAuthenticationRequired
	}

	// Check AAL requirement
	if rule.RequiredAAL != "" && sess.AAL < rule.RequiredAAL {
		return ErrInsufficientPrivileges
	}

	// Check required capabilities
	if len(rule.Capabilities) > 0 {
		if err := checkCapabilities(sess, rule.Capabilities); err != nil {
			return err
		}
	}

	return nil
}

// AddRule adds a new rule to the engine.
func (e *RuleEngine) AddRule(rule Rule) {
	e.rules = append(e.rules, rule)
	
	// Re-sort rules by priority
	for i := len(e.rules) - 1; i > 0; i-- {
		if e.rules[i].Priority > e.rules[i-1].Priority {
			e.rules[i], e.rules[i-1] = e.rules[i-1], e.rules[i]
		} else {
			break
		}
	}
}

// RemoveRule removes a rule by ID.
func (e *RuleEngine) RemoveRule(id string) bool {
	for i, rule := range e.rules {
		if rule.ID == id {
			e.rules = append(e.rules[:i], e.rules[i+1:]...)
			return true
		}
	}
	return false
}

// GetRules returns all rules in the engine.
func (e *RuleEngine) GetRules() []Rule {
	rules := make([]Rule, len(e.rules))
	copy(rules, e.rules)
	return rules
}

// GetRule retrieves a rule by ID.
func (e *RuleEngine) GetRule(id string) (*Rule, bool) {
	for i := range e.rules {
		if e.rules[i].ID == id {
			return &e.rules[i], true
		}
	}
	return nil, false
}

// UpdateRule updates an existing rule.
func (e *RuleEngine) UpdateRule(rule Rule) bool {
	for i := range e.rules {
		if e.rules[i].ID == rule.ID {
			e.rules[i] = rule
			
			// Re-sort if priority changed
			e.sortRules()
			return true
		}
	}
	return false
}

// matchesMethod checks if the rule allows the given HTTP method.
func (r *Rule) matchesMethod(method string) bool {
	// Empty methods slice means all methods are allowed
	if len(r.Methods) == 0 {
		return true
	}

	method = strings.ToUpper(method)
	for _, allowed := range r.Methods {
		if strings.ToUpper(allowed) == method {
			return true
		}
	}
	return false
}

// sortRules sorts rules by priority (highest first).
func (e *RuleEngine) sortRules() {
	for i := 0; i < len(e.rules)-1; i++ {
		for j := i + 1; j < len(e.rules); j++ {
			if e.rules[i].Priority < e.rules[j].Priority {
				e.rules[i], e.rules[j] = e.rules[j], e.rules[i]
			}
		}
	}
}

// matchesPattern checks if a path matches a glob pattern.
func matchesPattern(path, pattern string) bool {
	// Normalize paths
	path = strings.TrimPrefix(path, "/")
	pattern = strings.TrimPrefix(pattern, "/")

	// Use filepath.Match for glob pattern matching
	matched, err := filepath.Match(pattern, path)
	if err != nil {
		// If pattern is invalid, try exact match
		return path == pattern
	}

	return matched
}

// checkCapabilities validates if the session has required capabilities.
func checkCapabilities(sess *session.Session, requiredCaps []string) error {
	// For now, we'll implement a simple capability check
	// In a real implementation, you'd query the user's permissions from the database
	
	// This is a placeholder - you would implement actual capability checking
	// based on your authorization system (roles, permissions, etc.)
	
	// Example implementation:
	// 1. Query user roles/permissions from database using sess.IdentityID
	// 2. Check if user has all required capabilities
	// 3. Return appropriate error if not
	
	// For demonstration, we'll return nil (allow all)
	// TODO: Implement actual capability checking
	_ = requiredCaps // Silence unused variable warning
	
	return nil
}

// ApplyRewrite applies path rewriting based on the rule's rewrite configuration.
func (r *Rule) ApplyRewrite(path string) string {
	if r.Rewrite == nil {
		return path
	}

	// Strip prefix
	if r.Rewrite.StripPrefix != "" && strings.HasPrefix(path, r.Rewrite.StripPrefix) {
		path = strings.TrimPrefix(path, r.Rewrite.StripPrefix)
		
		// Ensure path starts with /
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
	}

	// Add prefix
	if r.Rewrite.AddPrefix != "" {
		prefix := r.Rewrite.AddPrefix
		
		// Ensure prefix starts with /
		if !strings.HasPrefix(prefix, "/") {
			prefix = "/" + prefix
		}
		
		// Ensure prefix doesn't end with / unless path is empty
		if strings.HasSuffix(prefix, "/") && path != "/" && path != "" {
			prefix = strings.TrimSuffix(prefix, "/")
		}
		
		path = prefix + path
	}

	return path
}

// Validate checks if the rule configuration is valid.
func (r *Rule) Validate() error {
	if r.ID == "" {
		return errors.New("rule ID cannot be empty")
	}

	if r.Path == "" {
		return errors.New("rule path cannot be empty")
	}

	if r.Target == "" {
		return errors.New("rule target cannot be empty")
	}

	// Validate glob pattern
	if _, err := filepath.Match(r.Path, "test"); err != nil {
		return errors.New("invalid glob pattern in path: " + err.Error())
	}

	// Validate AAL
	if r.RequiredAAL != "" {
		switch r.RequiredAAL {
		case session.AAL0, session.AAL1, session.AAL2:
			// Valid AAL
		default:
			return errors.New("invalid required AAL: " + string(r.RequiredAAL))
		}
	}

	return nil
}