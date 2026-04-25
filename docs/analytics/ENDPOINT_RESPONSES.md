# Aegion Analytics - Response Examples

Complete response examples for all major endpoint categories.

---

## Health & Status Responses

### GET /health

**Success (200):**
```json
{
  "status": "healthy",
  "version": "1.0.0",
  "timestamp": "2024-01-15T10:30:45.123Z",
  "uptime": "168h45m30s"
}
```

### GET /ready

**Success (200):**
```json
{
  "ready": true,
  "dependencies": {
    "database": {
      "status": "ready",
      "latency": "2ms"
    },
    "cache": {
      "status": "ready",
      "latency": "1ms"
    },
    "storage": {
      "status": "ready",
      "latency": "45ms"
    }
  },
  "checks": 3,
  "timestamp": "2024-01-15T10:30:45.123Z"
}
```

### GET /live

**Success (200):**
```json
{
  "live": true
}
```

### GET /metrics

**Success (200):**
```
# HELP analytics_events_processed_total Total events processed since startup
# TYPE analytics_events_processed_total counter
analytics_events_processed_total 15234

# HELP analytics_dashboards_created Total dashboards created
# TYPE analytics_dashboards_created counter
analytics_dashboards_created 42

# HELP analytics_query_duration_seconds Query execution duration
# TYPE analytics_query_duration_seconds histogram
analytics_query_duration_seconds_bucket{le="0.1"} 234
analytics_query_duration_seconds_bucket{le="0.5"} 1024
analytics_query_duration_seconds_bucket{le="1.0"} 1542
analytics_query_duration_seconds_sum 523.45
analytics_query_duration_seconds_count 1542
```

### GET /stats

**Success (200):**
```json
{
  "eventsProcessed": 15234,
  "dashboardsCreated": 42,
  "activeQueries": 3,
  "uptime": "48h30m",
  "averageQueryTime": "245ms",
  "totalUsers": 156,
  "activeUsers": 23,
  "storageUsedGB": 45.3,
  "timestamp": "2024-01-15T10:30:45.123Z"
}
```

---

## Events Responses

### GET /events

**Success (200):**
```json
{
  "data": [
    {
      "id": "evt_550e8400e29b41d4a716446655440000",
      "category": "user_action",
      "eventType": "button_click",
      "data": {
        "buttonId": "submit_btn",
        "page": "/dashboard",
        "value": "save"
      },
      "userId": "usr_550e8400e29b41d4a716446655440001",
      "sessionId": "sess_550e8400e29b41d4a716446655440002",
      "createdAt": "2024-01-15T10:30:45.123Z",
      "updatedAt": "2024-01-15T10:30:45.123Z"
    },
    {
      "id": "evt_550e8400e29b41d4a716446655440003",
      "category": "system",
      "eventType": "error",
      "data": {
        "code": "QUERY_TIMEOUT",
        "message": "Query exceeded timeout"
      },
      "userId": "usr_550e8400e29b41d4a716446655440004",
      "sessionId": "sess_550e8400e29b41d4a716446655440005",
      "createdAt": "2024-01-15T10:28:30.456Z",
      "updatedAt": "2024-01-15T10:28:30.456Z"
    }
  ],
  "pagination": {
    "page": 1,
    "limit": 50,
    "total": 1542,
    "pages": 31
  }
}
```

**Error (400) - Invalid Filter:**
```json
{
  "error": "INVALID_FILTER",
  "message": "Invalid date format in startDate",
  "details": "Expected ISO 8601 format (YYYY-MM-DDTHH:MM:SSZ)"
}
```

**Error (401) - Unauthorized:**
```json
{
  "error": "UNAUTHORIZED",
  "message": "Missing or invalid authentication token",
  "details": ""
}
```

### POST /events/search

**Success (200):**
```json
{
  "query": "button click",
  "filters": {
    "category": "user_action",
    "eventType": "button_click"
  },
  "data": [
    {
      "id": "evt_550e8400e29b41d4a716446655440000",
      "category": "user_action",
      "eventType": "button_click",
      "data": {
        "buttonId": "submit_btn"
      },
      "userId": "usr_550e8400e29b41d4a716446655440001",
      "sessionId": "sess_550e8400e29b41d4a716446655440002",
      "createdAt": "2024-01-15T10:30:45.123Z",
      "updatedAt": "2024-01-15T10:30:45.123Z"
    }
  ],
  "pagination": {
    "page": 1,
    "limit": 50,
    "total": 342,
    "pages": 7
  },
  "searchTime": "245ms"
}
```

**Error (400) - Malformed Query:**
```json
{
  "error": "INVALID_REQUEST",
  "message": "Invalid search request",
  "details": "filters.dateRange.start must be before dateRange.end"
}
```

### GET /events/{id}

**Success (200):**
```json
{
  "id": "evt_550e8400e29b41d4a716446655440000",
  "category": "user_action",
  "eventType": "button_click",
  "data": {
    "buttonId": "submit_btn",
    "page": "/dashboard",
    "value": "save",
    "timestamp": 1705316445123
  },
  "userId": "usr_550e8400e29b41d4a716446655440001",
  "sessionId": "sess_550e8400e29b41d4a716446655440002",
  "properties": {
    "browser": "Chrome 121",
    "os": "Windows 10",
    "device": "desktop"
  },
  "createdAt": "2024-01-15T10:30:45.123Z",
  "updatedAt": "2024-01-15T10:30:45.123Z"
}
```

**Error (404) - Not Found:**
```json
{
  "error": "NOT_FOUND",
  "message": "Event not found",
  "details": "Event with ID evt_invalid does not exist"
}
```

### GET /events/{id}/related

**Success (200):**
```json
{
  "eventId": "evt_550e8400e29b41d4a716446655440000",
  "relation": "session",
  "data": [
    {
      "id": "evt_550e8400e29b41d4a716446655440003",
      "category": "user_action",
      "eventType": "page_view",
      "data": {
        "page": "/dashboard"
      },
      "userId": "usr_550e8400e29b41d4a716446655440001",
      "sessionId": "sess_550e8400e29b41d4a716446655440002",
      "createdAt": "2024-01-15T10:30:30.000Z",
      "updatedAt": "2024-01-15T10:30:30.000Z"
    },
    {
      "id": "evt_550e8400e29b41d4a716446655440004",
      "category": "user_action",
      "eventType": "scroll",
      "data": {
        "page": "/dashboard",
        "scrollDepth": 75
      },
      "userId": "usr_550e8400e29b41d4a716446655440001",
      "sessionId": "sess_550e8400e29b41d4a716446655440002",
      "createdAt": "2024-01-15T10:30:35.789Z",
      "updatedAt": "2024-01-15T10:30:35.789Z"
    }
  ],
  "count": 2,
  "hasMore": false
}
```

### POST /events/export

**Success (200) - CSV:**
```
id,category,eventType,userId,createdAt
evt_550e8400e29b41d4a716446655440000,user_action,button_click,usr_550e8400e29b41d4a716446655440001,2024-01-15T10:30:45Z
evt_550e8400e29b41d4a716446655440003,user_action,page_view,usr_550e8400e29b41d4a716446655440001,2024-01-15T10:30:30Z
evt_550e8400e29b41d4a716446655440004,user_action,scroll,usr_550e8400e29b41d4a716446655440001,2024-01-15T10:30:35Z
```

**Success (200) - JSON:**
```json
{
  "data": [
    {
      "id": "evt_550e8400e29b41d4a716446655440000",
      "category": "user_action",
      "eventType": "button_click",
      "userId": "usr_550e8400e29b41d4a716446655440001",
      "createdAt": "2024-01-15T10:30:45.123Z"
    }
  ],
  "total": 1542,
  "exportedAt": "2024-01-15T10:35:00.000Z"
}
```

---

## Dashboard Responses

### GET /dashboards

**Success (200):**
```json
{
  "data": [
    {
      "id": "dash_550e8400e29b41d4a716446655440000",
      "name": "User Analytics",
      "description": "Main analytics dashboard for user behavior",
      "config": {
        "theme": "light",
        "layout": "grid",
        "refreshInterval": 60,
        "components": [
          {
            "id": "comp_1",
            "type": "chart",
            "title": "Daily Active Users",
            "queryId": "qry_550e8400e29b41d4a716446655440001"
          }
        ]
      },
      "ownerId": "usr_550e8400e29b41d4a716446655440002",
      "isDefault": true,
      "isPublic": false,
      "createdAt": "2024-01-10T09:00:00.000Z",
      "updatedAt": "2024-01-15T10:30:45.123Z"
    }
  ],
  "pagination": {
    "page": 1,
    "limit": 50,
    "total": 8,
    "pages": 1
  }
}
```

### POST /dashboards

**Success (201):**
```json
{
  "id": "dash_550e8400e29b41d4a716446655440000",
  "name": "User Analytics",
  "description": "Main analytics dashboard for user behavior",
  "config": {
    "theme": "light",
    "layout": "grid",
    "refreshInterval": 60,
    "components": []
  },
  "ownerId": "usr_550e8400e29b41d4a716446655440002",
  "isDefault": false,
  "isPublic": false,
  "createdAt": "2024-01-15T10:30:45.123Z",
  "updatedAt": "2024-01-15T10:30:45.123Z"
}
```

**Error (400) - Invalid Request:**
```json
{
  "error": "INVALID_DASHBOARD",
  "message": "Dashboard name is required",
  "details": "name field cannot be empty"
}
```

### PUT /dashboards/{id}

**Success (200):**
```json
{
  "id": "dash_550e8400e29b41d4a716446655440000",
  "name": "User Analytics - Updated",
  "description": "Updated analytics dashboard",
  "config": {
    "theme": "dark",
    "layout": "grid",
    "refreshInterval": 30
  },
  "ownerId": "usr_550e8400e29b41d4a716446655440002",
  "isDefault": true,
  "isPublic": true,
  "createdAt": "2024-01-10T09:00:00.000Z",
  "updatedAt": "2024-01-15T10:35:20.456Z"
}
```

**Error (403) - Forbidden:**
```json
{
  "error": "FORBIDDEN",
  "message": "Cannot update dashboard owned by another user",
  "details": "User usr_550e8400e29b41d4a716446655440003 does not own dashboard dash_550e8400e29b41d4a716446655440000"
}
```

### DELETE /dashboards/{id}

**Success (204):** No Content

### POST /dashboards/{id}/share

**Success (200):**
```json
{
  "dashboardId": "dash_550e8400e29b41d4a716446655440000",
  "sharedWith": [
    {
      "userId": "usr_550e8400e29b41d4a716446655440003",
      "permission": "view",
      "sharedAt": "2024-01-15T10:30:45.123Z"
    }
  ],
  "shareUrl": "https://analytics.example.com/shared/dash_550e8400e29b41d4a716446655440000"
}
```

---

## Query Responses

### GET /queries

**Success (200):**
```json
{
  "data": [
    {
      "id": "qry_550e8400e29b41d4a716446655440000",
      "name": "Daily Active Users",
      "description": "Count of daily active users",
      "sql": "SELECT DATE(created_at) as day, COUNT(DISTINCT user_id) as count FROM events GROUP BY day ORDER BY day DESC",
      "ownerId": "usr_550e8400e29b41d4a716446655440001",
      "createdAt": "2024-01-10T09:00:00.000Z",
      "updatedAt": "2024-01-15T10:30:45.123Z"
    }
  ],
  "pagination": {
    "page": 1,
    "limit": 50,
    "total": 12,
    "pages": 1
  }
}
```

### POST /queries

**Success (201):**
```json
{
  "id": "qry_550e8400e29b41d4a716446655440000",
  "name": "Daily Active Users",
  "description": "Count of daily active users",
  "sql": "SELECT DATE(created_at) as day, COUNT(DISTINCT user_id) as count FROM events GROUP BY day ORDER BY day DESC",
  "ownerId": "usr_550e8400e29b41d4a716446655440001",
  "createdAt": "2024-01-15T10:30:45.123Z",
  "updatedAt": "2024-01-15T10:30:45.123Z"
}
```

### GET /queries/{id}/execute

**Success (200):**
```json
{
  "queryId": "qry_550e8400e29b41d4a716446655440000",
  "data": [
    {
      "day": "2024-01-15",
      "count": 1250
    },
    {
      "day": "2024-01-14",
      "count": 1180
    },
    {
      "day": "2024-01-13",
      "count": 1320
    }
  ],
  "executionTime": "245ms",
  "rowsReturned": 3
}
```

**Error (400) - Query Error:**
```json
{
  "error": "QUERY_FAILED",
  "message": "Query execution failed",
  "details": "Error: Column 'invalid_column' does not exist"
}
```

---

## Report Responses

### GET /reports

**Success (200):**
```json
{
  "data": [
    {
      "id": "rpt_550e8400e29b41d4a716446655440000",
      "name": "Weekly Summary",
      "description": "Weekly analytics summary",
      "queryId": "qry_550e8400e29b41d4a716446655440001",
      "format": "pdf",
      "schedule": "weekly",
      "recipients": ["analyst@company.com"],
      "lastGenerated": "2024-01-15T08:00:00.000Z",
      "lastStatus": "completed",
      "createdAt": "2024-01-10T09:00:00.000Z",
      "updatedAt": "2024-01-15T08:00:00.000Z"
    }
  ],
  "pagination": {
    "page": 1,
    "limit": 50,
    "total": 5,
    "pages": 1
  }
}
```

### POST /reports

**Success (201):**
```json
{
  "id": "rpt_550e8400e29b41d4a716446655440000",
  "name": "Weekly Summary",
  "description": "Weekly analytics summary",
  "queryId": "qry_550e8400e29b41d4a716446655440001",
  "format": "pdf",
  "schedule": "weekly",
  "recipients": ["analyst@company.com"],
  "enabled": true,
  "createdAt": "2024-01-15T10:30:45.123Z",
  "updatedAt": "2024-01-15T10:30:45.123Z"
}
```

### POST /reports/{id}/generate

**Success (202):**
```json
{
  "jobId": "job_550e8400e29b41d4a716446655440000",
  "reportId": "rpt_550e8400e29b41d4a716446655440001",
  "status": "generating",
  "progress": 0,
  "createdAt": "2024-01-15T10:30:45.123Z",
  "estimatedCompletionTime": "2024-01-15T10:35:45.123Z"
}
```

### GET /reports/{id}/download

**Success (200):**
```
Content-Type: application/pdf
Content-Disposition: attachment; filename="report_2024-01-15.pdf"
Content-Length: 145382

[PDF binary content]
```

---

## Configuration Responses

### GET /config/storage

**Success (200):**
```json
{
  "provider": "s3",
  "endpoint": "s3.amazonaws.com",
  "bucket": "analytics-data",
  "region": "us-east-1",
  "retentionDays": 90,
  "compressionEnabled": true,
  "encryptionEnabled": true,
  "lastVerified": "2024-01-15T10:00:00.000Z"
}
```

### POST /config/storage/test

**Success (200):**
```json
{
  "status": "connected",
  "message": "Storage connection successful",
  "latency": "45ms",
  "timestamp": "2024-01-15T10:30:45.123Z"
}
```

**Error (500) - Connection Failed:**
```json
{
  "error": "CONNECTION_FAILED",
  "message": "Failed to connect to storage",
  "details": "Connection timeout after 30s trying to reach s3.amazonaws.com"
}
```

### GET /config/sync

**Success (200):**
```json
{
  "enabled": true,
  "interval": "1h",
  "timeout": "30m",
  "retryAttempts": 3,
  "lastSyncTime": "2024-01-15T09:30:00.000Z",
  "lastSyncStatus": "completed"
}
```

### POST /config/sync/trigger

**Success (202):**
```json
{
  "syncId": "sync_550e8400e29b41d4a716446655440000",
  "status": "running",
  "startedAt": "2024-01-15T10:30:45.123Z",
  "estimatedCompletionTime": "2024-01-15T10:45:45.123Z"
}
```

### GET /config/sync/{syncId}/status

**Success (200):**
```json
{
  "syncId": "sync_550e8400e29b41d4a716446655440000",
  "status": "completed",
  "progress": 100,
  "startedAt": "2024-01-15T10:30:45.123Z",
  "completedAt": "2024-01-15T10:45:30.789Z",
  "recordsSynced": 15234,
  "errors": 0,
  "duration": "14m45s"
}
```

### GET /config/retention

**Success (200):**
```json
{
  "hotDataDays": 30,
  "warmDataDays": 90,
  "coldDataDays": 365,
  "archiveEnabled": true,
  "archiveInterval": "monthly",
  "lastArchiveTime": "2024-01-01T00:00:00.000Z",
  "nextArchiveTime": "2024-02-01T00:00:00.000Z"
}
```

### GET /config/retention/archive-history

**Success (200):**
```json
{
  "data": [
    {
      "jobId": "job_550e8400e29b41d4a716446655440000",
      "startedAt": "2024-01-15T08:00:00.000Z",
      "completedAt": "2024-01-15T08:30:00.000Z",
      "status": "completed",
      "recordsArchived": 5000,
      "duration": "30m0s"
    },
    {
      "jobId": "job_550e8400e29b41d4a716446655440001",
      "startedAt": "2024-01-14T08:00:00.000Z",
      "completedAt": "2024-01-14T08:25:00.000Z",
      "status": "completed",
      "recordsArchived": 4800,
      "duration": "25m0s"
    }
  ],
  "total": 45
}
```

---

## Webhook Responses

### GET /webhooks

**Success (200):**
```json
{
  "data": [
    {
      "id": "whk_550e8400e29b41d4a716446655440000",
      "url": "https://example.com/webhook",
      "events": ["event.created", "dashboard.updated"],
      "enabled": true,
      "lastDelivery": "2024-01-15T10:25:00.000Z",
      "deliveryCount": 42,
      "failureCount": 2,
      "createdAt": "2024-01-10T09:00:00.000Z",
      "updatedAt": "2024-01-15T10:30:45.123Z"
    }
  ],
  "pagination": {
    "page": 1,
    "limit": 50,
    "total": 3,
    "pages": 1
  }
}
```

### POST /webhooks

**Success (201):**
```json
{
  "id": "whk_550e8400e29b41d4a716446655440000",
  "url": "https://example.com/webhook",
  "events": ["event.created", "dashboard.updated"],
  "headers": {
    "Authorization": "Bearer your-token"
  },
  "enabled": true,
  "secret": "whk_secret_550e8400e29b41d4a716446655440",
  "createdAt": "2024-01-15T10:30:45.123Z",
  "updatedAt": "2024-01-15T10:30:45.123Z"
}
```

### POST /webhooks/{id}/test

**Success (200):**
```json
{
  "webhookId": "whk_550e8400e29b41d4a716446655440000",
  "deliveryId": "del_550e8400e29b41d4a716446655440001",
  "status": "sent",
  "responseCode": 200,
  "responseTime": "145ms",
  "timestamp": "2024-01-15T10:30:45.123Z"
}
```

### GET /webhooks/{id}/delivery-history

**Success (200):**
```json
{
  "webhookId": "whk_550e8400e29b41d4a716446655440000",
  "data": [
    {
      "deliveryId": "del_550e8400e29b41d4a716446655440001",
      "timestamp": "2024-01-15T10:30:45.123Z",
      "status": "success",
      "responseCode": 200,
      "responseTime": "145ms",
      "event": "event.created"
    },
    {
      "deliveryId": "del_550e8400e29b41d4a716446655440002",
      "timestamp": "2024-01-15T10:20:15.456Z",
      "status": "failed",
      "responseCode": 500,
      "responseTime": "2500ms",
      "event": "dashboard.updated",
      "error": "Internal Server Error"
    }
  ],
  "pagination": {
    "total": 42,
    "failed": 2
  }
}
```

---

## Error Response Examples

### 400 Bad Request

```json
{
  "error": "INVALID_REQUEST",
  "message": "Invalid request parameters",
  "details": "Field 'name' is required and cannot be empty"
}
```

### 401 Unauthorized

```json
{
  "error": "UNAUTHORIZED",
  "message": "Authentication required",
  "details": "Missing or invalid Authorization header"
}
```

### 403 Forbidden

```json
{
  "error": "FORBIDDEN",
  "message": "Permission denied",
  "details": "User does not have permission to access this resource"
}
```

### 404 Not Found

```json
{
  "error": "NOT_FOUND",
  "message": "Resource not found",
  "details": "Dashboard with ID dash_invalid does not exist"
}
```

### 429 Too Many Requests

```json
{
  "error": "RATE_LIMITED",
  "message": "Rate limit exceeded",
  "retryAfter": 60,
  "limit": "100 requests per minute"
}
```

### 500 Internal Server Error

```json
{
  "error": "INTERNAL_ERROR",
  "message": "An unexpected error occurred",
  "details": "Please contact support with request ID: req_550e8400e29b41d4a716446655440000"
}
```

---

**Last Updated:** January 2024
