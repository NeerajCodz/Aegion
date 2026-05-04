package rest

import (
	"testing"
)

func TestValidateEmail(t *testing.T) {
	validator := NewValidator()

	tests := []struct {
		email string
		valid bool
		desc  string
	}{
		{"user@example.com", true, "valid email"},
		{"user.name@example.co.uk", true, "valid email with dots"},
		{"user+tag@example.com", true, "valid email with plus"},
		{"", false, "empty email"},
		{"user@", false, "invalid email format"},
		{"@example.com", false, "invalid email format"},
		{"user@example", false, "invalid email format"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			err := validator.ValidateEmail(tt.email)
			if (err == nil) != tt.valid {
				t.Errorf("ValidateEmail(%q) error = %v, wantErr = %v", tt.email, err, !tt.valid)
			}
		})
	}
}

func TestValidateURL(t *testing.T) {
	validator := NewValidator()

	tests := []struct {
		url          string
		requireHTTPS bool
		valid        bool
		desc         string
	}{
		{"https://example.com", true, true, "valid HTTPS URL"},
		{"http://example.com", false, true, "valid HTTP URL"},
		{"http://example.com", true, false, "HTTP when HTTPS required"},
		{"javascript:alert(1)", false, false, "dangerous javascript protocol"},
		{"data:text/html,<script>alert(1)</script>", false, false, "dangerous data protocol"},
		{"", false, false, "empty URL"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			err := validator.ValidateURL(tt.url, tt.requireHTTPS)
			if (err == nil) != tt.valid {
				t.Errorf("ValidateURL error = %v, wantErr = %v", err, !tt.valid)
			}
		})
	}
}

func TestValidateInputLength(t *testing.T) {
	validator := NewValidator()

	tests := []struct {
		input     string
		maxLength int
		valid     bool
		desc      string
	}{
		{"hello", 10, true, "input within limit"},
		{"hello", 5, true, "input at limit"},
		{"hello", 4, false, "input exceeds limit"},
		{"", 10, true, "empty input"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			err := validator.ValidateInputLength(tt.input, tt.maxLength)
			if (err == nil) != tt.valid {
				t.Errorf("ValidateInputLength error = %v, wantErr = %v", err, !tt.valid)
			}
		})
	}
}

func TestValidateQueryComplexity(t *testing.T) {
	validator := NewValidator()

	tests := []struct {
		req       QueryRequest
		maxFields int
		valid     bool
		desc      string
	}{
		{
			QueryRequest{Fields: []string{"a", "b", "c"}},
			10,
			true,
			"simple query",
		},
		{
			QueryRequest{Fields: make([]string, 101)},
			100,
			false,
			"too many fields",
		},
		{
			QueryRequest{Filters: func() map[string]interface{} {
				m := make(map[string]interface{})
				for i := 0; i < 101; i++ {
					m[string(rune(i))] = "value"
				}
				return m
			}()},
			100,
			false,
			"too many filters",
		},
		{
			QueryRequest{Aggregate: make([]AggregateField, 51)},
			100,
			false,
			"too many aggregations",
		},
		{
			QueryRequest{Sort: make([]SortField, 11)},
			100,
			false,
			"too many sort fields",
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			err := validator.ValidateQueryComplexity(tt.req, tt.maxFields)
			if (err == nil) != tt.valid {
				t.Errorf("ValidateQueryComplexity error = %v, wantErr = %v", err, !tt.valid)
			}
		})
	}
}

func TestValidateSQLWithDangerousPatterns(t *testing.T) {
	validator := NewValidator()

	tests := []struct {
		sql   string
		valid bool
		desc  string
	}{
		{"SELECT * FROM analytics_events", true, "valid SELECT"},
		{"SELECT COUNT(*) FROM analytics_events", true, "valid SELECT with aggregate"},
		{"SELECT * FROM users", false, "select from non-events table"},
		{"DROP TABLE users", false, "dangerous DROP"},
		{"DELETE FROM users", false, "dangerous DELETE"},
		{"INSERT INTO users VALUES (1)", false, "dangerous INSERT"},
		{"UPDATE users SET x=1", false, "dangerous UPDATE"},
		{"ALTER TABLE users", false, "dangerous ALTER"},
		{"TRUNCATE TABLE users", false, "dangerous TRUNCATE"},
		{"SELECT * FROM analytics_events; DROP TABLE users", false, "multiple statements"},
		{"SELECT * FROM analytics_events -- comment", false, "comment injection"},
		{"SELECT * FROM analytics_events /* comment */", false, "block comment injection"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			err := validator.ValidateSQL(tt.sql)
			if (err == nil) != tt.valid {
				t.Errorf("ValidateSQL error = %v, wantErr = %v", err, !tt.valid)
			}
		})
	}
}

func TestSanitizeInputSecurity(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		desc     string
	}{
		{"hello world", "hello world", "normal input"},
		{"hello\x00world", "helloworld", "null byte removed"},
		{"hello\nworld", "hello world", "newline replaced"},
		{"hello\rworld", "hello world", "carriage return replaced"},
		{"hello\tworld", "hello world", "tab replaced"},
		{"hello\x01world", "hello world", "control char replaced"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			result := SanitizeInput(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeInput(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestSanitizeJSONSecurity(t *testing.T) {
	tests := []struct {
		input string
		valid bool
		desc  string
	}{
		{`{"key":"value"}`, true, "valid JSON"},
		{`invalid json`, false, "invalid JSON"},
		{`{"nested":{"key":"value"}}`, true, "nested JSON"},
		{`{"array":[1,2,3]}`, true, "JSON with array"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			_, err := SanitizeJSON([]byte(tt.input))
			if (err == nil) != tt.valid {
				t.Errorf("SanitizeJSON error = %v, wantErr = %v", err, !tt.valid)
			}
		})
	}
}

// Helper for string repetition
func repeat(s string, n int) string {
	result := ""
	for i := 0; i < n; i++ {
		result += s
	}
	return result
}
