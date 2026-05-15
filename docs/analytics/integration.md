# Analytics Integration Guide

**Version:** 1.0  
**Last Updated:** 2026-04-24  
**Module:** `modules/analytics`

## Overview

This guide explains how to integrate the Aegion analytics module with the Aegion core system and external applications.

---

## Event Ingestion

### Aegion Core Event Sources

The analytics system automatically captures events from Aegion core:

```go
// Core events that trigger analytics ingestion
type CoreEvent struct {
  Category    string                 // user_action, system_event, error_event
  EventType   string                 // page_view, click, login, etc.
  UserID      string                 // Authenticated user
  SessionID   string                 // Session identifier
  Timestamp   time.Time
  Data        map[string]interface{} // Custom data
}
```

### Mapping Aegion Events to Analytics

**User Actions:**
```
core.event.user.login           → analytics.user_action.login
core.event.user.logout          → analytics.user_action.logout
core.event.user.update_profile  → analytics.user_action.update_profile
```

**System Events:**
```
core.event.system.startup       → analytics.system_event.startup
core.event.system.config_change → analytics.system_event.config_change
core.event.system.backup        → analytics.system_event.backup
```

**API Events:**
```
core.event.api.request          → analytics.api_event.request
core.event.api.error            → analytics.api_event.error
```

### Custom Event Ingestion

Ingest custom events via REST API:

```bash
curl -X POST \
  -H "Authorization: Bearer ${AEGION_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{
    "category": "custom_event",
    "eventType": "user_conversion",
    "userId": "user_123",
    "sessionId": "sess_456",
    "data": {
      "conversionType": "premium_signup",
      "planTier": "professional",
      "amount": 9900
    }
  }' \
  http://localhost:8080/api/v1/analytics/events
```

---

## Data Synchronization

### PostgreSQL → DuckDB Sync

The analytics module automatically syncs data from PostgreSQL to DuckDB using three strategies:

#### 1. Real-Time Sync (CDC)

Captures changes immediately via Change Data Capture:

```yaml
analytics:
  sync:
    enable_real_time: true
    real_time:
      batch_size: 100
      max_latency_ms: 1000
```

**Setup:**
```sql
-- Enable logical decoding in PostgreSQL
ALTER SYSTEM SET wal_level = logical;
SELECT pg_reload_conf();

-- Verify
SHOW wal_level;  -- Should be 'logical'
```

#### 2. Batch Sync (Scheduled)

Periodic full or incremental sync:

```yaml
analytics:
  sync:
    enable_batch: true
    batch:
      schedule: "*/5 * * * *"      # Every 5 minutes
      batch_size: 10000
      parallel_workers: 4
```

#### 3. Async Sync (Queue-Based)

Background processing for complex transformations:

```yaml
analytics:
  sync:
    enable_async: true
    async:
      queue_depth: 100000
      worker_count: 8
      retry_policy: exponential
```

### Sync Health Monitoring

```bash
# Check sync status
curl -H "Authorization: Bearer token" \
  http://localhost:8080/api/v1/analytics/stats

Response:
{
  "sync": {
    "realTime": {
      "status": "running",
      "lag_ms": 50,
      "events_processed": 15234
    },
    "batch": {
      "status": "scheduled",
      "last_run_at": "2026-04-24T10:25:00Z",
      "lag_ms": 120000
    },
    "async": {
      "status": "running",
      "queue_size": 1234,
      "workers_active": 8
    }
  }
}
```

---

## API Integration

### REST API Integration

**Example: Dashboard Integration**

```javascript
// Initialize analytics client
const analyticsAPI = {
  baseURL: 'http://localhost:8080/api/v1/analytics',
  token: 'your-bearer-token',
  
  headers() {
    return {
      'Authorization': `Bearer ${this.token}`,
      'Content-Type': 'application/json'
    };
  },
  
  async getDashboard(dashboardId) {
    const response = await fetch(
      `${this.baseURL}/dashboards/${dashboardId}`,
      { headers: this.headers() }
    );
    return response.json();
  },
  
  async getEvents(filters) {
    const response = await fetch(
      `${this.baseURL}/events?category=${filters.category}`,
      { headers: this.headers() }
    );
    return response.json();
  }
};

// Usage
const dashboard = await analyticsAPI.getDashboard('dash_123');
const events = await analyticsAPI.getEvents({ category: 'user_action' });
```

### GraphQL Integration

**Example: Real-time Subscriptions**

```javascript
import { gql, ApolloClient } from '@apollo/client';

const client = new ApolloClient({
  uri: 'http://localhost:8080/api/v1/analytics/graphql',
  headers: {
    'Authorization': 'Bearer your-token'
  }
});

// Subscribe to new events
client.subscribe({
  query: gql`
    subscription OnNewEvent {
      onNewEvent(filter: { category: "user_action" }) {
        id
        category
        eventType
        userId
        createdAt
      }
    }
  `
}).subscribe({
  next: (event) => {
    console.log('New event:', event);
    updateDashboard(event);
  }
});
```

### gRPC Integration

**Example: High-Performance Service-to-Service**

```go
import (
  "context"
  pb "github.com/neerajcodz/aegion/pkg/proto/analytics"
  "google.golang.org/grpc"
)

// Connect to analytics gRPC service
conn, err := grpc.Dial("localhost:50051")
defer conn.Close()

client := pb.NewAnalyticsClient(conn)

// Get events
resp, err := client.GetEvents(context.Background(), &pb.GetEventsRequest{
  Limit: 100,
  Filter: &pb.EventFilter{
    Category: "user_action",
  },
})

if err != nil {
  log.Fatal(err)
}

for _, event := range resp.Events {
  processEvent(event)
}
```

---

## Admin SPA Integration

### Building Custom Analytics Pages

The Admin SPA includes an analytics module at: `modules/admin/spa/src/components/Analytics`

**Available Components:**
```
├── EventViewer.tsx        # Real-time event viewer
├── DashboardBuilder.tsx   # Drag-drop dashboard editor
├── QueryEditor.tsx        # SQL/GraphQL query editor
├── WebhookManager.tsx     # Webhook configuration
├── MetricsPanel.tsx       # System metrics display
└── ConfigPanel.tsx        # Configuration management
```

**Usage Example:**

```typescript
import { DashboardBuilder } from '@/components/Analytics';

export function MyAnalyticsPage() {
  return (
    <DashboardBuilder
      onSave={(config) => saveDashboard(config)}
      readOnly={false}
    />
  );
}
```

---

## Webhook Integration with External Systems

### Slack Integration

**Setup webhook in Admin SPA:**
1. Navigate to **Webhooks**
2. Create webhook with URL: `https://hooks.slack.com/services/YOUR/WEBHOOK/URL`
3. Add filter: `categories: ["error"]`
4. Add custom headers: `Content-Type: application/json`

**Message transformation:**
```javascript
// Transform analytics event to Slack format
function transformToSlack(analyticsEvent) {
  return {
    text: `🚨 Error: ${analyticsEvent.data.errorType}`,
    blocks: [
      {
        type: "section",
        text: {
          type: "mrkdwn",
          text: `*Category:* ${analyticsEvent.category}\n*Error:* ${analyticsEvent.data.message}`
        }
      }
    ]
  };
}
```

### PagerDuty Integration

**Setup webhook:**
```bash
curl -X POST http://localhost:8080/api/v1/analytics/webhooks \
  -d '{
    "url": "https://events.pagerduty.com/v2/enqueue",
    "events": ["event.created"],
    "filters": { "minSeverity": "ERROR" },
    "headers": [{
      "name": "Authorization",
      "value": "Token token=YOUR_PAGERDUTY_TOKEN"
    }]
  }'
```

### Custom Data Lake Integration

**Export events to S3 daily:**
```yaml
analytics:
  webhooks:
    - name: daily-export
      events: ["event.created"]
      schedule: "0 0 * * *"  # Daily at midnight
      export:
        format: parquet
        destination: "s3://my-datalake/events/"
        partitioning: "date=YYYY-MM-DD"
```

---

## Configuration Management

### Aegion YAML Configuration

**Analytics section in `aegion.yaml`:**

```yaml
analytics:
  enabled: true
  
  # Event sources
  event_sources:
    - type: aegion_core
      enabled: true
      categories:
        - user_action
        - system_event
        - error_event
  
  # Sync strategy
  sync:
    enable_real_time: true
    enable_batch: true
    enable_async: false
    batch_schedule: "*/5 * * * *"
  
  # Storage
  storage:
    type: local
    hot_ttl_hours: 24
    base_path: "./data/analytics"
  
  # APIs
  rest:
    enabled: true
    endpoint: /api/v1/analytics
    
  graphql:
    enabled: true
    
  grpc:
    enabled: true
    port: 50051
```

### Runtime Configuration Updates

Update config without restart:

```bash
curl -X POST \
  -H "Authorization: Bearer ${AEGION_ADMIN_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{
    "sync": {
      "enable_real_time": false,
      "batch_schedule": "*/10 * * * *"
    }
  }' \
  http://localhost:8080/api/v1/analytics/config
```

---

## Data Retention & Compliance

### Setting Retention Policies

```yaml
analytics:
  retention:
    policies:
      # User events: 30 days
      - category: user_action
        ttl_days: 30
        
      # Audit logs: 7 years (compliance)
      - category: system_event
        type: audit
        ttl_days: 2555
        
      # PII-containing events: 7 days then encrypt
      - category: user_action
        contains_pii: true
        ttl_days: 7
```

### GDPR Compliance

**Delete user data:**
```bash
curl -X POST \
  -H "Authorization: Bearer ${AEGION_ADMIN_TOKEN}" \
  http://localhost:8080/api/v1/analytics/users/{userId}/delete

Response:
{
  "status": "processing",
  "deletedRecords": 15234,
  "completionEstimate": "10 minutes"
}
```

---

## Event Ingestion Examples

### Track User Actions

```javascript
// Track page views
analyticsAPI.trackEvent({
  category: 'user_action',
  eventType: 'page_view',
  data: {
    page: '/dashboard',
    referrer: '/home',
    duration: 5000
  }
});
```

### Track System Events

```go
// Track config changes
analyticsClient.TrackEvent(&pb.Event{
  Category: "system_event",
  EventType: "config_change",
  Data: map[string]interface{}{
    "configKey": "rate_limit_per_minute",
    "oldValue": 600,
    "newValue": 1000,
    "changedBy": "admin@example.com",
  },
})
```

### Track Error Events

```python
# Track API errors
analytics_client.track_event(
  category='error_event',
  event_type='api_error',
  data={
    'error_type': 'database_connection',
    'status_code': 500,
    'endpoint': '/api/users',
    'duration_ms': 5000
  }
)
```

---

## Performance Optimization for Integrations

### Batch Ingestion

For high-volume event ingestion, use batch API:

```bash
curl -X POST \
  -H "Authorization: Bearer token" \
  -H "Content-Type: application/json" \
  -d '{
    "events": [
      { "category": "user_action", "eventType": "click", ... },
      { "category": "user_action", "eventType": "page_view", ... }
    ]
  }' \
  http://localhost:8080/api/v1/analytics/events/batch
```

### Connection Pooling

Reuse connections for repeated requests:

```go
// Connection pooling example
client := &http.Client{
  Transport: &http.Transport{
    MaxIdleConns: 100,
    MaxIdleConnsPerHost: 100,
    MaxConnsPerHost: 100,
  },
}
```

### Caching Results

Cache dashboard/query results to reduce database load:

```javascript
const cache = new Map();

async function getDashboard(id) {
  if (cache.has(id)) {
    return cache.get(id);
  }
  
  const dashboard = await api.getDashboard(id);
  cache.set(id, dashboard);
  
  // Invalidate cache after 5 minutes
  setTimeout(() => cache.delete(id), 5 * 60 * 1000);
  
  return dashboard;
}
```

---

## Related Documentation

- [Setup Guide](./setup.md)
- [API Reference](./api.md)
- [Webhooks](./webhooks.md)
- [Security](./security.md)
- [Admin SPA Guide](./admin-spa.md)
