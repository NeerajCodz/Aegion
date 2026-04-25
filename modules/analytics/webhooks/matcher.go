package webhooks

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Matcher matches events against webhook filters.
type Matcher struct {
	config MatcherConfig
}

// NewMatcher creates a new event matcher.
func NewMatcher(config MatcherConfig) *Matcher {
	if config.MaxCustomFilterDepth == 0 {
		config.MaxCustomFilterDepth = 10
	}
	return &Matcher{config: config}
}

// Matches determines if an event matches a webhook's filters.
func (m *Matcher) Matches(filter EventFilter, eventType, category string, data map[string]interface{}) bool {
	// Check event type patterns
	if !m.matchesEventType(filter.EventTypes, eventType) {
		return false
	}

	// Check category
	if len(filter.Categories) > 0 && !m.matchesCategory(filter.Categories, category) {
		return false
	}

	// Check custom filter
	if filter.CustomFilter != nil && len(filter.CustomFilter) > 0 {
		if !m.matchesCustomFilter(filter.CustomFilter, data) {
			return false
		}
	}

	return true
}

// matchesEventType checks if eventType matches any pattern in patterns.
func (m *Matcher) matchesEventType(patterns []string, eventType string) bool {
	if len(patterns) == 0 {
		return true // No patterns means match all
	}

	for _, pattern := range patterns {
		if m.matchesGlobPattern(pattern, eventType) {
			return true
		}
	}

	return false
}

// matchesGlobPattern matches a glob pattern against a string.
// Supports *.category and type.* patterns.
func (m *Matcher) matchesGlobPattern(pattern, str string) bool {
	// Exact match
	if pattern == str {
		return true
	}

	// Use filepath.Match for glob-style matching
	matched, err := filepath.Match(pattern, str)
	if err != nil {
		return false
	}

	return matched
}

// matchesCategory checks if category matches any in the list.
func (m *Matcher) matchesCategory(categories []string, category string) bool {
	for _, cat := range categories {
		if cat == category || cat == "*" {
			return true
		}
	}
	return false
}

// matchesCustomFilter recursively checks custom filter conditions.
func (m *Matcher) matchesCustomFilter(filter map[string]interface{}, data map[string]interface{}) bool {
	return m.evaluateFilter(filter, data, 0)
}

// evaluateFilter recursively evaluates a filter expression.
func (m *Matcher) evaluateFilter(filter map[string]interface{}, data map[string]interface{}, depth int) bool {
	if depth > m.config.MaxCustomFilterDepth {
		return false
	}

	for key, value := range filter {
		switch key {
		case "$and":
			// All conditions must match
			if conditions, ok := value.([]interface{}); ok {
				for _, cond := range conditions {
					if condMap, ok := cond.(map[string]interface{}); ok {
						if !m.evaluateFilter(condMap, data, depth+1) {
							return false
						}
					}
				}
				return true
			}

		case "$or":
			// Any condition must match
			if conditions, ok := value.([]interface{}); ok {
				for _, cond := range conditions {
					if condMap, ok := cond.(map[string]interface{}); ok {
						if m.evaluateFilter(condMap, data, depth+1) {
							return true
						}
					}
				}
				return false
			}

		case "$not":
			// Negate the condition
			if condMap, ok := value.(map[string]interface{}); ok {
				return !m.evaluateFilter(condMap, data, depth+1)
			}

		default:
			// Field matching
			if !m.matchesField(key, value, data) {
				return false
			}
		}
	}

	return true
}

// matchesField checks if a field in data matches the expected value.
func (m *Matcher) matchesField(key string, expected interface{}, data map[string]interface{}) bool {
	actual, exists := data[key]
	if !exists {
		return false
	}

	// Handle operators in expected value
	if opMap, ok := expected.(map[string]interface{}); ok {
		return m.matchesOperators(actual, opMap)
	}

	// Direct equality
	return m.deepEqual(actual, expected)
}

// matchesOperators evaluates operator conditions like $eq, $in, $contains, etc.
func (m *Matcher) matchesOperators(value interface{}, ops map[string]interface{}) bool {
	for op, operand := range ops {
		switch op {
		case "$eq":
			if !m.deepEqual(value, operand) {
				return false
			}

		case "$ne":
			if m.deepEqual(value, operand) {
				return false
			}

		case "$in":
			if list, ok := operand.([]interface{}); ok {
				found := false
				for _, item := range list {
					if m.deepEqual(value, item) {
						found = true
						break
					}
				}
				if !found {
					return false
				}
			}

		case "$contains":
			if str, ok := value.(string); ok {
				if operandStr, ok := operand.(string); ok {
					if !strings.Contains(str, operandStr) {
						return false
					}
				}
			}

		case "$exists":
			if exists, ok := operand.(bool); ok {
				actualExists := value != nil
				if exists != actualExists {
					return false
				}
			}

		default:
			return false
		}
	}

	return true
}

// deepEqual compares two values for equality (type-aware).
func (m *Matcher) deepEqual(a, b interface{}) bool {
	if a == nil && b == nil {
		return true
	}

	if a == nil || b == nil {
		return false
	}

	switch aVal := a.(type) {
	case string:
		if bVal, ok := b.(string); ok {
			return aVal == bVal
		}
	case float64:
		if bVal, ok := b.(float64); ok {
			return aVal == bVal
		}
		if bVal, ok := b.(int); ok {
			return aVal == float64(bVal)
		}
	case int:
		if bVal, ok := b.(int); ok {
			return aVal == bVal
		}
		if bVal, ok := b.(float64); ok {
			return float64(aVal) == bVal
		}
	case bool:
		if bVal, ok := b.(bool); ok {
			return aVal == bVal
		}
	}

	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}
