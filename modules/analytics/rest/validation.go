package rest

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Validator provides request validation
type Validator struct{}

// NewValidator creates a new validator
func NewValidator() *Validator {
	return &Validator{}
}

// ValidateQueryRequest validates a query request
func (v *Validator) ValidateQueryRequest(req QueryRequest) error {
	if req.PageSize < 0 {
		return fmt.Errorf("page_size must be non-negative")
	}

	if req.Page < 0 {
		return fmt.Errorf("page must be non-negative")
	}

	if req.PageSize > 100000 {
		return fmt.Errorf("page_size exceeds maximum of 100000")
	}

	// Validate filters
	if err := v.validateFilters(req.Filters); err != nil {
		return err
	}

	// Validate sort fields
	for _, sort := range req.Sort {
		if err := v.validateSortField(sort); err != nil {
			return err
		}
	}

	// Validate aggregate functions
	for _, agg := range req.Aggregate {
		if err := v.validateAggregateField(agg); err != nil {
			return err
		}
	}

	return nil
}

// ValidateDashboardRequest validates a dashboard request
func (v *Validator) ValidateDashboardRequest(req DashboardRequest) error {
	if req.Name == "" {
		return fmt.Errorf("name is required")
	}

	if len(req.Name) > 255 {
		return fmt.Errorf("name must not exceed 255 characters")
	}

	if len(req.Description) > 1000 {
		return fmt.Errorf("description must not exceed 1000 characters")
	}

	if req.Config == nil {
		return fmt.Errorf("config is required")
	}

	return nil
}

// ValidateQuerySaveRequest validates a query save request
func (v *Validator) ValidateQuerySaveRequest(req QuerySaveRequest) error {
	if req.Name == "" {
		return fmt.Errorf("name is required")
	}

	if len(req.Name) > 255 {
		return fmt.Errorf("name must not exceed 255 characters")
	}

	if req.SQL == "" {
		return fmt.Errorf("sql is required")
	}

	if len(req.SQL) > 100000 {
		return fmt.Errorf("sql must not exceed 100000 characters")
	}

	// Basic SQL validation (prevent dangerous operations)
	if err := v.validateSQL(req.SQL); err != nil {
		return err
	}

	return nil
}

// ValidateSearchRequest validates a search request
func (v *Validator) ValidateSearchRequest(req SearchRequest) error {
	if req.Query == "" {
		return fmt.Errorf("query is required")
	}

	if len(req.Query) > 1000 {
		return fmt.Errorf("query must not exceed 1000 characters")
	}

	if req.PageSize > 10000 {
		return fmt.Errorf("page_size exceeds maximum of 10000")
	}

	return nil
}

// ValidateReportRequest validates a report request
func (v *Validator) ValidateReportRequest(req ReportRequest) error {
	if req.Title == "" {
		return fmt.Errorf("title is required")
	}

	if len(req.Title) > 255 {
		return fmt.Errorf("title must not exceed 255 characters")
	}

	if len(req.Queries) == 0 {
		return fmt.Errorf("at least one query is required")
	}

	if req.Format == "" {
		return fmt.Errorf("format is required")
	}

	validFormats := map[string]bool{"pdf": true, "html": true, "json": true}
	if !validFormats[req.Format] {
		return fmt.Errorf("invalid format: must be pdf, html, or json")
	}

	return nil
}

// ValidateExportRequest validates an export request
func (v *Validator) ValidateExportRequest(req ExportRequest) error {
	if req.Format == "" {
		return fmt.Errorf("format is required")
	}

	validFormats := map[string]bool{"csv": true, "json": true, "parquet": true}
	if !validFormats[req.Format] {
		return fmt.Errorf("invalid format: must be csv, json, or parquet")
	}

	return nil
}

// Helper validation functions

func (v *Validator) validateFilters(filters map[string]interface{}) error {
	if filters == nil {
		return nil
	}

	for field, value := range filters {
		if field == "" {
			return fmt.Errorf("filter field name cannot be empty")
		}

		if len(field) > 255 {
			return fmt.Errorf("filter field name exceeds maximum of 255 characters")
		}

		// Validate field name (only alphanumeric, underscore, dot)
		if !regexp.MustCompile(`^[a-zA-Z0-9_\.]+$`).MatchString(field) {
			return fmt.Errorf("invalid filter field name: %s", field)
		}

		// If value is a map, validate operators
		if operatorMap, ok := value.(map[string]interface{}); ok {
			for op := range operatorMap {
				if err := v.validateOperator(op); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func (v *Validator) validateOperator(op string) error {
	validOps := map[string]bool{
		"$eq":         true,
		"$ne":         true,
		"$gt":         true,
		"$gte":        true,
		"$lt":         true,
		"$lte":        true,
		"$in":         true,
		"$nin":        true,
		"$contains":   true,
		"$regex":      true,
		"$startsWith": true,
		"$endsWith":   true,
	}

	if !validOps[op] {
		return fmt.Errorf("invalid operator: %s", op)
	}

	return nil
}

func (v *Validator) validateSortField(sort SortField) error {
	if sort.Field == "" {
		return fmt.Errorf("sort field cannot be empty")
	}

	if len(sort.Field) > 255 {
		return fmt.Errorf("sort field exceeds maximum of 255 characters")
	}

	// Validate field name
	if !regexp.MustCompile(`^[a-zA-Z0-9_\.]+$`).MatchString(sort.Field) {
		return fmt.Errorf("invalid sort field name: %s", sort.Field)
	}

	// Validate direction
	direction := strings.ToLower(sort.Direction)
	if direction != "asc" && direction != "desc" {
		return fmt.Errorf("invalid sort direction: must be asc or desc")
	}

	return nil
}

func (v *Validator) validateAggregateField(agg AggregateField) error {
	if agg.Function == "" {
		return fmt.Errorf("aggregate function is required")
	}

	validFunctions := map[string]bool{
		"count":      true,
		"sum":        true,
		"avg":        true,
		"min":        true,
		"max":        true,
		"percentile": true,
	}

	if !validFunctions[agg.Function] {
		return fmt.Errorf("invalid aggregate function: %s", agg.Function)
	}

	if agg.Field == "" && agg.Function != "count" {
		return fmt.Errorf("field is required for %s aggregation", agg.Function)
	}

	return nil
}

func (v *Validator) validateSQL(sql string) error {
	// Check for dangerous operations
	dangerousPatterns := []string{
		"DROP",
		"DELETE",
		"TRUNCATE",
		"ALTER",
		"CREATE",
		"INSERT",
		"UPDATE",
	}

	upperSQL := strings.ToUpper(sql)
	for _, pattern := range dangerousPatterns {
		if strings.Contains(upperSQL, pattern) {
			return fmt.Errorf("SQL operation not allowed: %s", pattern)
		}
	}

	// Validate SQL syntax (basic check)
	if !strings.Contains(strings.ToUpper(sql), "SELECT") {
		return fmt.Errorf("only SELECT queries are allowed")
	}

	return nil
}

// SanitizeInput removes potentially dangerous characters
func SanitizeInput(input string) string {
	// Remove null bytes
	input = strings.ReplaceAll(input, "\x00", "")

	// Remove line breaks and control characters
	input = regexp.MustCompile(`[\r\n\t\x00-\x1f]`).ReplaceAllString(input, " ")

	return input
}

// SanitizeJSON validates and sanitizes JSON input
func SanitizeJSON(data []byte) (map[string]interface{}, error) {
	var result map[string]interface{}

	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	// Recursively sanitize string values
	sanitizeMapValues(result)

	return result, nil
}

func sanitizeMapValues(m map[string]interface{}) {
	for key, value := range m {
		switch v := value.(type) {
		case string:
			m[key] = SanitizeInput(v)
		case map[string]interface{}:
			sanitizeMapValues(v)
		case []interface{}:
			for i, item := range v {
				if strVal, ok := item.(string); ok {
					v[i] = SanitizeInput(strVal)
				}
			}
		}
	}
}

// ValidateContext ensures context is valid for request
func ValidateContext(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
