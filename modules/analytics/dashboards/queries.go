package dashboards

import (
	"fmt"
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
	// Placeholder implementation - would need more context for real CSV generation
	return fmt.Sprintf("SELECT * FROM analytics_events WHERE dashboard_id = '%s'", query.DashboardID), nil
}

// ExportToJSON generates JSON export SQL.
func ExportToJSON(query *ExportQuery, dashboardQueries map[string]*DashboardQuery) (string, error) {
	return fmt.Sprintf("SELECT row_to_json(t) FROM analytics_events t WHERE dashboard_id = '%s'", query.DashboardID), nil
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
		selectParts = append(selectParts, dim)
	}
	for _, metric := range aq.Metrics {
		selectParts = append(selectParts, metric)
	}

	query := fmt.Sprintf("SELECT %s FROM %s", strings.Join(selectParts, ", "), aq.Table)

	if len(aq.Filters) > 0 {
		query += " WHERE "
		whereParts := []string{}
		for key, value := range aq.Filters {
			whereParts = append(whereParts, fmt.Sprintf("%s = '%v'", key, value))
		}
		query += strings.Join(whereParts, " AND ")
	}

	if len(aq.Dimensions) > 0 {
		query += fmt.Sprintf(" GROUP BY %s", strings.Join(aq.Dimensions, ", "))
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
	for key, value := range qt.Parameters {
		placeholder := fmt.Sprintf("{%s}", key)
		result = strings.ReplaceAll(result, placeholder, fmt.Sprintf("%v", value))
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
