package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// QueryExecutionPlan represents a parsed EXPLAIN output
type QueryExecutionPlan struct {
	Query         string
	Plan          string
	NodeCount     int
	EstimatedCost float64
	EstimatedRows int64
	ExecutionTime time.Duration
}

// QueryAnalyzer analyzes query execution plans
type QueryAnalyzer struct {
	db            *sql.DB
	monitor       *PerformanceMonitor
	enableEXPLAIN bool
}

// NewQueryAnalyzer creates a new query analyzer
func NewQueryAnalyzer(db *sql.DB, monitor *PerformanceMonitor) *QueryAnalyzer {
	return &QueryAnalyzer{
		db:            db,
		monitor:       monitor,
		enableEXPLAIN: true,
	}
}

// AnalyzeQuery analyzes a query's execution plan
func (qa *QueryAnalyzer) AnalyzeQuery(ctx context.Context, query string) (*QueryExecutionPlan, error) {
	if !qa.enableEXPLAIN {
		return nil, nil
	}

	plan := &QueryExecutionPlan{
		Query: query,
	}

	// Run EXPLAIN to get execution plan
	explainQuery := fmt.Sprintf("EXPLAIN %s", query)

	rows, err := qa.db.QueryContext(ctx, explainQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to analyze query: %w", err)
	}
	defer rows.Close()

	var planLines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return nil, err
		}
		planLines = append(planLines, line)
	}

	plan.Plan = strings.Join(planLines, "\n")
	plan.NodeCount = len(planLines)

	// Extract estimated statistics from plan
	qa.extractPlanStats(plan)

	return plan, nil
}

// extractPlanStats extracts statistics from the execution plan
func (qa *QueryAnalyzer) extractPlanStats(plan *QueryExecutionPlan) {
	// Simple heuristics for parsing EXPLAIN output
	for _, line := range strings.Split(plan.Plan, "\n") {
		// Look for estimated rows
		if strings.Contains(line, "rows=") {
			fmt.Sscanf(line, "rows=%d", &plan.EstimatedRows)
		}
		// Look for estimated cost
		if strings.Contains(line, "cost=") {
			fmt.Sscanf(line, "cost=%f", &plan.EstimatedCost)
		}
	}
}

// GetQueryRecommendations provides optimization recommendations for a query
func (qa *QueryAnalyzer) GetQueryRecommendations(ctx context.Context, query string) []string {
	recommendations := []string{}

	queryUpper := strings.ToUpper(query)

	// Check for missing ORDER BY with LIMIT
	if strings.Contains(queryUpper, "LIMIT") && !strings.Contains(queryUpper, "ORDER BY") {
		recommendations = append(recommendations, "Consider adding ORDER BY to ensure consistent results with LIMIT")
	}

	// Check for SELECT *
	if strings.Contains(queryUpper, "SELECT *") {
		recommendations = append(recommendations, "Avoid SELECT * - specify only required columns")
	}

	// Check for expensive operators
	if strings.Contains(queryUpper, "LIKE") && !strings.Contains(queryUpper, "%") {
		recommendations = append(recommendations, "Use equality comparison instead of LIKE for better performance")
	}

	// Check for missing JOIN conditions
	if strings.Count(queryUpper, "JOIN") > 0 && !strings.Contains(queryUpper, "ON") {
		recommendations = append(recommendations, "Missing JOIN conditions detected")
	}

	// Check for subqueries that could be optimized
	if strings.Contains(queryUpper, "WHERE") && strings.Count(queryUpper, "SELECT") > 1 {
		recommendations = append(recommendations, "Consider using JOINs instead of subqueries")
	}

	return recommendations
}

// OptimizeQuery returns optimization suggestions for a query
func (qa *QueryAnalyzer) OptimizeQuery(ctx context.Context, query string) (string, []string) {
	recommendations := qa.GetQueryRecommendations(ctx, query)

	optimized := query

	// Suggest index hints based on query pattern
	indexHints := qa.getSuggestedIndexes(query)
	if len(indexHints) > 0 {
		recommendations = append(recommendations, indexHints...)
	}

	return optimized, recommendations
}

// getSuggestedIndexes returns suggested indexes for a query
func (qa *QueryAnalyzer) getSuggestedIndexes(query string) []string {
	suggestions := []string{}
	queryUpper := strings.ToUpper(query)

	// Suggest indexes based on WHERE clauses
	if strings.Contains(queryUpper, "WHERE") {
		if strings.Contains(queryUpper, "CATEGORY") {
			suggestions = append(suggestions, "Use index idx_ae_category for category filtering")
		}
		if strings.Contains(queryUpper, "EVENT_TYPE") {
			suggestions = append(suggestions, "Use index idx_ae_event_type for event_type filtering")
		}
		if strings.Contains(queryUpper, "CREATED_AT") {
			suggestions = append(suggestions, "Use index idx_ae_created_at_desc for time-range queries")
		}
		if strings.Contains(queryUpper, "USER_ID") {
			suggestions = append(suggestions, "Use index idx_ae_user_id for user filtering")
		}
	}

	// Suggest composite indexes for common combinations
	if strings.Contains(queryUpper, "CATEGORY") && strings.Contains(queryUpper, "CREATED_AT") {
		suggestions = append(suggestions, "Use composite index idx_ae_category_created_at")
	}
	if strings.Contains(queryUpper, "EVENT_TYPE") && strings.Contains(queryUpper, "CREATED_AT") {
		suggestions = append(suggestions, "Use composite index idx_ae_event_type_created_at")
	}

	return suggestions
}

// ProfileQuery profiles a query and returns timing information
func (qa *QueryAnalyzer) ProfileQuery(ctx context.Context, query string) (map[string]interface{}, error) {
	start := time.Now()

	rows, err := qa.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	rowCount := 0
	for rows.Next() {
		rowCount++
	}

	duration := time.Since(start)

	profile := map[string]interface{}{
		"query_text":     query,
		"execution_time": duration.Milliseconds(),
		"rows_returned":  rowCount,
		"throughput":     float64(rowCount) / duration.Seconds(),
	}

	return profile, nil
}

// DetectSlowQueries returns queries that exceed the slow query threshold
func (qa *QueryAnalyzer) DetectSlowQueries(slowThresholdMs int64) []QueryMetrics {
	if qa.monitor == nil {
		return []QueryMetrics{}
	}

	return qa.monitor.GetSlowQueries(10)
}

// GeneratePerformanceReport generates a comprehensive performance report
func (qa *QueryAnalyzer) GeneratePerformanceReport(ctx context.Context) string {
	report := strings.Builder{}
	report.WriteString("=== Query Performance Report ===\n")
	report.WriteString(fmt.Sprintf("Generated: %s\n\n", time.Now().Format(time.RFC3339)))

	if qa.monitor != nil {
		stats := qa.monitor.GetStats()
		report.WriteString("Performance Statistics:\n")
		for key, value := range stats {
			report.WriteString(fmt.Sprintf("  %s: %v\n", key, value))
		}
		report.WriteString("\n")

		// Include slow queries
		slowQueries := qa.monitor.GetSlowQueries(5)
		if len(slowQueries) > 0 {
			report.WriteString("Slowest Queries:\n")
			for _, sq := range slowQueries {
				report.WriteString(fmt.Sprintf("  %s: %dms\n", sq.Query[:min(50, len(sq.Query))], sq.DurationMs))
			}
		}
	}

	return report.String()
}
