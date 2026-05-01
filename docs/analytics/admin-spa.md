# Admin SPA Analytics Guide

**Version:** 1.0  
**Last Updated:** 2026-04-24  
**Module:** `modules/admin/spa/src/components/Analytics`

## Overview

The Admin SPA provides a comprehensive web-based interface for managing the Aegion analytics system. This guide covers all available features and how to use them.

**Access:** `http://localhost:3000/admin/analytics`

---

## Dashboard

### Dashboard Overview

The main dashboard displays:
- System health status
- Active events per minute
- Top event categories
- Recent queries
- Webhook delivery status

### Create Dashboard

**Steps:**
1. Navigate to **Dashboards** → **+ New Dashboard**
2. Enter dashboard name and description
3. Click **Create**
4. Use drag-drop interface to add widgets

### Add Widgets

**Widget Types:**
- **Line Chart** - Time-series data
- **Bar Chart** - Category comparisons
- **Pie Chart** - Distribution
- **Gauge** - Single metric
- **Table** - Raw data display
- **Map** - Geographic distribution
- **Heatmap** - Density visualization

**To add widget:**
1. Click **+ Add Widget**
2. Select widget type
3. Choose query or metric
4. Configure widget settings
5. Click **Save**

### Widget Configuration

**Example: Line Chart Configuration**

```json
{
  "type": "line_chart",
  "query": "q_daily_events",
  "settings": {
    "title": "Daily Events",
    "xAxis": "date",
    "yAxis": "count",
    "timeRange": "7d",
    "groupBy": "category",
    "colors": ["#1f77b4", "#ff7f0e"],
    "showLegend": true,
    "showGrid": true,
    "smooth": true
  }
}
```

### Save & Share Dashboard

**Save:**
- Changes auto-save every 10 seconds
- Manual save: **Ctrl+S** or click **Save**

**Share:**
1. Click **Share** button
2. Select **Public** or **Internal**
3. Copy share link
4. Send to colleagues

**Pin:**
- Pin frequently used dashboards to sidebar
- Click **Pin** icon in dashboard header

---

## Event Viewer

### Real-Time Event Stream

Live view of all events as they occur:

**Features:**
- Real-time updates (WebSocket)
- Search by category, event type, user
- Filter by time range
- Export to CSV/JSON

### Event Details

Click any event to see full details:
- **Event ID** - Unique identifier
- **Category** - Classification
- **Event Type** - Specific action
- **User** - Associated user
- **Session** - Session ID
- **Data** - Event-specific fields
- **Timestamp** - When it occurred
- **Metadata** - IP, user agent, location

### Search Events

```
Search syntax:
  category:user_action           # Filter by category
  eventType:page_view            # Filter by event type
  userId:user_123                # Filter by user
  "purchase"                     # Full-text search
  after:2026-04-20               # Date filter
  error                          # Case-insensitive search
```

### Export Events

**Export Formats:**
- CSV - Spreadsheet compatible
- JSON - Full data structure
- Parquet - Analytics-optimized
- Excel - Microsoft format

**Export Process:**
1. Click **⋮** menu (three dots)
2. Select **Export**
3. Choose format and date range
4. Download file

---

## Dashboard Builder

### Query Integration

Link dashboards to saved queries:

**Create Query:**
1. Navigate to **Queries**
2. Click **+ New Query**
3. Write SQL or use query builder
4. Set parameters (optional)
5. Click **Save**

**Add to Dashboard:**
1. Open dashboard
2. Add **Table** widget
3. Select query
4. Configure columns to display
5. Set pagination/sorting

### Query Parameters

Create parameterized queries for flexibility:

```sql
SELECT *
FROM events
WHERE category = :category
  AND createdAt > :startDate
  AND createdAt < :endDate
LIMIT :limit
```

**Parameter Configuration:**
- **Name** - `:paramName`
- **Type** - STRING, INT, DATETIME, BOOLEAN
- **Default** - Optional default value
- **Required** - Mark as required/optional

### Query Execution

**Execute Query:**
1. Open query
2. Set parameter values
3. Click **Execute**
4. View results
5. Export or add to dashboard

**Query Performance:**
- Display execution time
- Show row count
- Indicate if cached
- Plan query time vs execution time

---

## Webhook Manager

### View Webhooks

**Webhooks Table:**
- Webhook ID
- Target URL
- Status (active/inactive)
- Event count
- Last delivery
- Success rate

### Create Webhook

**Steps:**
1. Navigate to **Webhooks** → **+ New Webhook**
2. Enter webhook URL
3. Select events to subscribe
4. Configure filters (optional)
5. Add custom headers (optional)
6. Set retry policy
7. Click **Create**

**Example Configuration:**
```json
{
  "url": "https://webhook.example.com/analytics",
  "events": ["event.created", "event.updated"],
  "filters": {
    "categories": ["user_action"],
    "minSeverity": "WARN"
  },
  "retryPolicy": {
    "maxRetries": 3,
    "backoffMultiplier": 2.0
  },
  "headers": [
    {
      "name": "Authorization",
      "value": "Bearer secret-token"
    }
  ]
}
```

### Test Webhook

**Manual Test:**
1. Click webhook
2. Click **Send Test**
3. View response
4. Check **Delivery History**

### Monitor Deliveries

**Delivery History Table:**
- Delivery ID
- Status (success/failed)
- HTTP status code
- Attempt number
- Sent time
- Response time

**Troubleshooting:**
- Click failed delivery to see error
- View response body
- Check retry schedule
- Inspect raw webhook payload

### Dead Letter Queue (DLQ)

Failed webhooks after max retries appear in DLQ:

**DLQ Features:**
- View failed events
- Inspect failure reason
- Manually replay delivery
- Export DLQ for analysis

---

## Configuration Panel

### General Settings

**Basic Configuration:**
- Enable/disable analytics
- Set query timeout (seconds)
- Configure rate limits
- Set default page size

### DuckDB Settings

```yaml
duckdb:
  threads: 8
  memory_limit_gb: 8
  connection_pool_size: 50
  enable_parallel: true
```

### Sync Configuration

**Real-Time:**
- Enable/disable CDC
- Batch size
- Max latency

**Batch:**
- Schedule (cron)
- Batch size
- Parallel workers

**Async:**
- Queue depth
- Worker count

### Storage Settings

**Configure Storage:**
- Storage type (local, S3, Iceberg, K8s)
- Storage-specific settings
- Tier management (hot/warm/cold)
- TTL settings

### API Configuration

**REST API:**
- Enable/disable
- Endpoint path
- CORS settings
- Rate limits

**GraphQL:**
- Enable/disable
- Query depth limit
- Query complexity limit

**gRPC:**
- Enable/disable
- Port number
- mTLS settings

### Security Settings

**Authentication:**
- JWT secret (change regularly)
- Token expiry
- Refresh token expiry

**Authorization:**
- Default role assignments
- Permission matrix
- Resource ownership rules

**Encryption:**
- Enable at-rest encryption
- Configure key management
- Specify encrypted fields

---

## Metrics & Monitoring

### System Metrics Dashboard

Displays:
- **Events Processed** - Total events today
- **Query Performance** - P50, P95, P99 latencies
- **API Throughput** - Requests per second
- **Webhook Deliveries** - Success rate
- **Storage Usage** - Hot/warm/cold breakdown
- **Sync Lag** - Real-time, batch, async latency

### Health Status

**Component Status:**
- PostgreSQL - Connection status
- DuckDB - Query engine status
- Sync Layer - Running/paused/failed
- Storage - Backend connectivity
- API Services - Response time
- Webhooks - Queue status

**Health Indicators:**
- 🟢 Healthy - All systems operational
- 🟡 Degraded - Some issues, still functional
- 🔴 Unhealthy - Critical issues

### Performance Charts

**Available Charts:**
- Query latency over time
- Concurrent user count
- Cache hit rate
- Error rate
- Webhook delivery success rate
- Storage tier distribution

---

## Query Editor

### Interactive Query Builder

**Features:**
- Visual query builder (no-code)
- SQL editor with syntax highlighting
- Query validation
- Auto-complete table/column names
- Query history

### Write Custom Queries

**Example Queries:**

```sql
-- Top 10 users by event count
SELECT userId, COUNT(*) as count
FROM events
GROUP BY userId
ORDER BY count DESC
LIMIT 10;
```

```sql
-- Daily event distribution
SELECT DATE(createdAt) as date, category, COUNT(*) as count
FROM events
GROUP BY DATE(createdAt), category
ORDER BY date DESC;
```

```sql
-- User journey analysis
SELECT userId, COUNT(DISTINCT sessionId) as sessions,
       COUNT(*) as events,
       MAX(createdAt) as last_active
FROM events
WHERE category = 'user_action'
GROUP BY userId
ORDER BY last_active DESC;
```

### Query Performance Analysis

**EXPLAIN Plan:**
- Shows execution strategy
- Identifies full table scans
- Suggests missing indexes
- Estimates execution time

**Query Execution:**
- Actual execution time
- Rows scanned vs returned
- Memory usage
- Cache effectiveness

---

## Admin Functions

### User Management

**Manage Analytics Users:**
- View active sessions
- Revoke tokens
- Reset passwords
- Assign roles (admin, analyst, viewer)

### Audit Log

**View Audit Trail:**
- User actions
- Config changes
- Query execution history
- Webhook deliveries
- Failed authentication attempts

**Export Audit Log:**
- CSV/JSON export
- Date range selection
- Filter by user/action

### Backup & Restore

**Backup:**
- Full database backup
- Schema-only backup
- Export to S3
- Scheduled backups

**Restore:**
- Point-in-time restore
- Restore from S3
- Verify restore integrity

### System Health

**Check System:**
- Database connectivity
- Disk space
- Memory usage
- CPU utilization
- Network connectivity

---

## Keyboard Shortcuts

| Shortcut | Action |
|----------|--------|
| `Ctrl+S` | Save current dashboard |
| `Ctrl+/` | Toggle sidebar |
| `Ctrl+K` | Command palette |
| `Esc` | Close modal/dialog |
| `?` | Show help |

---

## Troubleshooting SPA Issues

### SPA Won't Load

1. Check browser console for errors (F12)
2. Verify authentication token
3. Check CORS settings
4. Try clearing browser cache

### Slow Dashboard

1. Check query performance
2. Reduce dashboard widget count
3. Increase DuckDB threads
4. Check network latency

### Widget Not Updating

1. Verify query is still valid
2. Check event filters
3. Refresh browser page
4. Check real-time sync status

---

## Related Documentation

- [API Reference](./api.md)
- [Security](./security.md)
- [Integration Guide](./integration.md)
- [Troubleshooting](./troubleshooting.md)
