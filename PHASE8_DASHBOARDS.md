# Phase 8 - Pre-built Dashboards Implementation

## Overview

Phase 8 implements comprehensive pre-built analytics dashboards with real-time data updates, advanced visualization components, and full CRUD capabilities. The implementation provides 5 pre-built dashboards covering authentication, user activity, sessions, security, and system health.

## Module Structure

```
modules/analytics/dashboards/
├── models.go              - Data structures (Dashboard, Component, Query)
├── defaults.go            - Pre-built dashboard definitions (5 dashboards + 50+ queries)
├── manager.go             - Dashboard CRUD, query execution, sharing
├── builder.go             - Fluent builders for dashboards and components
├── queries.go             - Query builder utilities and helpers
└── migrations/
    ├── 0005_dashboards.up.sql     - Schema creation
    └── 0005_dashboards.down.sql   - Schema rollback
```

## 5 Pre-built Dashboards

### 1. Authentication Dashboard
**Category**: `authentication`
**Refresh Interval**: 30 seconds
**Components**: 10

**Metrics**:
- Login success rate (time series)
- Failed auth attempts by reason (pie chart)
- MFA adoption rate (gauge)
- Peak login times (heatmap)
- Geographic login distribution (map)
- Session distribution by device type (pie)
- Top 10 failing users (table)
- Auth errors trend (time series)
- Average login time (gauge)
- Suspicious activity alerts (banner)

**Key Queries**:
- `auth_login_success_rate` - Success rate time series
- `auth_failed_reasons` - Failure distribution
- `auth_mfa_adoption` - MFA adoption percentage
- `auth_peak_login_times` - Peak times heatmap
- `auth_geographic_distribution` - Geographic heatmap
- `auth_suspicious_activity` - Real-time alerts

### 2. User Activity Dashboard
**Category**: `user_activity`
**Refresh Interval**: 30 seconds
**Components**: 10

**Metrics**:
- New user signups (time series)
- Active users DAU/MAU/WAU (gauge)
- User lifecycle funnel (funnel chart)
- Most active users (leaderboard)
- Activity distribution (histogram)
- Churn rate (time series)
- Account deletion trends (time series)
- User role distribution (pie chart)
- Regional user distribution (map)
- Retention cohorts (table)

**Key Queries**:
- `activity_new_signups` - Daily signups
- `activity_active_users` - DAU/MAU/WAU metrics
- `activity_user_lifecycle` - Signup to retention funnel
- `activity_churn_rate` - Churn percentage trend
- `activity_retention_cohorts` - Cohort retention analysis

### 3. Session Analytics Dashboard
**Category**: `sessions`
**Refresh Interval**: 30 seconds
**Components**: 10

**Metrics**:
- Current active sessions (gauge)
- Session duration distribution (histogram)
- Concurrent users peak (time series)
- Session timeout patterns (time series)
- Device type distribution (pie)
- Browser/OS breakdown (table)
- Geographic session distribution (map)
- Session lifecycle status (pie)
- Avg duration by user segment (bar chart)
- Top user sessions (table)

**Key Queries**:
- `session_current_active` - Real-time active count
- `session_duration_distribution` - Session length distribution
- `session_concurrent_peak` - Peak concurrent users
- `session_timeout_patterns` - Timeout trends
- `session_browser_os` - Device breakdowns

### 4. Security Dashboard
**Category**: `security`
**Refresh Interval**: 15 seconds (higher frequency for security)
**Components**: 10

**Metrics**:
- Suspicious activities (timeline)
- Rate limit violations (counter)
- Policy violation attempts (time series)
- Geographic anomalies (map + table)
- Unusual login patterns (alerts)
- Brute force attempts (trending)
- Token abuse incidents (counter)
- Deprecated protocol usage (pie)
- Failed MFA attempts (time series)
- Recent security events (table)

**Key Queries**:
- `security_suspicious_activities` - Timeline of suspicious events
- `security_rate_limit_violations` - RateLimit violations
- `security_brute_force` - Brute force attempts
- `security_geographic_anomalies` - Unusual locations
- `security_failed_mfa` - MFA failures

### 5. System Health Dashboard
**Category**: `system_health`
**Refresh Interval**: 30 seconds
**Components**: 10

**Metrics**:
- API latency percentiles (p50, p95, p99)
- Error rate (time series)
- Database performance (query time, connections)
- Storage tier occupancy (gauges)
- Sync lag (real-time gauge)
- Webhook delivery success rate (gauge)
- Cache hit ratio (gauge)
- Memory usage (DuckDB) (gauge)
- Query count/sec (time series)
- Slowest queries (table)

**Key Queries**:
- `health_api_latency` - Response time percentiles
- `health_error_rate` - Error rate trend
- `health_db_performance` - Query performance metrics
- `health_storage_occupancy` - Storage usage per tier
- `health_sync_lag` - Replication lag

## Component Types

| Type | Purpose | Best For |
|------|---------|----------|
| `time_series` | Line/area charts | Trends over time |
| `pie_chart` | Distribution | Composition breakdown |
| `gauge` | Single value display | Current status (KPI) |
| `table` | Data grid | Detailed records |
| `heatmap` | 2D distribution | Patterns (day/hour) |
| `histogram` | Range distribution | Activity distribution |
| `leaderboard` | Ranked list | Top performers |
| `map` | Geographic data | Regional distribution |
| `alert_banner` | Alert display | Real-time warnings |
| `counter` | Large number display | Total count |
| `bar_chart` | Comparison | Category comparison |
| `funnel` | Conversion funnel | User journey |
| `timeline` | Event sequence | Chronological events |

## Database Schema

### Tables

1. **analytics_dashboards**
   - `id` - UUID primary key
   - `name` - Dashboard name (unique)
   - `description` - Dashboard description
   - `config` - JSON config (layout, components, etc.)
   - `owner_id` - Owner UUID (null for default dashboards)
   - `public` - Public access flag
   - `pinned` - Pinned to home flag
   - `created_at` - Creation timestamp
   - `updated_at` - Last update timestamp

2. **analytics_dashboard_shares**
   - `id` - UUID primary key
   - `dashboard_id` - Reference to dashboard
   - `token` - Unique share token
   - `expires_at` - Optional expiration
   - `read_only` - Read-only access flag
   - `created_at`, `updated_at` - Timestamps

3. **analytics_dashboard_metrics**
   - `id` - UUID primary key
   - `dashboard_id` - Dashboard reference
   - `metric_name` - Metric name
   - `last_computed` - Last computation time
   - `next_compute` - Next scheduled computation
   - `compute_status` - Status (pending/running/completed/failed)
   - `error_message` - Optional error message

4. **analytics_dashboard_alerts**
   - `id` - UUID primary key
   - `dashboard_id` - Dashboard reference
   - `metric_name` - Metric name
   - `operator` - Comparison operator (gt, lt, eq, gte, lte)
   - `threshold` - Alert threshold value
   - `severity_level` - Severity (info/warning/critical)
   - `enabled` - Alert enabled flag

5. **analytics_dashboard_query_cache**
   - `id` - UUID primary key
   - `query_id` - Query identifier
   - `result` - JSON query result
   - `execution_time_ms` - Query duration
   - `expires_at` - Cache expiration time

6. **analytics_dashboard_access_logs**
   - `id` - UUID primary key
   - `dashboard_id` - Dashboard reference
   - `user_id` - User UUID
   - `action` - Action type (view/edit/share/export)
   - `details` - JSON details
   - `created_at` - Timestamp

## API Endpoints (REST)

### Dashboard Management
```
GET    /dashboards              - List user dashboards
POST   /dashboards              - Create custom dashboard
GET    /dashboards/{id}         - Get dashboard by ID
PUT    /dashboards/{id}         - Update dashboard
DELETE /dashboards/{id}         - Delete dashboard
GET    /dashboards/defaults     - List default dashboards
```

### Dashboard Sharing
```
POST   /dashboards/{id}/share   - Create share link
GET    /dashboards/share/{token} - Access shared dashboard
DELETE /dashboards/{id}/shares/{shareId} - Revoke share
```

### Query Execution
```
POST   /dashboards/{id}/query   - Execute dashboard query
GET    /dashboards/{id}/export  - Export dashboard data
```

### Alerts
```
POST   /dashboards/{id}/alerts  - Create alert threshold
GET    /dashboards/{id}/alerts  - List alerts
PUT    /dashboards/{id}/alerts/{alertId} - Update alert
DELETE /dashboards/{id}/alerts/{alertId} - Delete alert
```

## GraphQL Subscriptions (Real-time)

```graphql
type Subscription {
  # Subscribe to dashboard data updates
  onDashboardUpdate(dashboardId: ID!): DashboardUpdate!
  
  # Subscribe to alert triggers
  onAlertTriggered(dashboardId: ID!): Alert!
  
  # Subscribe to metric changes
  onMetricChange(queryId: ID!): MetricValue!
}
```

## Configuration (aegion.yaml)

```yaml
modules:
  analytics:
    dashboards:
      # Auto-refresh interval in seconds (default: 30)
      auto_refresh_interval_seconds: 30
      
      # Default time range in days (default: 7)
      default_time_range_days: 7
      
      # Maximum custom dashboards per user (default: 50)
      max_custom_dashboards: 50
      
      # Enable dashboard sharing (default: true)
      enable_sharing: true
      
      # Enable scheduled reports (default: true)
      enable_scheduled_reports: true
      
      # Query cache TTL in seconds (default: 300)
      query_cache_ttl_seconds: 300
      
      # Maximum query result rows (default: 10000)
      max_result_rows: 10000
```

## Usage Examples

### Creating a Custom Dashboard

```go
import "github.com/aegion/modules/analytics/dashboards"

manager := dashboards.NewManager(db, logger, config)

// Create dashboard using builder
dashboard := dashboards.NewBuilder("my-dashboard").
  Name("My Custom Dashboard").
  Description("Performance metrics for my app").
  Category("custom").
  Layout("grid-3col").
  RefreshInterval(60).
  AddTimeSeriesComponent("requests", "Request Rate", "health_api_latency", []string{"p50", "p95", "p99"}).
  AddGaugeComponent("errors", "Error Rate", "health_error_rate", "error_rate", 0, 100).
  Build()

created, err := manager.CreateDashboard(ctx, dashboard)
```

### Executing a Dashboard Query

```go
// Get pre-built query
queries := dashboards.PrebuiltQueries()
query := queries["auth_login_success_rate"]

// Execute with caching
result, err := manager.ExecuteQuery(ctx, "auth_login_success_rate", query)
if err != nil {
  log.Fatal(err)
}

// Result contains:
// - Data: []map[string]interface{} - Query results
// - RowCount: int - Number of rows
// - ExecutionTime: int - Milliseconds
// - FromCache: bool - Whether result was cached
```

### Sharing a Dashboard

```go
// Create share link (expires in 30 days, read-only)
duration := 30 * 24 * time.Hour
share, err := manager.CreateShare(ctx, dashboardID, &duration, true)

// Share URL: /dashboards/share/{{share.Token}}
```

### Setting Alert Thresholds

```go
alert := &dashboards.AlertThreshold{
  DashboardID:  dashboardID,
  MetricName:   "error_rate",
  Operator:     "gt",
  Threshold:    5.0,
  SeverityLevel: "critical",
  Enabled:       true,
}

saved, err := manager.SaveAlert(ctx, alert)
```

## Query Library (50+ Queries)

All pre-built queries are optimized for performance:

- **Authentication** (10 queries): Login metrics, MFA adoption, failure analysis
- **User Activity** (10 queries): Signups, retention, churn, lifecycle
- **Sessions** (10 queries): Duration, concurrency, device breakdown
- **Security** (10 queries): Suspicious activity, brute force, policy violations
- **System Health** (10 queries): Latency, errors, database, storage, sync

Each query includes:
- Optimized SQL with appropriate indexes
- Parameterized for date ranges and filters
- Caching configuration (CacheTTL in seconds)
- Category and description

## Real-time Updates

### Webhook Integration
Dashboards trigger webhooks on:
- Alert threshold breached
- Critical security event
- System health degradation
- Data anomaly detected

### GraphQL Subscriptions
Real-time updates via WebSocket for:
- Active dashboard updates (30s intervals)
- Alert triggers (immediate)
- Metric changes (configurable)

### Fallback Strategy
- Primary: GraphQL subscriptions (WebSocket)
- Secondary: Webhook callbacks
- Tertiary: REST polling (client-side)

## Performance Optimization

### Caching Strategy
- Query results cached based on CacheTTL (60-600 seconds)
- Dashboard metadata cached (10 minutes)
- Component definitions cached (1 hour)

### Query Optimization
- Appropriate indexes on foreign keys and timestamps
- Partitioned events table (by month)
- Pre-aggregated metrics table
- Selective column selection

### Incremental Updates
- Only fetch data since last refresh
- Delta compression for large result sets
- Streaming results for large exports

## Testing Checklist

- [x] All 5 dashboards compile and load
- [x] Pre-built queries execute correctly
- [x] Date range filtering works
- [x] Dashboard CRUD operations work
- [x] Query caching functions
- [x] Share token generation works
- [x] Alert thresholds save/update
- [x] Export functionality available
- [x] Real-time subscriptions available
- [x] Code follows project patterns

## Key Highlights

1. **Production Ready**: All dashboards tested and optimized
2. **Scalable**: Handles large datasets with pagination
3. **Secure**: User-scoped dashboards with share tokens
4. **Flexible**: Builder pattern for custom dashboards
5. **Fast**: Query caching and optimized indexes
6. **Real-time**: GraphQL subscriptions + webhooks
7. **Comprehensive**: 50+ pre-built queries
8. **Well-documented**: Complete API reference

## Next Steps

The dashboard system is ready for:
- Frontend integration (React/Vue components)
- Scheduled report generation (emails)
- Advanced anomaly detection
- Machine learning insights
- Custom alert actions (webhooks, emails, Slack)
- Dashboard versioning and rollback
