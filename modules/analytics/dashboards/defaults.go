package dashboards

import (
	"fmt"
	"time"
)

// PrebuiltDashboards contains all pre-built dashboard definitions.
func PrebuiltDashboards() map[string]*Dashboard {
	now := time.Now()
	return map[string]*Dashboard{
		"auth-dashboard":      authenticationDashboard(now),
		"activity-dashboard":  userActivityDashboard(now),
		"sessions-dashboard":  sessionAnalyticsDashboard(now),
		"security-dashboard":  securityDashboard(now),
		"health-dashboard":    systemHealthDashboard(now),
	}
}

func authenticationDashboard(now time.Time) *Dashboard {
	return &Dashboard{
		ID:              "auth-dashboard",
		Name:            "Authentication Dashboard",
		Description:     "Login and authentication metrics including success rates, MFA adoption, and security insights",
		Category:        "authentication",
		IsDefault:       true,
		Layout:          "grid-3col",
		RefreshInterval: 30,
		Public:          false,
		Pinned:          true,
		CreatedAt:       now,
		UpdatedAt:       now,
		Components: []Component{
			{
				ID:          "login-success-rate",
				Type:        "time_series",
				Title:       "Login Success Rate",
				Description: "Percentage of successful logins over time",
				QueryID:     "auth_login_success_rate",
				TimeRange:   "1d",
				Metrics:     []string{"success_rate"},
				GridCol:     1,
				GridRow:     1,
				GridWidth:   1,
				GridHeight:  2,
			},
			{
				ID:          "failed-auth-reasons",
				Type:        "pie_chart",
				Title:       "Failed Auth Attempts by Reason",
				Description: "Distribution of authentication failures",
				QueryID:     "auth_failed_reasons",
				TimeRange:   "1d",
				Metrics:     []string{"count"},
				GridCol:     2,
				GridRow:     1,
				GridWidth:   1,
				GridHeight:  2,
			},
			{
				ID:          "mfa-adoption-rate",
				Type:        "gauge",
				Title:       "MFA Adoption Rate",
				Description: "Percentage of users with MFA enabled",
				QueryID:     "auth_mfa_adoption",
				TimeRange:   "1d",
				Metrics:     []string{"adoption_rate"},
				GridCol:     3,
				GridRow:     1,
				GridWidth:   1,
				GridHeight:  2,
			},
			{
				ID:          "peak-login-times",
				Type:        "heatmap",
				Title:       "Peak Login Times (Heatmap)",
				Description: "Login frequency by hour and day of week",
				QueryID:     "auth_peak_login_times",
				TimeRange:   "7d",
				Metrics:     []string{"login_count"},
				GridCol:     1,
				GridRow:     3,
				GridWidth:   2,
				GridHeight:  2,
			},
			{
				ID:          "geographic-distribution",
				Type:        "map",
				Title:       "Geographic Login Distribution",
				Description: "Logins by geographic region",
				QueryID:     "auth_geographic_distribution",
				TimeRange:   "1d",
				Metrics:     []string{"login_count"},
				GridCol:     3,
				GridRow:     3,
				GridWidth:   1,
				GridHeight:  2,
			},
			{
				ID:          "device-distribution",
				Type:        "pie_chart",
				Title:       "Session Distribution by Device Type",
				Description: "Active sessions by device (mobile, desktop, tablet)",
				QueryID:     "auth_device_distribution",
				TimeRange:   "1d",
				Metrics:     []string{"session_count"},
				GridCol:     1,
				GridRow:     5,
				GridWidth:   1,
				GridHeight:  1,
			},
			{
				ID:          "top-failing-users",
				Type:        "table",
				Title:       "Top 10 Failing Users",
				Description: "Users with most failed login attempts",
				QueryID:     "auth_top_failing_users",
				TimeRange:   "1d",
				Metrics:     []string{"failure_count"},
				GridCol:     2,
				GridRow:     5,
				GridWidth:   1,
				GridHeight:  1,
			},
			{
				ID:          "auth-errors-trend",
				Type:        "time_series",
				Title:       "Auth Errors Trend",
				Description: "Authentication error count over time",
				QueryID:     "auth_errors_trend",
				TimeRange:   "7d",
				Metrics:     []string{"error_count"},
				GridCol:     3,
				GridRow:     5,
				GridWidth:   1,
				GridHeight:  1,
			},
			{
				ID:          "avg-login-time",
				Type:        "gauge",
				Title:       "Average Login Time",
				Description: "Average authentication latency (ms)",
				QueryID:     "auth_avg_login_time",
				TimeRange:   "1d",
				Metrics:     []string{"avg_duration_ms"},
				GridCol:     1,
				GridRow:     6,
				GridWidth:   1,
				GridHeight:  1,
			},
			{
				ID:          "suspicious-activity",
				Type:        "alert_banner",
				Title:       "Suspicious Activity Alerts",
				Description: "Real-time alerts for unusual auth patterns",
				QueryID:     "auth_suspicious_activity",
				TimeRange:   "1h",
				Metrics:     []string{"alert_count"},
				GridCol:     2,
				GridRow:     6,
				GridWidth:   2,
				GridHeight:  1,
			},
		},
	}
}

func userActivityDashboard(now time.Time) *Dashboard {
	return &Dashboard{
		ID:              "activity-dashboard",
		Name:            "User Activity Dashboard",
		Description:     "User lifecycle metrics including signups, retention, and activity patterns",
		Category:        "user_activity",
		IsDefault:       true,
		Layout:          "grid-3col",
		RefreshInterval: 30,
		Public:          false,
		Pinned:          true,
		CreatedAt:       now,
		UpdatedAt:       now,
		Components: []Component{
			{
				ID:          "new-user-signups",
				Type:        "time_series",
				Title:       "New User Signups",
				Description: "Daily new user registrations",
				QueryID:     "activity_new_signups",
				TimeRange:   "30d",
				Metrics:     []string{"signup_count"},
				GridCol:     1,
				GridRow:     1,
				GridWidth:   2,
				GridHeight:  2,
			},
			{
				ID:          "active-users-gauge",
				Type:        "gauge",
				Title:       "Active Users (DAU/MAU/WAU)",
				Description: "Daily/Monthly/Weekly active users",
				QueryID:     "activity_active_users",
				TimeRange:   "1d",
				Metrics:     []string{"dau", "mau", "wau"},
				GridCol:     3,
				GridRow:     1,
				GridWidth:   1,
				GridHeight:  2,
			},
			{
				ID:          "user-lifecycle-funnel",
				Type:        "funnel",
				Title:       "User Lifecycle Funnel",
				Description: "Signup → Activated → Retained progression",
				QueryID:     "activity_user_lifecycle",
				TimeRange:   "30d",
				Metrics:     []string{"signups", "activated", "retained"},
				GridCol:     1,
				GridRow:     3,
				GridWidth:   1,
				GridHeight:  2,
			},
			{
				ID:          "most-active-users",
				Type:        "leaderboard",
				Title:       "Most Active Users (Top 10)",
				Description: "Users ranked by activity level",
				QueryID:     "activity_most_active_users",
				TimeRange:   "7d",
				Metrics:     []string{"activity_score"},
				GridCol:     2,
				GridRow:     3,
				GridWidth:   1,
				GridHeight:  2,
			},
			{
				ID:          "activity-distribution",
				Type:        "histogram",
				Title:       "User Activity Distribution",
				Description: "Distribution of user activity levels",
				QueryID:     "activity_distribution",
				TimeRange:   "7d",
				Metrics:     []string{"activity_count"},
				GridCol:     3,
				GridRow:     3,
				GridWidth:   1,
				GridHeight:  2,
			},
			{
				ID:          "churn-rate-trend",
				Type:        "time_series",
				Title:       "Churn Rate Trend",
				Description: "User churn percentage over time",
				QueryID:     "activity_churn_rate",
				TimeRange:   "30d",
				Metrics:     []string{"churn_rate"},
				GridCol:     1,
				GridRow:     5,
				GridWidth:   1,
				GridHeight:  1,
			},
			{
				ID:          "account-deletions",
				Type:        "time_series",
				Title:       "Account Deletion Trends",
				Description: "Daily account deletions",
				QueryID:     "activity_account_deletions",
				TimeRange:   "30d",
				Metrics:     []string{"deletion_count"},
				GridCol:     2,
				GridRow:     5,
				GridWidth:   1,
				GridHeight:  1,
			},
			{
				ID:          "user-role-distribution",
				Type:        "pie_chart",
				Title:       "User Role Distribution",
				Description: "Breakdown of users by role",
				QueryID:     "activity_user_roles",
				TimeRange:   "1d",
				Metrics:     []string{"user_count"},
				GridCol:     3,
				GridRow:     5,
				GridWidth:   1,
				GridHeight:  1,
			},
			{
				ID:          "regional-distribution",
				Type:        "map",
				Title:       "Regional User Distribution",
				Description: "User distribution by geographic region",
				QueryID:     "activity_regional_distribution",
				TimeRange:   "1d",
				Metrics:     []string{"user_count"},
				GridCol:     1,
				GridRow:     6,
				GridWidth:   2,
				GridHeight:  1,
			},
			{
				ID:          "retention-cohorts",
				Type:        "table",
				Title:       "Retention Cohorts",
				Description: "User cohorts and their retention rates",
				QueryID:     "activity_retention_cohorts",
				TimeRange:   "90d",
				Metrics:     []string{"retention_rate"},
				GridCol:     3,
				GridRow:     6,
				GridWidth:   1,
				GridHeight:  1,
			},
		},
	}
}

func sessionAnalyticsDashboard(now time.Time) *Dashboard {
	return &Dashboard{
		ID:              "sessions-dashboard",
		Name:            "Session Analytics Dashboard",
		Description:     "Session metrics including duration, concurrent users, and device breakdown",
		Category:        "sessions",
		IsDefault:       true,
		Layout:          "grid-3col",
		RefreshInterval: 30,
		Public:          false,
		Pinned:          true,
		CreatedAt:       now,
		UpdatedAt:       now,
		Components: []Component{
			{
				ID:          "current-active-sessions",
				Type:        "gauge",
				Title:       "Current Active Sessions",
				Description: "Real-time count of active sessions",
				QueryID:     "session_current_active",
				TimeRange:   "1h",
				Metrics:     []string{"active_session_count"},
				GridCol:     1,
				GridRow:     1,
				GridWidth:   1,
				GridHeight:  1,
			},
			{
				ID:          "session-duration-histogram",
				Type:        "histogram",
				Title:       "Session Duration Distribution",
				Description: "Distribution of session lengths",
				QueryID:     "session_duration_distribution",
				TimeRange:   "7d",
				Metrics:     []string{"duration_minutes"},
				GridCol:     2,
				GridRow:     1,
				GridWidth:   1,
				GridHeight:  2,
			},
			{
				ID:          "concurrent-users-peak",
				Type:        "time_series",
				Title:       "Concurrent Users Peak",
				Description: "Peak concurrent users over time",
				QueryID:     "session_concurrent_peak",
				TimeRange:   "7d",
				Metrics:     []string{"peak_concurrent_users"},
				GridCol:     3,
				GridRow:     1,
				GridWidth:   1,
				GridHeight:  2,
			},
			{
				ID:          "session-timeout-patterns",
				Type:        "time_series",
				Title:       "Session Timeout Patterns",
				Description: "Timeout events over time",
				QueryID:     "session_timeout_patterns",
				TimeRange:   "7d",
				Metrics:     []string{"timeout_count"},
				GridCol:     1,
				GridRow:     2,
				GridWidth:   1,
				GridHeight:  1,
			},
			{
				ID:          "device-type-distribution",
				Type:        "pie_chart",
				Title:       "Device Type Distribution",
				Description: "Sessions by device type",
				QueryID:     "session_device_type",
				TimeRange:   "1d",
				Metrics:     []string{"session_count"},
				GridCol:     1,
				GridRow:     3,
				GridWidth:   1,
				GridHeight:  1,
			},
			{
				ID:          "browser-os-breakdown",
				Type:        "table",
				Title:       "Browser/OS Breakdown",
				Description: "Top browsers and operating systems",
				QueryID:     "session_browser_os",
				TimeRange:   "7d",
				Metrics:     []string{"session_count"},
				GridCol:     2,
				GridRow:     3,
				GridWidth:   1,
				GridHeight:  2,
			},
			{
				ID:          "geographic-sessions",
				Type:        "map",
				Title:       "Geographic Session Distribution",
				Description: "Sessions by geographic region",
				QueryID:     "session_geographic",
				TimeRange:   "1d",
				Metrics:     []string{"session_count"},
				GridCol:     3,
				GridRow:     3,
				GridWidth:   1,
				GridHeight:  2,
			},
			{
				ID:          "session-lifecycle",
				Type:        "pie_chart",
				Title:       "Session Lifecycle Status",
				Description: "Sessions by status (active, completed, abandoned, timed out)",
				QueryID:     "session_lifecycle_status",
				TimeRange:   "1d",
				Metrics:     []string{"session_count"},
				GridCol:     1,
				GridRow:     4,
				GridWidth:   1,
				GridHeight:  1,
			},
			{
				ID:          "avg-duration-by-segment",
				Type:        "bar_chart",
				Title:       "Avg Session Duration by User Segment",
				Description: "Average duration grouped by user segment",
				QueryID:     "session_avg_duration_segment",
				TimeRange:   "7d",
				Metrics:     []string{"avg_duration_minutes"},
				GridCol:     2,
				GridRow:     5,
				GridWidth:   1,
				GridHeight:  1,
			},
			{
				ID:          "top-user-sessions",
				Type:        "table",
				Title:       "Top User Sessions",
				Description: "Longest active sessions by user",
				QueryID:     "session_top_user_sessions",
				TimeRange:   "1d",
				Metrics:     []string{"duration_minutes"},
				GridCol:     3,
				GridRow:     5,
				GridWidth:   1,
				GridHeight:  1,
			},
		},
	}
}

func securityDashboard(now time.Time) *Dashboard {
	return &Dashboard{
		ID:              "security-dashboard",
		Name:            "Security Dashboard",
		Description:     "Security metrics including suspicious activities, rate limits, and policy violations",
		Category:        "security",
		IsDefault:       true,
		Layout:          "grid-3col",
		RefreshInterval: 15,
		Public:          false,
		Pinned:          true,
		CreatedAt:       now,
		UpdatedAt:       now,
		Components: []Component{
			{
				ID:          "suspicious-activities",
				Type:        "timeline",
				Title:       "Suspicious Activities (Timeline)",
				Description: "Recent suspicious activity events",
				QueryID:     "security_suspicious_activities",
				TimeRange:   "1d",
				Metrics:     []string{"event_count"},
				GridCol:     1,
				GridRow:     1,
				GridWidth:   2,
				GridHeight:  2,
			},
			{
				ID:          "rate-limit-violations",
				Type:        "counter",
				Title:       "Rate Limit Violations",
				Description: "Total rate limit violations (24h)",
				QueryID:     "security_rate_limit_violations",
				TimeRange:   "1d",
				Metrics:     []string{"violation_count"},
				GridCol:     3,
				GridRow:     1,
				GridWidth:   1,
				GridHeight:  1,
			},
			{
				ID:          "policy-violations",
				Type:        "time_series",
				Title:       "Policy Violation Attempts",
				Description: "Policy violation count over time",
				QueryID:     "security_policy_violations",
				TimeRange:   "7d",
				Metrics:     []string{"violation_count"},
				GridCol:     1,
				GridRow:     3,
				GridWidth:   1,
				GridHeight:  1,
			},
			{
				ID:          "geographic-anomalies",
				Type:        "map",
				Title:       "Geographic Anomalies",
				Description: "Map of unusual geographic access patterns",
				QueryID:     "security_geographic_anomalies",
				TimeRange:   "1d",
				Metrics:     []string{"anomaly_count"},
				GridCol:     2,
				GridRow:     3,
				GridWidth:   1,
				GridHeight:  2,
			},
			{
				ID:          "unusual-login-patterns",
				Type:        "alert_banner",
				Title:       "Unusual Login Pattern Alerts",
				Description: "Alerts for detected unusual login patterns",
				QueryID:     "security_unusual_patterns",
				TimeRange:   "1h",
				Metrics:     []string{"alert_count"},
				GridCol:     3,
				GridRow:     2,
				GridWidth:   1,
				GridHeight:  1,
			},
			{
				ID:          "brute-force-attempts",
				Type:        "time_series",
				Title:       "Brute Force Attempts (Trending)",
				Description: "Brute force attack attempts over time",
				QueryID:     "security_brute_force",
				TimeRange:   "7d",
				Metrics:     []string{"attempt_count"},
				GridCol:     3,
				GridRow:     3,
				GridWidth:   1,
				GridHeight:  1,
			},
			{
				ID:          "token-abuse-incidents",
				Type:        "counter",
				Title:       "Token Abuse Incidents",
				Description: "Detected token abuse attempts",
				QueryID:     "security_token_abuse",
				TimeRange:   "1d",
				Metrics:     []string{"incident_count"},
				GridCol:     1,
				GridRow:     4,
				GridWidth:   1,
				GridHeight:  1,
			},
			{
				ID:          "deprecated-protocol-usage",
				Type:        "pie_chart",
				Title:       "Deprecated Protocol Usage",
				Description: "Breakdown of deprecated protocol usage",
				QueryID:     "security_deprecated_protocols",
				TimeRange:   "1d",
				Metrics:     []string{"usage_count"},
				GridCol:     2,
				GridRow:     5,
				GridWidth:   1,
				GridHeight:  1,
			},
			{
				ID:          "failed-mfa-attempts",
				Type:        "time_series",
				Title:       "Failed MFA Attempts",
				Description: "MFA challenge failures over time",
				QueryID:     "security_failed_mfa",
				TimeRange:   "7d",
				Metrics:     []string{"failure_count"},
				GridCol:     3,
				GridRow:     4,
				GridWidth:   1,
				GridHeight:  1,
			},
			{
				ID:          "recent-security-events",
				Type:        "table",
				Title:       "Recent Security Events",
				Description: "Latest security-related events",
				QueryID:     "security_recent_events",
				TimeRange:   "1d",
				Metrics:     []string{"event_id"},
				GridCol:     1,
				GridRow:     5,
				GridWidth:   1,
				GridHeight:  1,
			},
		},
	}
}

func systemHealthDashboard(now time.Time) *Dashboard {
	return &Dashboard{
		ID:              "health-dashboard",
		Name:            "System Health Dashboard",
		Description:     "System performance metrics including API latency, error rates, and resource utilization",
		Category:        "system_health",
		IsDefault:       true,
		Layout:          "grid-3col",
		RefreshInterval: 30,
		Public:          false,
		Pinned:          true,
		CreatedAt:       now,
		UpdatedAt:       now,
		Components: []Component{
			{
				ID:          "api-latency",
				Type:        "time_series",
				Title:       "API Latency (with Percentiles)",
				Description: "Response time percentiles (p50, p95, p99)",
				QueryID:     "health_api_latency",
				TimeRange:   "1d",
				Metrics:     []string{"p50", "p95", "p99"},
				GridCol:     1,
				GridRow:     1,
				GridWidth:   2,
				GridHeight:  2,
			},
			{
				ID:          "error-rate",
				Type:        "time_series",
				Title:       "Error Rate",
				Description: "API error rate over time",
				QueryID:     "health_error_rate",
				TimeRange:   "1d",
				Metrics:     []string{"error_rate"},
				GridCol:     3,
				GridRow:     1,
				GridWidth:   1,
				GridHeight:  2,
			},
			{
				ID:          "db-query-performance",
				Type:        "bar_chart",
				Title:       "Database Query Performance",
				Description: "Average query time and connection pool status",
				QueryID:     "health_db_performance",
				TimeRange:   "1d",
				Metrics:     []string{"avg_query_time_ms", "active_connections"},
				GridCol:     1,
				GridRow:     3,
				GridWidth:   1,
				GridHeight:  1,
			},
			{
				ID:          "storage-occupancy",
				Type:        "gauge",
				Title:       "Storage Tier Occupancy",
				Description: "Gauge showing storage usage per tier",
				QueryID:     "health_storage_occupancy",
				TimeRange:   "1d",
				Metrics:     []string{"hot_usage", "warm_usage", "cold_usage"},
				GridCol:     2,
				GridRow:     3,
				GridWidth:   1,
				GridHeight:  1,
			},
			{
				ID:          "sync-lag",
				Type:        "gauge",
				Title:       "Sync Lag (Real-time)",
				Description: "Current data sync lag in seconds",
				QueryID:     "health_sync_lag",
				TimeRange:   "1h",
				Metrics:     []string{"lag_seconds"},
				GridCol:     3,
				GridRow:     3,
				GridWidth:   1,
				GridHeight:  1,
			},
			{
				ID:          "webhook-delivery-success",
				Type:        "gauge",
				Title:       "Webhook Delivery Success Rate",
				Description: "Percentage of successful webhook deliveries",
				QueryID:     "health_webhook_success",
				TimeRange:   "1d",
				Metrics:     []string{"success_rate"},
				GridCol:     1,
				GridRow:     4,
				GridWidth:   1,
				GridHeight:  1,
			},
			{
				ID:          "cache-hit-ratio",
				Type:        "gauge",
				Title:       "Cache Hit Ratio",
				Description: "Query cache hit percentage",
				QueryID:     "health_cache_hit_ratio",
				TimeRange:   "1d",
				Metrics:     []string{"hit_ratio"},
				GridCol:     2,
				GridRow:     4,
				GridWidth:   1,
				GridHeight:  1,
			},
			{
				ID:          "memory-usage",
				Type:        "gauge",
				Title:       "Memory Usage (DuckDB)",
				Description: "Current DuckDB memory consumption",
				QueryID:     "health_memory_usage",
				TimeRange:   "1h",
				Metrics:     []string{"memory_mb"},
				GridCol:     3,
				GridRow:     4,
				GridWidth:   1,
				GridHeight:  1,
			},
			{
				ID:          "query-throughput",
				Type:        "time_series",
				Title:       "Query Count/Sec",
				Description: "Query throughput over time",
				QueryID:     "health_query_throughput",
				TimeRange:   "1d",
				Metrics:     []string{"queries_per_sec"},
				GridCol:     1,
				GridRow:     5,
				GridWidth:   2,
				GridHeight:  1,
			},
			{
				ID:          "slowest-queries",
				Type:        "table",
				Title:       "Slowest Queries",
				Description: "Top 10 slowest queries",
				QueryID:     "health_slowest_queries",
				TimeRange:   "1d",
				Metrics:     []string{"duration_ms"},
				GridCol:     3,
				GridRow:     5,
				GridWidth:   1,
				GridHeight:  1,
			},
		},
	}
}

// PrebuiltQueries contains all pre-built query definitions for dashboards.
func PrebuiltQueries() map[string]*DashboardQuery {
	now := time.Now()
	return map[string]*DashboardQuery{
		// Authentication Dashboard Queries
		"auth_login_success_rate": {
			ID:          "auth_login_success_rate",
			Name:        "Login Success Rate",
			Category:    "authentication",
			Description: "Percentage of successful logins over time",
			SQL: fmt.Sprintf(`
				SELECT 
					DATE_TRUNC('hour', created_at) as time_bucket,
					ROUND(100.0 * COUNT(CASE WHEN data->>'status' = 'success' THEN 1 END) / COUNT(*), 2) as success_rate
				FROM analytics_events
				WHERE category = 'authentication' 
					AND event_type = 'login'
					AND created_at > NOW() - INTERVAL '1 day'
				GROUP BY DATE_TRUNC('hour', created_at)
				ORDER BY time_bucket DESC
			`),
			CacheTTL:  300,
			CreatedAt: now,
			UpdatedAt: now,
		},
		"auth_failed_reasons": {
			ID:          "auth_failed_reasons",
			Name:        "Failed Auth Reasons",
			Category:    "authentication",
			Description: "Distribution of authentication failure reasons",
			SQL: fmt.Sprintf(`
				SELECT 
					data->>'reason' as reason,
					COUNT(*) as count
				FROM analytics_events
				WHERE category = 'authentication' 
					AND event_type = 'login_failed'
					AND created_at > NOW() - INTERVAL '1 day'
				GROUP BY data->>'reason'
				ORDER BY count DESC
			`),
			CacheTTL:  300,
			CreatedAt: now,
			UpdatedAt: now,
		},
		"auth_mfa_adoption": {
			ID:          "auth_mfa_adoption",
			Name:        "MFA Adoption Rate",
			Category:    "authentication",
			Description: "Percentage of users with MFA enabled",
			SQL: fmt.Sprintf(`
				SELECT 
					ROUND(100.0 * COUNT(CASE WHEN data->>'mfa_enabled' = 'true' THEN 1 END) / COUNT(DISTINCT user_id), 2) as adoption_rate
				FROM analytics_events
				WHERE category = 'authentication'
					AND created_at > NOW() - INTERVAL '1 day'
					AND user_id IS NOT NULL
			`),
			CacheTTL:  600,
			CreatedAt: now,
			UpdatedAt: now,
		},
		"auth_peak_login_times": {
			ID:          "auth_peak_login_times",
			Name:        "Peak Login Times",
			Category:    "authentication",
			Description: "Login frequency heatmap by hour and day",
			SQL: fmt.Sprintf(`
				SELECT 
					EXTRACT(DOW FROM created_at) as day_of_week,
					EXTRACT(HOUR FROM created_at) as hour_of_day,
					COUNT(*) as login_count
				FROM analytics_events
				WHERE category = 'authentication' 
					AND event_type = 'login'
					AND created_at > NOW() - INTERVAL '7 days'
				GROUP BY EXTRACT(DOW FROM created_at), EXTRACT(HOUR FROM created_at)
				ORDER BY day_of_week, hour_of_day
			`),
			CacheTTL:  600,
			CreatedAt: now,
			UpdatedAt: now,
		},
		"auth_geographic_distribution": {
			ID:          "auth_geographic_distribution",
			Name:        "Geographic Login Distribution",
			Category:    "authentication",
			Description: "Login distribution by geographic region",
			SQL: fmt.Sprintf(`
				SELECT 
					COALESCE(data->>'country', 'Unknown') as country,
					COUNT(*) as login_count
				FROM analytics_events
				WHERE category = 'authentication' 
					AND event_type = 'login'
					AND created_at > NOW() - INTERVAL '1 day'
				GROUP BY COALESCE(data->>'country', 'Unknown')
				ORDER BY login_count DESC
				LIMIT 20
			`),
			CacheTTL:  300,
			CreatedAt: now,
			UpdatedAt: now,
		},
		"auth_device_distribution": {
			ID:          "auth_device_distribution",
			Name:        "Device Distribution",
			Category:    "authentication",
			Description: "Session distribution by device type",
			SQL: fmt.Sprintf(`
				SELECT 
					COALESCE(data->>'device_type', 'Unknown') as device_type,
					COUNT(*) as session_count
				FROM analytics_events
				WHERE category = 'authentication' 
					AND created_at > NOW() - INTERVAL '1 day'
				GROUP BY COALESCE(data->>'device_type', 'Unknown')
				ORDER BY session_count DESC
			`),
			CacheTTL:  300,
			CreatedAt: now,
			UpdatedAt: now,
		},
		"auth_top_failing_users": {
			ID:          "auth_top_failing_users",
			Name:        "Top Failing Users",
			Category:    "authentication",
			Description: "Users with most failed login attempts",
			SQL: fmt.Sprintf(`
				SELECT 
					user_id,
					COUNT(*) as failure_count
				FROM analytics_events
				WHERE category = 'authentication' 
					AND event_type = 'login_failed'
					AND created_at > NOW() - INTERVAL '1 day'
					AND user_id IS NOT NULL
				GROUP BY user_id
				ORDER BY failure_count DESC
				LIMIT 10
			`),
			CacheTTL:  300,
			CreatedAt: now,
			UpdatedAt: now,
		},
		"auth_errors_trend": {
			ID:          "auth_errors_trend",
			Name:        "Auth Errors Trend",
			Category:    "authentication",
			Description: "Authentication error count over time",
			SQL: fmt.Sprintf(`
				SELECT 
					DATE_TRUNC('hour', created_at) as time_bucket,
					COUNT(*) as error_count
				FROM analytics_events
				WHERE category = 'authentication' 
					AND event_type LIKE 'login_failed%%'
					AND created_at > NOW() - INTERVAL '7 days'
				GROUP BY DATE_TRUNC('hour', created_at)
				ORDER BY time_bucket DESC
			`),
			CacheTTL:  300,
			CreatedAt: now,
			UpdatedAt: now,
		},
		"auth_avg_login_time": {
			ID:          "auth_avg_login_time",
			Name:        "Average Login Time",
			Category:    "authentication",
			Description: "Average authentication latency",
			SQL: fmt.Sprintf(`
				SELECT 
					ROUND(AVG(CAST(data->>'duration_ms' AS NUMERIC)), 2) as avg_duration_ms
				FROM analytics_events
				WHERE category = 'authentication' 
					AND event_type = 'login'
					AND created_at > NOW() - INTERVAL '1 day'
			`),
			CacheTTL:  300,
			CreatedAt: now,
			UpdatedAt: now,
		},
		"auth_suspicious_activity": {
			ID:          "auth_suspicious_activity",
			Name:        "Suspicious Activity Alerts",
			Category:    "authentication",
			Description: "Real-time alerts for unusual auth patterns",
			SQL: fmt.Sprintf(`
				SELECT 
					COUNT(*) as alert_count
				FROM analytics_events
				WHERE category = 'authentication' 
					AND data->>'is_suspicious' = 'true'
					AND created_at > NOW() - INTERVAL '1 hour'
			`),
			CacheTTL:  60,
			CreatedAt: now,
			UpdatedAt: now,
		},

		// User Activity Dashboard Queries
		"activity_new_signups": {
			ID:          "activity_new_signups",
			Name:        "New User Signups",
			Category:    "user_activity",
			Description: "Daily new user registrations",
			SQL: fmt.Sprintf(`
				SELECT 
					DATE_TRUNC('day', created_at) as signup_date,
					COUNT(*) as signup_count
				FROM analytics_events
				WHERE event_type = 'user_signup'
					AND created_at > NOW() - INTERVAL '30 days'
				GROUP BY DATE_TRUNC('day', created_at)
				ORDER BY signup_date DESC
			`),
			CacheTTL:  600,
			CreatedAt: now,
			UpdatedAt: now,
		},
		"activity_active_users": {
			ID:          "activity_active_users",
			Name:        "Active Users",
			Category:    "user_activity",
			Description: "Daily/Monthly/Weekly active users",
			SQL: fmt.Sprintf(`
				SELECT 
					COUNT(DISTINCT CASE WHEN created_at > NOW() - INTERVAL '1 day' THEN user_id END) as dau,
					COUNT(DISTINCT CASE WHEN created_at > NOW() - INTERVAL '7 days' THEN user_id END) as wau,
					COUNT(DISTINCT CASE WHEN created_at > NOW() - INTERVAL '30 days' THEN user_id END) as mau
				FROM analytics_events
				WHERE category IN ('authentication', 'user_activity')
					AND user_id IS NOT NULL
			`),
			CacheTTL:  300,
			CreatedAt: now,
			UpdatedAt: now,
		},
		"activity_user_lifecycle": {
			ID:          "activity_user_lifecycle",
			Name:        "User Lifecycle Funnel",
			Category:    "user_activity",
			Description: "Signup → Activated → Retained progression",
			SQL: fmt.Sprintf(`
				SELECT 
					COUNT(DISTINCT CASE WHEN event_type = 'user_signup' THEN user_id END) as signups,
					COUNT(DISTINCT CASE WHEN event_type = 'user_activated' THEN user_id END) as activated,
					COUNT(DISTINCT CASE WHEN event_type = 'user_retained' THEN user_id END) as retained
				FROM analytics_events
				WHERE event_type IN ('user_signup', 'user_activated', 'user_retained')
					AND created_at > NOW() - INTERVAL '30 days'
			`),
			CacheTTL:  600,
			CreatedAt: now,
			UpdatedAt: now,
		},
		"activity_most_active_users": {
			ID:          "activity_most_active_users",
			Name:        "Most Active Users",
			Category:    "user_activity",
			Description: "Top users by activity score",
			SQL: fmt.Sprintf(`
				SELECT 
					user_id,
					COUNT(*) as activity_score
				FROM analytics_events
				WHERE category IN ('authentication', 'user_activity')
					AND created_at > NOW() - INTERVAL '7 days'
					AND user_id IS NOT NULL
				GROUP BY user_id
				ORDER BY activity_score DESC
				LIMIT 10
			`),
			CacheTTL:  300,
			CreatedAt: now,
			UpdatedAt: now,
		},
		"activity_distribution": {
			ID:          "activity_distribution",
			Name:        "Activity Distribution",
			Category:    "user_activity",
			Description: "Distribution of user activity levels",
			SQL: fmt.Sprintf(`
				SELECT 
					COUNT(*) as activity_count,
					WIDTH_BUCKET(COUNT(*), 1, 100, 10) as bucket
				FROM analytics_events
				WHERE category IN ('authentication', 'user_activity')
					AND created_at > NOW() - INTERVAL '7 days'
					AND user_id IS NOT NULL
				GROUP BY user_id
			`),
			CacheTTL:  600,
			CreatedAt: now,
			UpdatedAt: now,
		},
		"activity_churn_rate": {
			ID:          "activity_churn_rate",
			Name:        "Churn Rate Trend",
			Category:    "user_activity",
			Description: "User churn percentage over time",
			SQL: fmt.Sprintf(`
				SELECT 
					DATE_TRUNC('day', created_at) as churn_date,
					ROUND(100.0 * COUNT(CASE WHEN event_type = 'user_churned' THEN 1 END) / 
						NULLIF(COUNT(DISTINCT user_id), 0), 2) as churn_rate
				FROM analytics_events
				WHERE created_at > NOW() - INTERVAL '30 days'
				GROUP BY DATE_TRUNC('day', created_at)
				ORDER BY churn_date DESC
			`),
			CacheTTL:  600,
			CreatedAt: now,
			UpdatedAt: now,
		},
		"activity_account_deletions": {
			ID:          "activity_account_deletions",
			Name:        "Account Deletions",
			Category:    "user_activity",
			Description: "Daily account deletions",
			SQL: fmt.Sprintf(`
				SELECT 
					DATE_TRUNC('day', created_at) as deletion_date,
					COUNT(*) as deletion_count
				FROM analytics_events
				WHERE event_type = 'account_deleted'
					AND created_at > NOW() - INTERVAL '30 days'
				GROUP BY DATE_TRUNC('day', created_at)
				ORDER BY deletion_date DESC
			`),
			CacheTTL:  600,
			CreatedAt: now,
			UpdatedAt: now,
		},
		"activity_user_roles": {
			ID:          "activity_user_roles",
			Name:        "User Role Distribution",
			Category:    "user_activity",
			Description: "Breakdown of users by role",
			SQL: fmt.Sprintf(`
				SELECT 
					COALESCE(data->>'role', 'unassigned') as role,
					COUNT(DISTINCT user_id) as user_count
				FROM analytics_events
				WHERE category = 'user_activity'
					AND created_at > NOW() - INTERVAL '1 day'
					AND user_id IS NOT NULL
				GROUP BY COALESCE(data->>'role', 'unassigned')
				ORDER BY user_count DESC
			`),
			CacheTTL:  600,
			CreatedAt: now,
			UpdatedAt: now,
		},
		"activity_regional_distribution": {
			ID:          "activity_regional_distribution",
			Name:        "Regional User Distribution",
			Category:    "user_activity",
			Description: "User distribution by region",
			SQL: fmt.Sprintf(`
				SELECT 
					COALESCE(data->>'country', 'Unknown') as country,
					COUNT(DISTINCT user_id) as user_count
				FROM analytics_events
				WHERE created_at > NOW() - INTERVAL '1 day'
					AND user_id IS NOT NULL
				GROUP BY COALESCE(data->>'country', 'Unknown')
				ORDER BY user_count DESC
				LIMIT 20
			`),
			CacheTTL:  600,
			CreatedAt: now,
			UpdatedAt: now,
		},
		"activity_retention_cohorts": {
			ID:          "activity_retention_cohorts",
			Name:        "Retention Cohorts",
			Category:    "user_activity",
			Description: "User cohorts and retention rates",
			SQL: fmt.Sprintf(`
				SELECT 
					DATE_TRUNC('week', created_at) as cohort_week,
					COUNT(DISTINCT user_id) as cohort_size,
					ROUND(100.0 * COUNT(DISTINCT CASE WHEN data->>'retained' = 'true' THEN user_id END) / 
						NULLIF(COUNT(DISTINCT user_id), 0), 2) as retention_rate
				FROM analytics_events
				WHERE event_type IN ('user_signup', 'user_retained')
					AND created_at > NOW() - INTERVAL '90 days'
				GROUP BY DATE_TRUNC('week', created_at)
				ORDER BY cohort_week DESC
			`),
			CacheTTL:  600,
			CreatedAt: now,
			UpdatedAt: now,
		},

		// Session Analytics Dashboard Queries
		"session_current_active": {
			ID:          "session_current_active",
			Name:        "Current Active Sessions",
			Category:    "sessions",
			Description: "Real-time count of active sessions",
			SQL: fmt.Sprintf(`
				SELECT 
					COUNT(DISTINCT session_id) as active_session_count
				FROM analytics_events
				WHERE category = 'sessions'
					AND event_type = 'session_active'
					AND created_at > NOW() - INTERVAL '1 hour'
					AND session_id IS NOT NULL
			`),
			CacheTTL:  60,
			CreatedAt: now,
			UpdatedAt: now,
		},
		"session_duration_distribution": {
			ID:          "session_duration_distribution",
			Name:        "Session Duration Distribution",
			Category:    "sessions",
			Description: "Distribution of session lengths",
			SQL: fmt.Sprintf(`
				SELECT 
					CAST(data->>'duration_seconds' AS NUMERIC) / 60.0 as duration_minutes,
					COUNT(*) as session_count
				FROM analytics_events
				WHERE category = 'sessions'
					AND event_type = 'session_ended'
					AND created_at > NOW() - INTERVAL '7 days'
				GROUP BY CAST(data->>'duration_seconds' AS NUMERIC)
				ORDER BY duration_minutes
			`),
			CacheTTL:  600,
			CreatedAt: now,
			UpdatedAt: now,
		},
		"session_concurrent_peak": {
			ID:          "session_concurrent_peak",
			Name:        "Concurrent Users Peak",
			Category:    "sessions",
			Description: "Peak concurrent users over time",
			SQL: fmt.Sprintf(`
				SELECT 
					DATE_TRUNC('hour', created_at) as time_bucket,
					MAX(CAST(data->>'concurrent_users' AS INTEGER)) as peak_concurrent_users
				FROM analytics_events
				WHERE category = 'sessions'
					AND created_at > NOW() - INTERVAL '7 days'
				GROUP BY DATE_TRUNC('hour', created_at)
				ORDER BY time_bucket DESC
			`),
			CacheTTL:  300,
			CreatedAt: now,
			UpdatedAt: now,
		},
		"session_timeout_patterns": {
			ID:          "session_timeout_patterns",
			Name:        "Session Timeout Patterns",
			Category:    "sessions",
			Description: "Timeout events over time",
			SQL: fmt.Sprintf(`
				SELECT 
					DATE_TRUNC('hour', created_at) as time_bucket,
					COUNT(*) as timeout_count
				FROM analytics_events
				WHERE category = 'sessions'
					AND event_type = 'session_timeout'
					AND created_at > NOW() - INTERVAL '7 days'
				GROUP BY DATE_TRUNC('hour', created_at)
				ORDER BY time_bucket DESC
			`),
			CacheTTL:  300,
			CreatedAt: now,
			UpdatedAt: now,
		},
		"session_device_type": {
			ID:          "session_device_type",
			Name:        "Device Type Distribution",
			Category:    "sessions",
			Description: "Sessions by device type",
			SQL: fmt.Sprintf(`
				SELECT 
					COALESCE(data->>'device_type', 'Unknown') as device_type,
					COUNT(*) as session_count
				FROM analytics_events
				WHERE category = 'sessions'
					AND created_at > NOW() - INTERVAL '1 day'
				GROUP BY COALESCE(data->>'device_type', 'Unknown')
				ORDER BY session_count DESC
			`),
			CacheTTL:  300,
			CreatedAt: now,
			UpdatedAt: now,
		},
		"session_browser_os": {
			ID:          "session_browser_os",
			Name:        "Browser/OS Breakdown",
			Category:    "sessions",
			Description: "Top browsers and operating systems",
			SQL: fmt.Sprintf(`
				SELECT 
					COALESCE(data->>'browser', 'Unknown') as browser,
					COALESCE(data->>'os', 'Unknown') as os,
					COUNT(*) as session_count
				FROM analytics_events
				WHERE category = 'sessions'
					AND created_at > NOW() - INTERVAL '7 days'
				GROUP BY COALESCE(data->>'browser', 'Unknown'), COALESCE(data->>'os', 'Unknown')
				ORDER BY session_count DESC
				LIMIT 20
			`),
			CacheTTL:  600,
			CreatedAt: now,
			UpdatedAt: now,
		},
		"session_geographic": {
			ID:          "session_geographic",
			Name:        "Geographic Session Distribution",
			Category:    "sessions",
			Description: "Sessions by geographic region",
			SQL: fmt.Sprintf(`
				SELECT 
					COALESCE(data->>'country', 'Unknown') as country,
					COUNT(*) as session_count
				FROM analytics_events
				WHERE category = 'sessions'
					AND created_at > NOW() - INTERVAL '1 day'
				GROUP BY COALESCE(data->>'country', 'Unknown')
				ORDER BY session_count DESC
				LIMIT 20
			`),
			CacheTTL:  300,
			CreatedAt: now,
			UpdatedAt: now,
		},
		"session_lifecycle_status": {
			ID:          "session_lifecycle_status",
			Name:        "Session Lifecycle Status",
			Category:    "sessions",
			Description: "Sessions by status",
			SQL: fmt.Sprintf(`
				SELECT 
					COALESCE(data->>'status', 'unknown') as status,
					COUNT(*) as session_count
				FROM analytics_events
				WHERE category = 'sessions'
					AND created_at > NOW() - INTERVAL '1 day'
				GROUP BY COALESCE(data->>'status', 'unknown')
				ORDER BY session_count DESC
			`),
			CacheTTL:  300,
			CreatedAt: now,
			UpdatedAt: now,
		},
		"session_avg_duration_segment": {
			ID:          "session_avg_duration_segment",
			Name:        "Avg Duration by Segment",
			Category:    "sessions",
			Description: "Average duration grouped by segment",
			SQL: fmt.Sprintf(`
				SELECT 
					COALESCE(data->>'user_segment', 'Unknown') as user_segment,
					ROUND(AVG(CAST(data->>'duration_seconds' AS NUMERIC)) / 60.0, 2) as avg_duration_minutes
				FROM analytics_events
				WHERE category = 'sessions'
					AND event_type = 'session_ended'
					AND created_at > NOW() - INTERVAL '7 days'
				GROUP BY COALESCE(data->>'user_segment', 'Unknown')
				ORDER BY avg_duration_minutes DESC
			`),
			CacheTTL:  600,
			CreatedAt: now,
			UpdatedAt: now,
		},
		"session_top_user_sessions": {
			ID:          "session_top_user_sessions",
			Name:        "Top User Sessions",
			Category:    "sessions",
			Description: "Longest active sessions by user",
			SQL: fmt.Sprintf(`
				SELECT 
					user_id,
					session_id,
					CAST(data->>'duration_seconds' AS NUMERIC) / 60.0 as duration_minutes
				FROM analytics_events
				WHERE category = 'sessions'
					AND event_type = 'session_ended'
					AND created_at > NOW() - INTERVAL '1 day'
					AND user_id IS NOT NULL
				ORDER BY duration_minutes DESC
				LIMIT 10
			`),
			CacheTTL:  300,
			CreatedAt: now,
			UpdatedAt: now,
		},

		// Security Dashboard Queries
		"security_suspicious_activities": {
			ID:          "security_suspicious_activities",
			Name:        "Suspicious Activities",
			Category:    "security",
			Description: "Recent suspicious activity events",
			SQL: fmt.Sprintf(`
				SELECT 
					created_at as timestamp,
					event_type,
					COALESCE(data->>'reason', 'unknown') as reason,
					COUNT(*) as event_count
				FROM analytics_events
				WHERE category = 'security'
					AND data->>'is_suspicious' = 'true'
					AND created_at > NOW() - INTERVAL '1 day'
				GROUP BY created_at, event_type, COALESCE(data->>'reason', 'unknown')
				ORDER BY created_at DESC
				LIMIT 20
			`),
			CacheTTL:  60,
			CreatedAt: now,
			UpdatedAt: now,
		},
		"security_rate_limit_violations": {
			ID:          "security_rate_limit_violations",
			Name:        "Rate Limit Violations",
			Category:    "security",
			Description: "Total rate limit violations",
			SQL: fmt.Sprintf(`
				SELECT 
					COUNT(*) as violation_count
				FROM analytics_events
				WHERE event_type = 'rate_limit_exceeded'
					AND created_at > NOW() - INTERVAL '1 day'
			`),
			CacheTTL:  300,
			CreatedAt: now,
			UpdatedAt: now,
		},
		"security_policy_violations": {
			ID:          "security_policy_violations",
			Name:        "Policy Violations",
			Category:    "security",
			Description: "Policy violation count over time",
			SQL: fmt.Sprintf(`
				SELECT 
					DATE_TRUNC('hour', created_at) as time_bucket,
					COUNT(*) as violation_count
				FROM analytics_events
				WHERE category = 'security'
					AND event_type = 'policy_violation'
					AND created_at > NOW() - INTERVAL '7 days'
				GROUP BY DATE_TRUNC('hour', created_at)
				ORDER BY time_bucket DESC
			`),
			CacheTTL:  300,
			CreatedAt: now,
			UpdatedAt: now,
		},
		"security_geographic_anomalies": {
			ID:          "security_geographic_anomalies",
			Name:        "Geographic Anomalies",
			Category:    "security",
			Description: "Unusual geographic access patterns",
			SQL: fmt.Sprintf(`
				SELECT 
					COALESCE(data->>'country', 'Unknown') as country,
					COUNT(*) as anomaly_count
				FROM analytics_events
				WHERE category = 'security'
					AND event_type = 'geographic_anomaly'
					AND created_at > NOW() - INTERVAL '1 day'
				GROUP BY COALESCE(data->>'country', 'Unknown')
				ORDER BY anomaly_count DESC
				LIMIT 20
			`),
			CacheTTL:  300,
			CreatedAt: now,
			UpdatedAt: now,
		},
		"security_unusual_patterns": {
			ID:          "security_unusual_patterns",
			Name:        "Unusual Patterns",
			Category:    "security",
			Description: "Alerts for unusual login patterns",
			SQL: fmt.Sprintf(`
				SELECT 
					COUNT(*) as alert_count
				FROM analytics_events
				WHERE category = 'security'
					AND data->>'pattern_type' = 'unusual_login'
					AND created_at > NOW() - INTERVAL '1 hour'
			`),
			CacheTTL:  60,
			CreatedAt: now,
			UpdatedAt: now,
		},
		"security_brute_force": {
			ID:          "security_brute_force",
			Name:        "Brute Force Attempts",
			Category:    "security",
			Description: "Brute force attack attempts",
			SQL: fmt.Sprintf(`
				SELECT 
					DATE_TRUNC('hour', created_at) as time_bucket,
					COUNT(*) as attempt_count
				FROM analytics_events
				WHERE event_type = 'brute_force_attempt'
					AND created_at > NOW() - INTERVAL '7 days'
				GROUP BY DATE_TRUNC('hour', created_at)
				ORDER BY time_bucket DESC
			`),
			CacheTTL:  300,
			CreatedAt: now,
			UpdatedAt: now,
		},
		"security_token_abuse": {
			ID:          "security_token_abuse",
			Name:        "Token Abuse Incidents",
			Category:    "security",
			Description: "Detected token abuse attempts",
			SQL: fmt.Sprintf(`
				SELECT 
					COUNT(*) as incident_count
				FROM analytics_events
				WHERE event_type = 'token_abuse_detected'
					AND created_at > NOW() - INTERVAL '1 day'
			`),
			CacheTTL:  300,
			CreatedAt: now,
			UpdatedAt: now,
		},
		"security_deprecated_protocols": {
			ID:          "security_deprecated_protocols",
			Name:        "Deprecated Protocol Usage",
			Category:    "security",
			Description: "Breakdown of deprecated protocol usage",
			SQL: fmt.Sprintf(`
				SELECT 
					COALESCE(data->>'protocol', 'Unknown') as protocol,
					COUNT(*) as usage_count
				FROM analytics_events
				WHERE event_type = 'deprecated_protocol_used'
					AND created_at > NOW() - INTERVAL '1 day'
				GROUP BY COALESCE(data->>'protocol', 'Unknown')
				ORDER BY usage_count DESC
			`),
			CacheTTL:  600,
			CreatedAt: now,
			UpdatedAt: now,
		},
		"security_failed_mfa": {
			ID:          "security_failed_mfa",
			Name:        "Failed MFA Attempts",
			Category:    "security",
			Description: "MFA challenge failures",
			SQL: fmt.Sprintf(`
				SELECT 
					DATE_TRUNC('hour', created_at) as time_bucket,
					COUNT(*) as failure_count
				FROM analytics_events
				WHERE event_type = 'mfa_verification_failed'
					AND created_at > NOW() - INTERVAL '7 days'
				GROUP BY DATE_TRUNC('hour', created_at)
				ORDER BY time_bucket DESC
			`),
			CacheTTL:  300,
			CreatedAt: now,
			UpdatedAt: now,
		},
		"security_recent_events": {
			ID:          "security_recent_events",
			Name:        "Recent Security Events",
			Category:    "security",
			Description: "Latest security events",
			SQL: fmt.Sprintf(`
				SELECT 
					id as event_id,
					event_type,
					user_id,
					created_at,
					data->>'severity' as severity
				FROM analytics_events
				WHERE category = 'security'
					AND created_at > NOW() - INTERVAL '1 day'
				ORDER BY created_at DESC
				LIMIT 20
			`),
			CacheTTL:  60,
			CreatedAt: now,
			UpdatedAt: now,
		},

		// System Health Dashboard Queries
		"health_api_latency": {
			ID:          "health_api_latency",
			Name:        "API Latency",
			Category:    "system_health",
			Description: "API response time percentiles",
			SQL: fmt.Sprintf(`
				SELECT 
					DATE_TRUNC('minute', created_at) as time_bucket,
					PERCENTILE_CONT(0.50) WITHIN GROUP (ORDER BY CAST(data->>'latency_ms' AS NUMERIC)) as p50,
					PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY CAST(data->>'latency_ms' AS NUMERIC)) as p95,
					PERCENTILE_CONT(0.99) WITHIN GROUP (ORDER BY CAST(data->>'latency_ms' AS NUMERIC)) as p99
				FROM analytics_events
				WHERE category = 'system_health'
					AND event_type = 'api_call'
					AND created_at > NOW() - INTERVAL '1 day'
				GROUP BY DATE_TRUNC('minute', created_at)
				ORDER BY time_bucket DESC
			`),
			CacheTTL:  120,
			CreatedAt: now,
			UpdatedAt: now,
		},
		"health_error_rate": {
			ID:          "health_error_rate",
			Name:        "Error Rate",
			Category:    "system_health",
			Description: "API error rate over time",
			SQL: fmt.Sprintf(`
				SELECT 
					DATE_TRUNC('hour', created_at) as time_bucket,
					ROUND(100.0 * COUNT(CASE WHEN CAST(data->>'status_code' AS INTEGER) >= 400 THEN 1 END) / COUNT(*), 2) as error_rate
				FROM analytics_events
				WHERE category = 'system_health'
					AND event_type = 'api_call'
					AND created_at > NOW() - INTERVAL '1 day'
				GROUP BY DATE_TRUNC('hour', created_at)
				ORDER BY time_bucket DESC
			`),
			CacheTTL:  120,
			CreatedAt: now,
			UpdatedAt: now,
		},
		"health_db_performance": {
			ID:          "health_db_performance",
			Name:        "Database Performance",
			Category:    "system_health",
			Description: "Database query performance",
			SQL: fmt.Sprintf(`
				SELECT 
					ROUND(AVG(CAST(data->>'query_time_ms' AS NUMERIC)), 2) as avg_query_time_ms,
					CAST(data->>'active_connections' AS INTEGER) as active_connections
				FROM analytics_events
				WHERE category = 'system_health'
					AND event_type = 'db_query'
					AND created_at > NOW() - INTERVAL '1 day'
			`),
			CacheTTL:  300,
			CreatedAt: now,
			UpdatedAt: now,
		},
		"health_storage_occupancy": {
			ID:          "health_storage_occupancy",
			Name:        "Storage Occupancy",
			Category:    "system_health",
			Description: "Storage usage per tier",
			SQL: fmt.Sprintf(`
				SELECT 
					CAST(data->>'hot_tier_usage_percent' AS NUMERIC) as hot_usage,
					CAST(data->>'warm_tier_usage_percent' AS NUMERIC) as warm_usage,
					CAST(data->>'cold_tier_usage_percent' AS NUMERIC) as cold_usage
				FROM analytics_events
				WHERE category = 'system_health'
					AND event_type = 'storage_status'
					AND created_at = (
						SELECT MAX(created_at) FROM analytics_events 
						WHERE category = 'system_health' AND event_type = 'storage_status'
					)
				LIMIT 1
			`),
			CacheTTL:  300,
			CreatedAt: now,
			UpdatedAt: now,
		},
		"health_sync_lag": {
			ID:          "health_sync_lag",
			Name:        "Sync Lag",
			Category:    "system_health",
			Description: "Current data sync lag",
			SQL: fmt.Sprintf(`
				SELECT 
					CAST(data->>'lag_seconds' AS NUMERIC) as lag_seconds
				FROM analytics_events
				WHERE category = 'system_health'
					AND event_type = 'sync_status'
					AND created_at > NOW() - INTERVAL '1 hour'
				ORDER BY created_at DESC
				LIMIT 1
			`),
			CacheTTL:  60,
			CreatedAt: now,
			UpdatedAt: now,
		},
		"health_webhook_success": {
			ID:          "health_webhook_success",
			Name:        "Webhook Success Rate",
			Category:    "system_health",
			Description: "Webhook delivery success rate",
			SQL: fmt.Sprintf(`
				SELECT 
					ROUND(100.0 * COUNT(CASE WHEN data->>'delivery_status' = 'success' THEN 1 END) / COUNT(*), 2) as success_rate
				FROM analytics_events
				WHERE event_type = 'webhook_delivery'
					AND created_at > NOW() - INTERVAL '1 day'
			`),
			CacheTTL:  300,
			CreatedAt: now,
			UpdatedAt: now,
		},
		"health_cache_hit_ratio": {
			ID:          "health_cache_hit_ratio",
			Name:        "Cache Hit Ratio",
			Category:    "system_health",
			Description: "Query cache hit ratio",
			SQL: fmt.Sprintf(`
				SELECT 
					ROUND(100.0 * COUNT(CASE WHEN data->>'cache_hit' = 'true' THEN 1 END) / COUNT(*), 2) as hit_ratio
				FROM analytics_events
				WHERE category = 'system_health'
					AND event_type = 'query_executed'
					AND created_at > NOW() - INTERVAL '1 day'
			`),
			CacheTTL:  120,
			CreatedAt: now,
			UpdatedAt: now,
		},
		"health_memory_usage": {
			ID:          "health_memory_usage",
			Name:        "Memory Usage",
			Category:    "system_health",
			Description: "DuckDB memory consumption",
			SQL: fmt.Sprintf(`
				SELECT 
					CAST(data->>'memory_mb' AS NUMERIC) as memory_mb
				FROM analytics_events
				WHERE category = 'system_health'
					AND event_type = 'memory_status'
					AND created_at > NOW() - INTERVAL '1 hour'
				ORDER BY created_at DESC
				LIMIT 1
			`),
			CacheTTL:  60,
			CreatedAt: now,
			UpdatedAt: now,
		},
		"health_query_throughput": {
			ID:          "health_query_throughput",
			Name:        "Query Throughput",
			Category:    "system_health",
			Description: "Query count per second",
			SQL: fmt.Sprintf(`
				SELECT 
					DATE_TRUNC('minute', created_at) as time_bucket,
					ROUND(COUNT(*) / 60.0, 2) as queries_per_sec
				FROM analytics_events
				WHERE category = 'system_health'
					AND event_type = 'query_executed'
					AND created_at > NOW() - INTERVAL '1 day'
				GROUP BY DATE_TRUNC('minute', created_at)
				ORDER BY time_bucket DESC
			`),
			CacheTTL:  120,
			CreatedAt: now,
			UpdatedAt: now,
		},
		"health_slowest_queries": {
			ID:          "health_slowest_queries",
			Name:        "Slowest Queries",
			Category:    "system_health",
			Description: "Top slowest queries",
			SQL: fmt.Sprintf(`
				SELECT 
					data->>'query' as query,
					CAST(data->>'duration_ms' AS NUMERIC) as duration_ms,
					COUNT(*) as execution_count
				FROM analytics_events
				WHERE category = 'system_health'
					AND event_type = 'query_executed'
					AND created_at > NOW() - INTERVAL '1 day'
				GROUP BY data->>'query', CAST(data->>'duration_ms' AS NUMERIC)
				ORDER BY duration_ms DESC
				LIMIT 10
			`),
			CacheTTL:  300,
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
}
