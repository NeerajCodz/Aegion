package dashboards

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// QueryBuilder helps construct SQL queries with parameter substitution.
type QueryBuilder struct {
	baseSQL    string
	parameters map[string]interface{}
}

// NewQueryBuilder creates a new query builder.
func NewQueryBuilder(baseSQL string) *QueryBuilder {
	return &QueryBuilder{
		baseSQL:    baseSQL,
		parameters: make(map[string]interface{}),
	}
}

// WithParameter sets a query parameter.
func (qb *QueryBuilder) WithParameter(name string, value interface{}) *QueryBuilder {
	qb.parameters[name] = value
	return qb
}

// WithTimeRange sets the time range parameter.
func (qb *QueryBuilder) WithTimeRange(hours int) *QueryBuilder {
	qb.parameters["time_range"] = fmt.Sprintf("NOW() - INTERVAL '%d hours'", hours)
	return qb
}

// WithDateRange sets a date range.
func (qb *QueryBuilder) WithDateRange(startDate, endDate string) *QueryBuilder {
	qb.parameters["start_date"] = startDate
	qb.parameters["end_date"] = endDate
	return qb
}

// WithLimit sets the result limit.
func (qb *QueryBuilder) WithLimit(limit int) *QueryBuilder {
	qb.parameters["limit"] = limit
	return qb
}

// Build returns the constructed SQL query.
func (qb *QueryBuilder) Build() string {
	sql := qb.baseSQL
	for name, value := range qb.parameters {
		placeholder := fmt.Sprintf(":%s", name)
		var replacement string

		switch v := value.(type) {
		case string:
			replacement = fmt.Sprintf("'%s'", escapeSQL(v))
		case int:
			replacement = fmt.Sprintf("%d", v)
		case float64:
			replacement = fmt.Sprintf("%f", v)
		case bool:
			if v {
				replacement = "TRUE"
			} else {
				replacement = "FALSE"
			}
		default:
			replacement = fmt.Sprintf("'%v'", v)
		}

		sql = strings.ReplaceAll(sql, placeholder, replacement)
	}
	return sql
}

// ExportQuery represents parameters for exporting dashboard data.
type ExportQuery struct {
	DashboardID string
	Format      string // "csv", "json", "pdf"
	TimeRange   string
	Filters     map[string]interface{}
}

// ExportToCSV generates CSV export SQL.
func ExportToCSV(query *ExportQuery, dashboardQueries map[string]*DashboardQuery) (string, error) {
	baseQuery, err := resolveExportBaseQuery(query, dashboardQueries)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("SELECT * FROM (%s) export_rows", baseQuery), nil
}

// ExportToJSON generates JSON export SQL.
func ExportToJSON(query *ExportQuery, dashboardQueries map[string]*DashboardQuery) (string, error) {
	baseQuery, err := resolveExportBaseQuery(query, dashboardQueries)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("SELECT COALESCE(json_agg(export_rows), '[]'::json) AS data FROM (%s) export_rows", baseQuery), nil
}

// TimeRangeQuery represents a query with a time range.
type TimeRangeQuery struct {
	BaseQuery   string
	StartTime   string
	EndTime     string
	Granularity string // "minute", "hour", "day", "week", "month"
}

// QueryWithTimeRange builds a time range query.
func QueryWithTimeRange(base string, hours int, granularity string) string {
	return fmt.Sprintf(`
		%s
		AND created_at > NOW() - INTERVAL '%d hours'
		GROUP BY DATE_TRUNC('%s', created_at)
		ORDER BY DATE_TRUNC('%s', created_at) DESC
	`, base, hours, granularity, granularity)
}

// AggregateQuery represents a parameterized aggregation query.
type AggregateQuery struct {
	Table          string
	Dimensions     []string
	Metrics        []string
	Filters        map[string]interface{}
	OrderBy        string
	Limit          int
}

// Build constructs an aggregate query.
func (aq *AggregateQuery) Build() string {
	selectParts := []string{}
	for _, dim := range aq.Dimensions {
		selectParts = append(selectParts, sanitizeIdentifierExpression(dim))
	}
	for _, metric := range aq.Metrics {
		selectParts = append(selectParts, metric)
	}

	query := fmt.Sprintf("SELECT %s FROM %s", strings.Join(selectParts, ", "), sanitizeIdentifierExpression(aq.Table))

	if len(aq.Filters) > 0 {
		query += " WHERE "
		whereParts := []string{}
		keys := sortedKeys(aq.Filters)
		for _, key := range keys {
			value := aq.Filters[key]
			whereParts = append(whereParts, fmt.Sprintf("%s = %s", sanitizeIdentifierExpression(key), formatSQLValue(value)))
		}
		query += strings.Join(whereParts, " AND ")
	}

	if len(aq.Dimensions) > 0 {
		groupBy := make([]string, 0, len(aq.Dimensions))
		for _, dim := range aq.Dimensions {
			groupBy = append(groupBy, sanitizeIdentifierExpression(dim))
		}
		query += fmt.Sprintf(" GROUP BY %s", strings.Join(groupBy, ", "))
	}

	if aq.OrderBy != "" {
		query += fmt.Sprintf(" ORDER BY %s", aq.OrderBy)
	}

	if aq.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", aq.Limit)
	}

	return query
}

// ComparativePeriodQuery generates a query comparing two time periods.
type ComparativePeriodQuery struct {
	BaseQuery   string
	CurrentDays int
	PreviousDays int
}

// Build constructs a comparative query.
func (cpq *ComparativePeriodQuery) Build() string {
	return fmt.Sprintf(`
		SELECT 
			'current' as period,
			metrics.*
		FROM (
			%s
			AND created_at > NOW() - INTERVAL '%d days'
		) metrics
		UNION ALL
		SELECT 
			'previous' as period,
			metrics.*
		FROM (
			%s
			AND created_at > NOW() - INTERVAL '%d days'
			AND created_at <= NOW() - INTERVAL '%d days'
		) metrics
	`, cpq.BaseQuery, cpq.CurrentDays, cpq.BaseQuery, cpq.CurrentDays*2, cpq.CurrentDays)
}

// RankingQuery generates a ranking/leaderboard query.
type RankingQuery struct {
	BaseQuery string
	Dimension string
	Metric    string
	Limit     int
	Ascending bool
}

// Build constructs a ranking query.
func (rq *RankingQuery) Build() string {
	order := "DESC"
	if rq.Ascending {
		order = "ASC"
	}

	return fmt.Sprintf(`
		SELECT 
			ROW_NUMBER() OVER (ORDER BY %s %s) as rank,
			%s,
			%s
		FROM (
			%s
			LIMIT %d
		) ranked
	`, rq.Metric, order, rq.Dimension, rq.Metric, rq.BaseQuery, rq.Limit)
}

// AnomalyDetectionQuery generates a query for detecting anomalies.
type AnomalyDetectionQuery struct {
	BaseQuery string
	Metric    string
	StdDevs   float64 // Number of standard deviations for outlier detection
}

// Build constructs an anomaly detection query.
func (adq *AnomalyDetectionQuery) Build() string {
	return fmt.Sprintf(`
		SELECT 
			*,
			CASE 
				WHEN ABS(%s - avg_value) > %f * stddev_value THEN 'ANOMALY'
				ELSE 'NORMAL'
			END as anomaly_status
		FROM (
			%s
			SELECT 
				%s,
				AVG(%s) OVER () as avg_value,
				STDDEV(%s) OVER () as stddev_value
			FROM metrics
		) anomalies
	`, adq.Metric, adq.StdDevs, adq.BaseQuery, adq.Metric, adq.Metric, adq.Metric)
}

// FunnelQuery generates a funnel analysis query.
type FunnelQuery struct {
	Events []string // Sequential events
	Table  string
	UserIDField string
}

// Build constructs a funnel query.
func (fq *FunnelQuery) Build() string {
	selectParts := []string{}
	joinParts := []string{}

	for i, event := range fq.Events {
		alias := fmt.Sprintf("e%d", i)
		selectParts = append(selectParts, fmt.Sprintf("COUNT(DISTINCT %s.%s) as %s_count", alias, fq.UserIDField, event))

		if i == 0 {
			joinParts = append(joinParts, fmt.Sprintf("%s %s WHERE %s.event_type = '%s'", fq.Table, alias, alias, event))
		} else {
			prevAlias := fmt.Sprintf("e%d", i-1)
			joinParts = append(joinParts, fmt.Sprintf(
				"LEFT JOIN %s %s ON %s.%s = %s.%s AND %s.event_type = '%s'",
				fq.Table, alias, prevAlias, fq.UserIDField, alias, fq.UserIDField, alias, event,
			))
		}
	}

	return fmt.Sprintf("SELECT %s FROM %s", strings.Join(selectParts, ", "), strings.Join(joinParts, " "))
}

// CohortQuery generates a cohort analysis query.
type CohortQuery struct {
	CohortEvent    string
	AnalysisEvent  string
	Table          string
	UserIDField    string
	DateField      string
	DaysToAnalyze  int
}

// Build constructs a cohort query.
func (cq *CohortQuery) Build() string {
	return fmt.Sprintf(`
		SELECT 
			COALESCE(DATE_TRUNC('week', cohort_week), NOW()) as cohort_week,
			COUNT(DISTINCT user_id) as cohort_size,
			ROUND(100.0 * COUNT(DISTINCT CASE WHEN retained_week <= EXTRACT(WEEK FROM NOW()) THEN user_id END) / 
				NULLIF(COUNT(DISTINCT user_id), 0), 2) as retention_rate
		FROM (
			SELECT 
				user_id,
				DATE_TRUNC('week', created_at) as cohort_week,
				EXTRACT(WEEK FROM created_at) as retained_week
			FROM %s
			WHERE event_type = '%s'
		)
		GROUP BY DATE_TRUNC('week', cohort_week)
		ORDER BY cohort_week DESC
	`, cq.Table, cq.CohortEvent)
}

// Helper function to escape SQL strings
func escapeSQL(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// TimeBucketHelper helps generate time bucket expressions.
type TimeBucketHelper struct {
	Field       string
	Granularity string // "minute", "hour", "day", "week", "month"
}

// Expression returns the time bucket expression.
func (tbh *TimeBucketHelper) Expression() string {
	return fmt.Sprintf("DATE_TRUNC('%s', %s)", tbh.Granularity, tbh.Field)
}

// QueryTemplate represents a reusable query template with placeholders.
type QueryTemplate struct {
	Template   string
	Parameters map[string]interface{}
}

// Render renders the template with parameters.
func (qt *QueryTemplate) Render() string {
	result := qt.Template
	keys := sortedKeys(qt.Parameters)
	for _, key := range keys {
		value := qt.Parameters[key]
		placeholder := fmt.Sprintf("{%s}", key)
		result = strings.ReplaceAll(result, placeholder, formatTemplateValue(value))
	}
	return result
}

// CommonDashboardQueries provides pre-built query templates.
var CommonDashboardQueries = map[string]string{
	"active_users_today": `
		SELECT COUNT(DISTINCT user_id) as active_users
		FROM analytics_events
		WHERE created_at > NOW() - INTERVAL '1 day'
		AND user_id IS NOT NULL
	`,
	"revenue_today": `
		SELECT SUM(CAST(data->>'amount' AS NUMERIC)) as revenue
		FROM analytics_events
		WHERE created_at > NOW() - INTERVAL '1 day'
		AND event_type = 'purchase'
	`,
	"top_events": `
		SELECT event_type, COUNT(*) as count
		FROM analytics_events
		WHERE created_at > NOW() - INTERVAL '{days}' days
		GROUP BY event_type
		ORDER BY count DESC
		LIMIT {limit}
	`,
	"error_rate": `
		SELECT 
			DATE_TRUNC('hour', created_at) as time_bucket,
			ROUND(100.0 * COUNT(CASE WHEN data->>'status' LIKE '4%%' OR data->>'status' LIKE '5%%' THEN 1 END) / COUNT(*), 2) as error_rate
		FROM analytics_events
		WHERE created_at > NOW() - INTERVAL '{hours}' hours
		GROUP BY DATE_TRUNC('hour', created_at)
		ORDER BY time_bucket DESC
	`,
}

func resolveExportBaseQuery(query *ExportQuery, dashboardQueries map[string]*DashboardQuery) (string, error) {
	if query == nil {
		return "", errors.New("export query is required")
	}
	if query.DashboardID == "" {
		return "", errors.New("dashboard_id is required")
	}

	var baseSQL string
	if dashboardQueries != nil {
		if dashboardQuery, ok := dashboardQueries[query.DashboardID]; ok && dashboardQuery != nil {
			baseSQL = dashboardQuery.SQL
		}
	}
	if baseSQL == "" {
		if common, ok := CommonDashboardQueries[query.DashboardID]; ok {
			baseSQL = (&QueryTemplate{
				Template: common,
				Parameters: map[string]interface{}{
					"days":  7,
					"hours": 24,
					"limit": 1000,
				},
			}).Render()
		}
	}
	if strings.TrimSpace(baseSQL) == "" {
		return "", fmt.Errorf("dashboard query %q not found", query.DashboardID)
	}

	sql := strings.TrimSpace(baseSQL)
	if query.TimeRange != "" {
		sql = applyTimeRangeFilter(sql, query.TimeRange)
	}
	if len(query.Filters) > 0 {
		sql = applyFilterExpressions(sql, query.Filters)
	}

	return sql, nil
}

func applyTimeRangeFilter(sql, timeRange string) string {
	column, interval := parseTimeRange(timeRange)
	if interval == "" {
		return sql
	}

	return wrapWithWhere(sql, fmt.Sprintf("%s >= NOW() - INTERVAL '%s'", column, escapeSQL(interval)))
}

func parseTimeRange(timeRange string) (string, string) {
	trimmed := strings.TrimSpace(strings.ToLower(timeRange))
	if trimmed == "" {
		return "", ""
	}

	switch trimmed {
	case "1h", "hour", "1 hour":
		return "created_at", "1 hour"
	case "24h", "1d", "day", "1 day":
		return "created_at", "1 day"
	case "7d", "7 days", "week":
		return "created_at", "7 days"
	case "30d", "30 days", "month":
		return "created_at", "30 days"
	default:
		return "created_at", trimmed
	}
}

func applyFilterExpressions(sql string, filters map[string]interface{}) string {
	keys := sortedKeys(filters)
	if len(keys) == 0 {
		return sql
	}

	clauses := make([]string, 0, len(keys))
	for _, key := range keys {
		clauses = append(clauses, fmt.Sprintf("%s = %s", sanitizeIdentifierExpression(key), formatSQLValue(filters[key])))
	}

	return wrapWithWhere(sql, strings.Join(clauses, " AND "))
}

func wrapWithWhere(sql, clause string) string {
	base := strings.TrimSpace(sql)
	lowerBase := strings.ToLower(base)
	if strings.Contains(lowerBase, " where ") {
		return fmt.Sprintf("SELECT * FROM (%s) export_source WHERE %s", base, clause)
	}
	return fmt.Sprintf("SELECT * FROM (%s) export_source WHERE %s", base, clause)
}

func formatSQLValue(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return fmt.Sprintf("'%s'", escapeSQL(typed))
	case int, int32, int64, float32, float64:
		return fmt.Sprintf("%v", typed)
	case bool:
		if typed {
			return "TRUE"
		}
		return "FALSE"
	default:
		return fmt.Sprintf("'%s'", escapeSQL(fmt.Sprintf("%v", typed)))
	}
}

func formatTemplateValue(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return escapeSQL(typed)
	default:
		return fmt.Sprintf("%v", typed)
	}
}

func sortedKeys(values map[string]interface{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sanitizeIdentifierExpression(value string) string {
	replacer := strings.NewReplacer(
		";", "",
		"'", "",
		"\"", "",
		"--", "",
		"/*", "",
		"*/", "",
	)
	return replacer.Replace(strings.TrimSpace(value))
}
