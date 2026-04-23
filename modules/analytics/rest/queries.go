package rest

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// QueryBuilder builds SQL queries from filter requests
type DefaultQueryBuilder struct {
	db Database
}

// Database interface for query execution
type Database interface {
	Query(ctx context.Context, sql string) ([]map[string]interface{}, error)
	Count(ctx context.Context, sql string) (int, error)
}

// NewQueryBuilder creates a new query builder
func NewQueryBuilder(db Database) *DefaultQueryBuilder {
	return &DefaultQueryBuilder{db: db}
}

// BuildQuery builds a SQL query from a QueryRequest
func (qb *DefaultQueryBuilder) BuildQuery(ctx context.Context, req QueryRequest, table string) (string, error) {
	query := fmt.Sprintf("SELECT * FROM %s", table)

	// Add WHERE clause from filters
	if len(req.Filters) > 0 {
		whereClause, err := qb.buildWhereClause(req.Filters)
		if err != nil {
			return "", err
		}
		query += " WHERE " + whereClause
	}

	// Add time range filter
	if req.TimeRange != nil {
		timeFilter := qb.buildTimeFilter(req.TimeRange)
		if query != fmt.Sprintf("SELECT * FROM %s", table) {
			query += " AND " + timeFilter
		} else {
			query += " WHERE " + timeFilter
		}
	}

	// Add GROUP BY if aggregations
	if len(req.Aggregate) > 0 && len(req.GroupBy) > 0 {
		query = qb.buildAggregateQuery(req, table, query)
	}

	// Add ORDER BY
	if len(req.Sort) > 0 {
		orderClause := qb.buildOrderClause(req.Sort)
		query += " ORDER BY " + orderClause
	} else {
		query += " ORDER BY created_at DESC"
	}

	// Add LIMIT and OFFSET for pagination
	pageSize := req.PageSize
	if pageSize == 0 {
		pageSize = 100
	}
	if req.Page == 0 {
		req.Page = 1
	}

	offset := (req.Page - 1) * pageSize
	query += fmt.Sprintf(" LIMIT %d OFFSET %d", pageSize, offset)

	return query, nil
}

// buildWhereClause builds WHERE clause from filters
func (qb *DefaultQueryBuilder) buildWhereClause(filters map[string]interface{}) (string, error) {
	var conditions []string

	for field, value := range filters {
		condition := qb.buildFieldCondition(field, value)
		conditions = append(conditions, condition)
	}

	return strings.Join(conditions, " AND "), nil
}

// buildFieldCondition builds a condition for a single field
func (qb *DefaultQueryBuilder) buildFieldCondition(field string, value interface{}) string {
	switch v := value.(type) {
	case map[string]interface{}:
		// Handle operators like $gt, $lt, $contains, etc.
		return qb.buildOperatorCondition(field, v)
	case string:
		return fmt.Sprintf("%s = '%s'", field, sanitizeInput(v))
	case float64:
		return fmt.Sprintf("%s = %v", field, v)
	case bool:
		return fmt.Sprintf("%s = %v", field, v)
	default:
		return fmt.Sprintf("%s = '%v'", field, v)
	}
}

// buildOperatorCondition handles operators like $eq, $gt, $lt, $contains, $regex
func (qb *DefaultQueryBuilder) buildOperatorCondition(field string, operators map[string]interface{}) string {
	var conditions []string

	for op, opValue := range operators {
		switch op {
		case "$eq":
			conditions = append(conditions, fmt.Sprintf("%s = '%v'", field, opValue))
		case "$ne":
			conditions = append(conditions, fmt.Sprintf("%s != '%v'", field, opValue))
		case "$gt":
			conditions = append(conditions, fmt.Sprintf("%s > %v", field, opValue))
		case "$gte":
			conditions = append(conditions, fmt.Sprintf("%s >= %v", field, opValue))
		case "$lt":
			conditions = append(conditions, fmt.Sprintf("%s < %v", field, opValue))
		case "$lte":
			conditions = append(conditions, fmt.Sprintf("%s <= %v", field, opValue))
		case "$in":
			if arr, ok := opValue.([]interface{}); ok {
				values := make([]string, len(arr))
				for i, v := range arr {
					values[i] = fmt.Sprintf("'%v'", v)
				}
				conditions = append(conditions, fmt.Sprintf("%s IN (%s)", field, strings.Join(values, ",")))
			}
		case "$nin":
			if arr, ok := opValue.([]interface{}); ok {
				values := make([]string, len(arr))
				for i, v := range arr {
					values[i] = fmt.Sprintf("'%v'", v)
				}
				conditions = append(conditions, fmt.Sprintf("%s NOT IN (%s)", field, strings.Join(values, ",")))
			}
		case "$contains":
			conditions = append(conditions, fmt.Sprintf("%s ILIKE '%%%v%%'", field, opValue))
		case "$regex":
			conditions = append(conditions, fmt.Sprintf("%s ~ '%v'", field, opValue))
		case "$startsWith":
			conditions = append(conditions, fmt.Sprintf("%s ILIKE '%v%%'", field, opValue))
		case "$endsWith":
			conditions = append(conditions, fmt.Sprintf("%s ILIKE '%%%v'", field, opValue))
		}
	}

	if len(conditions) == 0 {
		return "1=1"
	}

	return "(" + strings.Join(conditions, " AND ") + ")"
}

// buildTimeFilter builds a time range filter
func (qb *DefaultQueryBuilder) buildTimeFilter(tr *TimeRange) string {
	now := time.Now()

	// If start and end are provided, use them
	if tr.Start != nil && tr.End != nil {
		return fmt.Sprintf("created_at BETWEEN '%s' AND '%s'", tr.Start.Format(time.RFC3339), tr.End.Format(time.RFC3339))
	}

	// Otherwise calculate from unit and value
	if tr.Value > 0 && tr.Unit != "" {
		var start time.Time

		switch tr.Unit {
		case "h":
			start = now.Add(-time.Duration(tr.Value) * time.Hour)
		case "d":
			start = now.Add(-time.Duration(tr.Value*24) * time.Hour)
		case "w":
			start = now.Add(-time.Duration(tr.Value*24*7) * time.Hour)
		case "mo":
			start = now.AddDate(0, -tr.Value, 0)
		default:
			start = now.Add(-time.Hour)
		}

		return fmt.Sprintf("created_at >= '%s'", start.Format(time.RFC3339))
	}

	return "1=1"
}

// buildAggregateQuery builds query with aggregations
func (qb *DefaultQueryBuilder) buildAggregateQuery(req QueryRequest, table string, baseQuery string) string {
	selectParts := make([]string, 0)

	// Add group by fields
	for _, field := range req.GroupBy {
		selectParts = append(selectParts, field)
	}

	// Add aggregations
	for _, agg := range req.Aggregate {
		aggFunc := qb.buildAggregateFunction(agg)
		alias := agg.Alias
		if alias == "" {
			alias = fmt.Sprintf("%s_%s", agg.Function, agg.Field)
		}
		selectParts = append(selectParts, fmt.Sprintf("%s AS %s", aggFunc, alias))
	}

	selectClause := strings.Join(selectParts, ", ")
	query := fmt.Sprintf("SELECT %s FROM %s", selectClause, table)

	// Add WHERE conditions (if any from original request)
	if len(req.Filters) > 0 {
		whereClause, _ := qb.buildWhereClause(req.Filters)
		query += " WHERE " + whereClause
	}

	// Add GROUP BY
	query += " GROUP BY " + strings.Join(req.GroupBy, ", ")

	return query
}

// buildAggregateFunction builds an aggregate function
func (qb *DefaultQueryBuilder) buildAggregateFunction(agg AggregateField) string {
	switch agg.Function {
	case "count":
		return "COUNT(*)"
	case "sum":
		return fmt.Sprintf("SUM(%s)", agg.Field)
	case "avg":
		return fmt.Sprintf("AVG(%s)", agg.Field)
	case "min":
		return fmt.Sprintf("MIN(%s)", agg.Field)
	case "max":
		return fmt.Sprintf("MAX(%s)", agg.Field)
	case "percentile":
		if agg.Param != "" {
			return fmt.Sprintf("percentile_cont(%s) WITHIN GROUP (ORDER BY %s)", agg.Param, agg.Field)
		}
		return fmt.Sprintf("percentile_cont(0.5) WITHIN GROUP (ORDER BY %s)", agg.Field)
	default:
		return fmt.Sprintf("COUNT(%s)", agg.Field)
	}
}

// buildOrderClause builds ORDER BY clause
func (qb *DefaultQueryBuilder) buildOrderClause(sorts []SortField) string {
	var clauses []string

	for _, sort := range sorts {
		direction := "ASC"
		if sort.Direction == "desc" || sort.Direction == "DESC" {
			direction = "DESC"
		}

		clauses = append(clauses, fmt.Sprintf("%s %s", sort.Field, direction))
	}

	if len(clauses) == 0 {
		return "created_at DESC"
	}

	return strings.Join(clauses, ", ")
}

// ExecuteQuery executes a query
func (qb *DefaultQueryBuilder) ExecuteQuery(ctx context.Context, sql string) ([]map[string]interface{}, error) {
	return qb.db.Query(ctx, sql)
}

// ExecuteCount executes a count query
func (qb *DefaultQueryBuilder) ExecuteCount(ctx context.Context, sql string) (int, error) {
	return qb.db.Count(ctx, sql)
}
