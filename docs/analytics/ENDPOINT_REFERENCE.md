# Aegion Analytics REST API - Endpoint Reference

Complete reference guide for all 32 REST API endpoints available in the Aegion Analytics system.

**Base URL:** `http://localhost:8080/api/v1/analytics` (development)

**Authentication:** JWT Bearer token in Authorization header (except health endpoints)

---

## Health & Status Endpoints (6)

### 1. System Health Check
- **Path:** `/health`
- **Method:** `GET`
- **Authentication:** No
- **Rate Limited:** No
- **Description:** Check if the analytics service is running

**Response (200):**
```json
{
  "status": "healthy",
  "version": "1.0.0",
  "timestamp": "2024-01-15T10:30:45Z"
}
```

**cURL:**
```bash
curl -X GET http://localhost:8080/api/v1/analytics/health
```

---

### 2. Readiness Check
- **Path:** `/ready`
- **Method:** `GET`
- **Authentication:** No
- **Rate Limited:** No
- **Description:** Check if all dependencies are ready (DB, cache, etc.)

**Response (200):**
```json
{
  "ready": true,
  "dependencies": {
    "database": "ready",
    "cache": "ready",
    "storage": "ready"
  }
}
```

**cURL:**
```bash
curl -X GET http://localhost:8080/api/v1/analytics/ready
```

---

### 3. Liveness Check
- **Path:** `/live`
- **Method:** `GET`
- **Authentication:** No
- **Rate Limited:** No
- **Description:** Check if service is alive (Kubernetes liveness probe)

**Response (200):**
```json
{
  "live": true
}
```

**cURL:**
```bash
curl -X GET http://localhost:8080/api/v1/analytics/live
```

---

### 4. Metrics
- **Path:** `/metrics`
- **Method:** `GET`
- **Authentication:** No
- **Rate Limited:** No
- **Description:** Prometheus-compatible metrics endpoint

**Response (200):**
```
# HELP analytics_events_processed_total Total events processed
# TYPE analytics_events_processed_total counter
analytics_events_processed_total 15234
```

**cURL:**
```bash
curl -X GET http://localhost:8080/api/v1/analytics/metrics
```

---

### 5. System Stats
- **Path:** `/stats`
- **Method:** `GET`
- **Authentication:** No
- **Rate Limited:** No
- **Description:** System statistics and usage information

**Response (200):**
```json
{
  "eventsProcessed": 15234,
  "dashboardsCreated": 42,
  "activeQueries": 3,
  "uptime": "48h30m",
  "averageQueryTime": "245ms"
}
```

**cURL:**
```bash
curl -X GET http://localhost:8080/api/v1/analytics/stats
```

---

### 6. Export Formats
- **Path:** `/export-formats`
- **Method:** `GET`
- **Authentication:** No
- **Rate Limited:** No
- **Description:** List supported export file formats

**Response (200):**
```json
{
  "formats": [
    {
      "id": "csv",
      "name": "CSV",
      "mimeType": "text/csv"
    },
    {
      "id": "json",
      "name": "JSON",
      "mimeType": "application/json"
    },
    {
      "id": "xlsx",
      "name": "Excel",
      "mimeType": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
    },
    {
      "id": "parquet",
      "name": "Parquet",
      "mimeType": "application/octet-stream"
    }
  ]
}
```

**cURL:**
```bash
curl -X GET http://localhost:8080/api/v1/analytics/export-formats
```

---

## Events Endpoints (5)

### 7. List Events
- **Path:** `/events`
- **Method:** `GET`
- **Authentication:** Yes (JWT)
- **Rate Limited:** Yes
- **Description:** List all events with pagination and filtering

**Query Parameters:**
- `page` (integer, default: 1) - Page number
- `limit` (integer, default: 50) - Results per page
- `category` (string) - Filter by event category
- `type` (string) - Filter by event type
- `userId` (string) - Filter by user ID
- `startDate` (ISO 8601) - Filter by start date
- `endDate` (ISO 8601) - Filter by end date

**Response (200):**
```json
{
  "data": [
    {
      "id": "evt_123abc",
      "category": "user_action",
      "eventType": "button_click",
      "data": {
        "buttonId": "submit_btn",
        "page": "/dashboard"
      },
      "userId": "usr_456def",
      "sessionId": "sess_789ghi",
      "createdAt": "2024-01-15T10:30:45Z",
      "updatedAt": "2024-01-15T10:30:45Z"
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

**Error (400):**
```json
{
  "error": "INVALID_FILTER",
  "message": "Invalid date format in startDate",
  "details": ""
}
```

**cURL:**
```bash
curl -X GET "http://localhost:8080/api/v1/analytics/events?page=1&limit=50&category=user_action" \
  -H "Authorization: Bearer YOUR_JWT_TOKEN"
```

---

### 8. Search Events
- **Path:** `/events/search`
- **Method:** `POST`
- **Authentication:** Yes (JWT)
- **Rate Limited:** Yes
- **Description:** Advanced search with full-text and complex filters

**Request Body:**
```json
{
  "query": "button click",
  "filters": {
    "category": "user_action",
    "eventType": "button_click",
    "dateRange": {
      "start": "2024-01-01T00:00:00Z",
      "end": "2024-01-31T23:59:59Z"
    },
    "userId": "usr_456def"
  },
  "page": 1,
  "limit": 50
}
```

**Response (200):**
```json
{
  "data": [
    {
      "id": "evt_123abc",
      "category": "user_action",
      "eventType": "button_click",
      "data": {},
      "userId": "usr_456def",
      "sessionId": "sess_789ghi",
      "createdAt": "2024-01-15T10:30:45Z",
      "updatedAt": "2024-01-15T10:30:45Z"
    }
  ],
  "pagination": {
    "page": 1,
    "limit": 50,
    "total": 342,
    "pages": 7
  }
}
```

**cURL:**
```bash
curl -X POST http://localhost:8080/api/v1/analytics/events/search \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "button click",
    "filters": {
      "category": "user_action"
    },
    "page": 1,
    "limit": 50
  }'
```

---

### 9. Get Event
- **Path:** `/events/{id}`
- **Method:** `GET`
- **Authentication:** Yes (JWT)
- **Rate Limited:** Yes
- **Description:** Retrieve a single event by ID

**Path Parameters:**
- `id` (string, required) - Event ID

**Response (200):**
```json
{
  "id": "evt_123abc",
  "category": "user_action",
  "eventType": "button_click",
  "data": {
    "buttonId": "submit_btn",
    "page": "/dashboard"
  },
  "userId": "usr_456def",
  "sessionId": "sess_789ghi",
  "createdAt": "2024-01-15T10:30:45Z",
  "updatedAt": "2024-01-15T10:30:45Z"
}
```

**Error (404):**
```json
{
  "error": "NOT_FOUND",
  "message": "Event not found",
  "details": ""
}
```

**cURL:**
```bash
curl -X GET http://localhost:8080/api/v1/analytics/events/evt_123abc \
  -H "Authorization: Bearer YOUR_JWT_TOKEN"
```

---

### 10. Get Related Events
- **Path:** `/events/{id}/related`
- **Method:** `GET`
- **Authentication:** Yes (JWT)
- **Rate Limited:** Yes
- **Description:** Get events related to a specific event (same session, user, etc.)

**Path Parameters:**
- `id` (string, required) - Event ID

**Query Parameters:**
- `limit` (integer, default: 20) - Maximum related events to return
- `relation` (string, default: "session") - Relation type: "session", "user", "data"

**Response (200):**
```json
{
  "data": [
    {
      "id": "evt_456def",
      "category": "user_action",
      "eventType": "page_view",
      "data": {},
      "userId": "usr_456def",
      "sessionId": "sess_789ghi",
      "createdAt": "2024-01-15T10:30:30Z",
      "updatedAt": "2024-01-15T10:30:30Z"
    }
  ],
  "relation": "session",
  "count": 1
}
```

**cURL:**
```bash
curl -X GET "http://localhost:8080/api/v1/analytics/events/evt_123abc/related?limit=20" \
  -H "Authorization: Bearer YOUR_JWT_TOKEN"
```

---

### 11. Export Events
- **Path:** `/events/export`
- **Method:** `POST`
- **Authentication:** Yes (JWT)
- **Rate Limited:** Yes
- **Description:** Export events in specified format

**Request Body:**
```json
{
  "format": "csv",
  "filters": {
    "category": "user_action",
    "startDate": "2024-01-01T00:00:00Z",
    "endDate": "2024-01-31T23:59:59Z"
  },
  "includeColumns": ["id", "category", "eventType", "userId", "createdAt"]
}
```

**Response (200):**
```
Content-Type: text/csv
Content-Disposition: attachment; filename="events_2024-01-15.csv"

id,category,eventType,userId,createdAt
evt_123abc,user_action,button_click,usr_456def,2024-01-15T10:30:45Z
evt_456def,user_action,page_view,usr_456def,2024-01-15T10:30:30Z
```

**cURL:**
```bash
curl -X POST http://localhost:8080/api/v1/analytics/events/export \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "format": "csv",
    "filters": {
      "category": "user_action"
    }
  }' > events.csv
```

---

## Dashboard Endpoints (8)

### 12. List Dashboards
- **Path:** `/dashboards`
- **Method:** `GET`
- **Authentication:** Yes (JWT)
- **Rate Limited:** Yes
- **Description:** List all dashboards accessible to the user

**Query Parameters:**
- `page` (integer, default: 1)
- `limit` (integer, default: 50)
- `includeShared` (boolean, default: true) - Include dashboards shared with user

**Response (200):**
```json
{
  "data": [
    {
      "id": "dash_123abc",
      "name": "User Analytics",
      "description": "Main analytics dashboard",
      "config": {},
      "ownerId": "usr_456def",
      "isDefault": true,
      "isPublic": false,
      "createdAt": "2024-01-10T09:00:00Z",
      "updatedAt": "2024-01-15T10:30:45Z"
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

**cURL:**
```bash
curl -X GET "http://localhost:8080/api/v1/analytics/dashboards?page=1" \
  -H "Authorization: Bearer YOUR_JWT_TOKEN"
```

---

### 13. Create Dashboard
- **Path:** `/dashboards`
- **Method:** `POST`
- **Authentication:** Yes (JWT)
- **Rate Limited:** Yes
- **Description:** Create a new dashboard

**Request Body:**
```json
{
  "name": "User Analytics",
  "description": "Main analytics dashboard",
  "config": {
    "theme": "light",
    "layout": "grid"
  },
  "isDefault": false,
  "isPublic": false
}
```

**Response (201):**
```json
{
  "id": "dash_123abc",
  "name": "User Analytics",
  "description": "Main analytics dashboard",
  "config": {
    "theme": "light",
    "layout": "grid"
  },
  "ownerId": "usr_456def",
  "isDefault": false,
  "isPublic": false,
  "createdAt": "2024-01-15T10:30:45Z",
  "updatedAt": "2024-01-15T10:30:45Z"
}
```

**Error (400):**
```json
{
  "error": "INVALID_DASHBOARD",
  "message": "Dashboard name is required",
  "details": ""
}
```

**cURL:**
```bash
curl -X POST http://localhost:8080/api/v1/analytics/dashboards \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "User Analytics",
    "description": "Main analytics dashboard",
    "isDefault": false,
    "isPublic": false
  }'
```

---

### 14. Get Dashboard
- **Path:** `/dashboards/{id}`
- **Method:** `GET`
- **Authentication:** Yes (JWT)
- **Rate Limited:** Yes
- **Description:** Retrieve a specific dashboard

**Path Parameters:**
- `id` (string, required) - Dashboard ID

**Response (200):**
```json
{
  "id": "dash_123abc",
  "name": "User Analytics",
  "description": "Main analytics dashboard",
  "config": {
    "theme": "light",
    "layout": "grid",
    "components": [
      {
        "id": "comp_1",
        "type": "chart",
        "queryId": "qry_123"
      }
    ]
  },
  "ownerId": "usr_456def",
  "isDefault": true,
  "isPublic": false,
  "createdAt": "2024-01-10T09:00:00Z",
  "updatedAt": "2024-01-15T10:30:45Z"
}
```

**cURL:**
```bash
curl -X GET http://localhost:8080/api/v1/analytics/dashboards/dash_123abc \
  -H "Authorization: Bearer YOUR_JWT_TOKEN"
```

---

### 15. Update Dashboard
- **Path:** `/dashboards/{id}`
- **Method:** `PUT`
- **Authentication:** Yes (JWT)
- **Rate Limited:** Yes
- **Description:** Update an existing dashboard (owner only)

**Path Parameters:**
- `id` (string, required) - Dashboard ID

**Request Body:**
```json
{
  "name": "Updated Analytics",
  "description": "Updated description",
  "config": {
    "theme": "dark",
    "layout": "grid"
  },
  "isPublic": true
}
```

**Response (200):**
```json
{
  "id": "dash_123abc",
  "name": "Updated Analytics",
  "description": "Updated description",
  "config": {
    "theme": "dark",
    "layout": "grid"
  },
  "ownerId": "usr_456def",
  "isDefault": true,
  "isPublic": true,
  "createdAt": "2024-01-10T09:00:00Z",
  "updatedAt": "2024-01-15T10:35:20Z"
}
```

**Error (403):**
```json
{
  "error": "FORBIDDEN",
  "message": "Cannot update dashboard owned by another user",
  "details": ""
}
```

**cURL:**
```bash
curl -X PUT http://localhost:8080/api/v1/analytics/dashboards/dash_123abc \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Updated Analytics",
    "isPublic": true
  }'
```

---

### 16. Delete Dashboard
- **Path:** `/dashboards/{id}`
- **Method:** `DELETE`
- **Authentication:** Yes (JWT)
- **Rate Limited:** Yes
- **Description:** Delete a dashboard (owner only)

**Path Parameters:**
- `id` (string, required) - Dashboard ID

**Response (204):** No Content

**Error (403):**
```json
{
  "error": "FORBIDDEN",
  "message": "Cannot delete dashboard owned by another user",
  "details": ""
}
```

**cURL:**
```bash
curl -X DELETE http://localhost:8080/api/v1/analytics/dashboards/dash_123abc \
  -H "Authorization: Bearer YOUR_JWT_TOKEN"
```

---

### 17. Share Dashboard
- **Path:** `/dashboards/{id}/share`
- **Method:** `POST`
- **Authentication:** Yes (JWT)
- **Rate Limited:** Yes
- **Description:** Share dashboard with other users

**Path Parameters:**
- `id` (string, required) - Dashboard ID

**Request Body:**
```json
{
  "userId": "usr_789ghi",
  "permission": "view"
}
```

**Response (200):**
```json
{
  "dashboardId": "dash_123abc",
  "sharedWith": {
    "userId": "usr_789ghi",
    "permission": "view"
  },
  "createdAt": "2024-01-15T10:30:45Z"
}
```

**cURL:**
```bash
curl -X POST http://localhost:8080/api/v1/analytics/dashboards/dash_123abc/share \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "userId": "usr_789ghi",
    "permission": "view"
  }'
```

---

### 18. Execute Dashboard Query
- **Path:** `/dashboards/{id}/components/{componentId}/execute`
- **Method:** `POST`
- **Authentication:** Yes (JWT)
- **Rate Limited:** Yes
- **Description:** Execute a query for a dashboard component

**Path Parameters:**
- `id` (string, required) - Dashboard ID
- `componentId` (string, required) - Component ID

**Request Body:**
```json
{
  "parameters": {
    "timeRange": "7d",
    "userId": "usr_456def"
  }
}
```

**Response (200):**
```json
{
  "componentId": "comp_1",
  "data": [
    {
      "timestamp": "2024-01-15T00:00:00Z",
      "count": 1250
    }
  ],
  "queryTime": "245ms"
}
```

**cURL:**
```bash
curl -X POST http://localhost:8080/api/v1/analytics/dashboards/dash_123abc/components/comp_1/execute \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "parameters": {
      "timeRange": "7d"
    }
  }'
```

---

## Query Endpoints (4)

### 19. List Queries
- **Path:** `/queries`
- **Method:** `GET`
- **Authentication:** Yes (JWT)
- **Rate Limited:** Yes
- **Description:** List saved queries

**Query Parameters:**
- `page` (integer, default: 1)
- `limit` (integer, default: 50)

**Response (200):**
```json
{
  "data": [
    {
      "id": "qry_123abc",
      "name": "Daily Active Users",
      "description": "Count of daily active users",
      "sql": "SELECT COUNT(*) FROM events WHERE date = CURRENT_DATE",
      "ownerId": "usr_456def",
      "createdAt": "2024-01-10T09:00:00Z",
      "updatedAt": "2024-01-15T10:30:45Z"
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

**cURL:**
```bash
curl -X GET "http://localhost:8080/api/v1/analytics/queries?page=1" \
  -H "Authorization: Bearer YOUR_JWT_TOKEN"
```

---

### 20. Save Query
- **Path:** `/queries`
- **Method:** `POST`
- **Authentication:** Yes (JWT)
- **Rate Limited:** Yes
- **Description:** Create and save a new query

**Request Body:**
```json
{
  "name": "Daily Active Users",
  "description": "Count of daily active users",
  "sql": "SELECT COUNT(*) FROM events WHERE date = CURRENT_DATE"
}
```

**Response (201):**
```json
{
  "id": "qry_123abc",
  "name": "Daily Active Users",
  "description": "Count of daily active users",
  "sql": "SELECT COUNT(*) FROM events WHERE date = CURRENT_DATE",
  "ownerId": "usr_456def",
  "createdAt": "2024-01-15T10:30:45Z",
  "updatedAt": "2024-01-15T10:30:45Z"
}
```

**cURL:**
```bash
curl -X POST http://localhost:8080/api/v1/analytics/queries \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Daily Active Users",
    "description": "Count of daily active users",
    "sql": "SELECT COUNT(*) FROM events WHERE date = CURRENT_DATE"
  }'
```

---

### 21. Execute Query
- **Path:** `/queries/{id}/execute`
- **Method:** `GET`
- **Authentication:** Yes (JWT)
- **Rate Limited:** Yes
- **Description:** Execute a saved query

**Path Parameters:**
- `id` (string, required) - Query ID

**Query Parameters:**
- `parameters` (object) - Query parameters as JSON

**Response (200):**
```json
{
  "queryId": "qry_123abc",
  "data": [
    {
      "count": 1250
    }
  ],
  "executionTime": "245ms",
  "rowsReturned": 1
}
```

**Error (400):**
```json
{
  "error": "INVALID_QUERY",
  "message": "Query execution failed",
  "details": "syntax error"
}
```

**cURL:**
```bash
curl -X GET http://localhost:8080/api/v1/analytics/queries/qry_123abc/execute \
  -H "Authorization: Bearer YOUR_JWT_TOKEN"
```

---

### 22. Delete Query
- **Path:** `/queries/{id}`
- **Method:** `DELETE`
- **Authentication:** Yes (JWT)
- **Rate Limited:** Yes
- **Description:** Delete a saved query

**Path Parameters:**
- `id` (string, required) - Query ID

**Response (204):** No Content

**cURL:**
```bash
curl -X DELETE http://localhost:8080/api/v1/analytics/queries/qry_123abc \
  -H "Authorization: Bearer YOUR_JWT_TOKEN"
```

---

## Report Endpoints (7)

### 23. List Reports
- **Path:** `/reports`
- **Method:** `GET`
- **Authentication:** Yes (JWT)
- **Rate Limited:** Yes
- **Description:** List all generated reports

**Query Parameters:**
- `page` (integer, default: 1)
- `limit` (integer, default: 50)

**Response (200):**
```json
{
  "data": [
    {
      "id": "rpt_123abc",
      "name": "Weekly Summary",
      "description": "Weekly analytics summary",
      "queryId": "qry_456def",
      "format": "pdf",
      "schedule": "weekly",
      "lastGenerated": "2024-01-15T08:00:00Z",
      "createdAt": "2024-01-10T09:00:00Z",
      "updatedAt": "2024-01-15T08:00:00Z"
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

**cURL:**
```bash
curl -X GET "http://localhost:8080/api/v1/analytics/reports?page=1" \
  -H "Authorization: Bearer YOUR_JWT_TOKEN"
```

---

### 24. Create Report
- **Path:** `/reports`
- **Method:** `POST`
- **Authentication:** Yes (JWT)
- **Rate Limited:** Yes
- **Description:** Create a new report template

**Request Body:**
```json
{
  "name": "Weekly Summary",
  "description": "Weekly analytics summary",
  "queryId": "qry_456def",
  "format": "pdf",
  "schedule": "weekly",
  "recipients": ["user@example.com"]
}
```

**Response (201):**
```json
{
  "id": "rpt_123abc",
  "name": "Weekly Summary",
  "description": "Weekly analytics summary",
  "queryId": "qry_456def",
  "format": "pdf",
  "schedule": "weekly",
  "recipients": ["user@example.com"],
  "createdAt": "2024-01-15T10:30:45Z",
  "updatedAt": "2024-01-15T10:30:45Z"
}
```

**cURL:**
```bash
curl -X POST http://localhost:8080/api/v1/analytics/reports \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Weekly Summary",
    "queryId": "qry_456def",
    "format": "pdf",
    "schedule": "weekly"
  }'
```

---

### 25. Get Report
- **Path:** `/reports/{id}`
- **Method:** `GET`
- **Authentication:** Yes (JWT)
- **Rate Limited:** Yes
- **Description:** Get report details

**Path Parameters:**
- `id` (string, required) - Report ID

**Response (200):**
```json
{
  "id": "rpt_123abc",
  "name": "Weekly Summary",
  "description": "Weekly analytics summary",
  "queryId": "qry_456def",
  "format": "pdf",
  "schedule": "weekly",
  "lastGenerated": "2024-01-15T08:00:00Z",
  "createdAt": "2024-01-10T09:00:00Z",
  "updatedAt": "2024-01-15T08:00:00Z"
}
```

**cURL:**
```bash
curl -X GET http://localhost:8080/api/v1/analytics/reports/rpt_123abc \
  -H "Authorization: Bearer YOUR_JWT_TOKEN"
```

---

### 26. Update Report
- **Path:** `/reports/{id}`
- **Method:** `PUT`
- **Authentication:** Yes (JWT)
- **Rate Limited:** Yes
- **Description:** Update report configuration

**Path Parameters:**
- `id` (string, required) - Report ID

**Request Body:**
```json
{
  "name": "Weekly Summary Updated",
  "schedule": "daily",
  "recipients": ["user@example.com", "admin@example.com"]
}
```

**Response (200):**
```json
{
  "id": "rpt_123abc",
  "name": "Weekly Summary Updated",
  "schedule": "daily",
  "recipients": ["user@example.com", "admin@example.com"],
  "updatedAt": "2024-01-15T10:35:20Z"
}
```

**cURL:**
```bash
curl -X PUT http://localhost:8080/api/v1/analytics/reports/rpt_123abc \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "schedule": "daily"
  }'
```

---

### 27. Delete Report
- **Path:** `/reports/{id}`
- **Method:** `DELETE`
- **Authentication:** Yes (JWT)
- **Rate Limited:** Yes
- **Description:** Delete a report

**Path Parameters:**
- `id` (string, required) - Report ID

**Response (204):** No Content

**cURL:**
```bash
curl -X DELETE http://localhost:8080/api/v1/analytics/reports/rpt_123abc \
  -H "Authorization: Bearer YOUR_JWT_TOKEN"
```

---

### 28. Generate Report Now
- **Path:** `/reports/{id}/generate`
- **Method:** `POST`
- **Authentication:** Yes (JWT)
- **Rate Limited:** Yes
- **Description:** Manually trigger report generation

**Path Parameters:**
- `id` (string, required) - Report ID

**Response (202):** Accepted
```json
{
  "jobId": "job_123abc",
  "reportId": "rpt_123abc",
  "status": "generating",
  "createdAt": "2024-01-15T10:30:45Z"
}
```

**cURL:**
```bash
curl -X POST http://localhost:8080/api/v1/analytics/reports/rpt_123abc/generate \
  -H "Authorization: Bearer YOUR_JWT_TOKEN"
```

---

### 29. Download Report
- **Path:** `/reports/{id}/download`
- **Method:** `GET`
- **Authentication:** Yes (JWT)
- **Rate Limited:** Yes
- **Description:** Download a generated report file

**Path Parameters:**
- `id` (string, required) - Report ID

**Query Parameters:**
- `format` (string) - Output format override (optional)

**Response (200):**
```
Content-Type: application/pdf
Content-Disposition: attachment; filename="report_2024-01-15.pdf"

[PDF binary content]
```

**cURL:**
```bash
curl -X GET http://localhost:8080/api/v1/analytics/reports/rpt_123abc/download \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -o report.pdf
```

---

## Configuration Endpoints

### Storage Configuration (3)

#### 30. Get Storage Config
- **Path:** `/config/storage`
- **Method:** `GET`
- **Authentication:** Yes (JWT)
- **Rate Limited:** Yes
- **Description:** Get current storage configuration

**Response (200):**
```json
{
  "provider": "s3",
  "endpoint": "s3.amazonaws.com",
  "bucket": "analytics-data",
  "region": "us-east-1",
  "retentionDays": 90
}
```

**cURL:**
```bash
curl -X GET http://localhost:8080/api/v1/analytics/config/storage \
  -H "Authorization: Bearer YOUR_JWT_TOKEN"
```

---

#### 31. Update Storage Config
- **Path:** `/config/storage`
- **Method:** `PUT`
- **Authentication:** Yes (JWT)
- **Rate Limited:** Yes
- **Description:** Update storage configuration

**Request Body:**
```json
{
  "provider": "s3",
  "endpoint": "s3.amazonaws.com",
  "bucket": "analytics-data",
  "region": "us-east-1",
  "retentionDays": 90
}
```

**Response (200):**
```json
{
  "provider": "s3",
  "endpoint": "s3.amazonaws.com",
  "bucket": "analytics-data",
  "region": "us-east-1",
  "retentionDays": 90,
  "updatedAt": "2024-01-15T10:30:45Z"
}
```

**cURL:**
```bash
curl -X PUT http://localhost:8080/api/v1/analytics/config/storage \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "provider": "s3",
    "retentionDays": 90
  }'
```

---

#### 32. Test Storage Connection
- **Path:** `/config/storage/test`
- **Method:** `POST`
- **Authentication:** Yes (JWT)
- **Rate Limited:** No
- **Description:** Test storage connection

**Response (200):**
```json
{
  "status": "connected",
  "message": "Storage connection successful"
}
```

**Error (500):**
```json
{
  "error": "CONNECTION_FAILED",
  "message": "Failed to connect to storage",
  "details": "Connection timeout"
}
```

**cURL:**
```bash
curl -X POST http://localhost:8080/api/v1/analytics/config/storage/test \
  -H "Authorization: Bearer YOUR_JWT_TOKEN"
```

---

### Sync Configuration (4)

#### 33. Get Sync Config
- **Path:** `/config/sync`
- **Method:** `GET`
- **Authentication:** Yes (JWT)
- **Rate Limited:** Yes
- **Description:** Get sync configuration

**Response (200):**
```json
{
  "enabled": true,
  "interval": "1h",
  "timeout": "30m",
  "retryAttempts": 3
}
```

**cURL:**
```bash
curl -X GET http://localhost:8080/api/v1/analytics/config/sync \
  -H "Authorization: Bearer YOUR_JWT_TOKEN"
```

---

#### 34. Update Sync Config
- **Path:** `/config/sync`
- **Method:** `PUT`
- **Authentication:** Yes (JWT)
- **Rate Limited:** Yes
- **Description:** Update sync configuration

**Request Body:**
```json
{
  "enabled": true,
  "interval": "30m",
  "timeout": "30m",
  "retryAttempts": 5
}
```

**Response (200):**
```json
{
  "enabled": true,
  "interval": "30m",
  "timeout": "30m",
  "retryAttempts": 5,
  "updatedAt": "2024-01-15T10:30:45Z"
}
```

**cURL:**
```bash
curl -X PUT http://localhost:8080/api/v1/analytics/config/sync \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "interval": "30m",
    "retryAttempts": 5
  }'
```

---

#### 35. Trigger Manual Sync
- **Path:** `/config/sync/trigger`
- **Method:** `POST`
- **Authentication:** Yes (JWT)
- **Rate Limited:** No
- **Description:** Manually trigger data sync

**Response (202):** Accepted
```json
{
  "syncId": "sync_123abc",
  "status": "running",
  "startedAt": "2024-01-15T10:30:45Z"
}
```

**cURL:**
```bash
curl -X POST http://localhost:8080/api/v1/analytics/config/sync/trigger \
  -H "Authorization: Bearer YOUR_JWT_TOKEN"
```

---

#### 36. Get Sync Status
- **Path:** `/config/sync/{syncId}/status`
- **Method:** `GET`
- **Authentication:** Yes (JWT)
- **Rate Limited:** Yes
- **Description:** Get status of a sync operation

**Path Parameters:**
- `syncId` (string, required) - Sync ID

**Response (200):**
```json
{
  "syncId": "sync_123abc",
  "status": "completed",
  "startedAt": "2024-01-15T10:30:45Z",
  "completedAt": "2024-01-15T10:45:30Z",
  "recordsSynced": 15234,
  "errors": 0
}
```

**cURL:**
```bash
curl -X GET http://localhost:8080/api/v1/analytics/config/sync/sync_123abc/status \
  -H "Authorization: Bearer YOUR_JWT_TOKEN"
```

---

### Retention Configuration (4)

#### 37. Get Retention Policy
- **Path:** `/config/retention`
- **Method:** `GET`
- **Authentication:** Yes (JWT)
- **Rate Limited:** Yes
- **Description:** Get data retention policy

**Response (200):**
```json
{
  "hotDataDays": 30,
  "warmDataDays": 90,
  "coldDataDays": 365,
  "archiveEnabled": true,
  "archiveInterval": "monthly"
}
```

**cURL:**
```bash
curl -X GET http://localhost:8080/api/v1/analytics/config/retention \
  -H "Authorization: Bearer YOUR_JWT_TOKEN"
```

---

#### 38. Update Retention Policy
- **Path:** `/config/retention`
- **Method:** `PUT`
- **Authentication:** Yes (JWT)
- **Rate Limited:** Yes
- **Description:** Update retention policy

**Request Body:**
```json
{
  "hotDataDays": 30,
  "warmDataDays": 90,
  "coldDataDays": 365,
  "archiveEnabled": true,
  "archiveInterval": "monthly"
}
```

**Response (200):**
```json
{
  "hotDataDays": 30,
  "warmDataDays": 90,
  "coldDataDays": 365,
  "archiveEnabled": true,
  "archiveInterval": "monthly",
  "updatedAt": "2024-01-15T10:30:45Z"
}
```

**cURL:**
```bash
curl -X PUT http://localhost:8080/api/v1/analytics/config/retention \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "hotDataDays": 30,
    "archiveInterval": "monthly"
  }'
```

---

#### 39. Trigger Archival
- **Path:** `/config/retention/archive`
- **Method:** `POST`
- **Authentication:** Yes (JWT)
- **Rate Limited:** No
- **Description:** Manually trigger data archival

**Response (202):** Accepted
```json
{
  "jobId": "job_123abc",
  "status": "running",
  "startedAt": "2024-01-15T10:30:45Z"
}
```

**cURL:**
```bash
curl -X POST http://localhost:8080/api/v1/analytics/config/retention/archive \
  -H "Authorization: Bearer YOUR_JWT_TOKEN"
```

---

#### 40. Get Archive History
- **Path:** `/config/retention/archive-history`
- **Method:** `GET`
- **Authentication:** Yes (JWT)
- **Rate Limited:** Yes
- **Description:** Get data archival history

**Query Parameters:**
- `limit` (integer, default: 50)
- `offset` (integer, default: 0)

**Response (200):**
```json
{
  "data": [
    {
      "jobId": "job_123abc",
      "startedAt": "2024-01-15T08:00:00Z",
      "completedAt": "2024-01-15T08:30:00Z",
      "status": "completed",
      "recordsArchived": 5000
    }
  ],
  "total": 45
}
```

**cURL:**
```bash
curl -X GET "http://localhost:8080/api/v1/analytics/config/retention/archive-history?limit=50" \
  -H "Authorization: Bearer YOUR_JWT_TOKEN"
```

---

## Validation Endpoints (4)

### 41. Validate Storage Config
- **Path:** `/validate/storage`
- **Method:** `POST`
- **Authentication:** Yes (JWT)
- **Rate Limited:** No
- **Description:** Validate storage configuration without saving

**Request Body:**
```json
{
  "provider": "s3",
  "endpoint": "s3.amazonaws.com",
  "bucket": "analytics-data",
  "region": "us-east-1"
}
```

**Response (200):**
```json
{
  "valid": true,
  "errors": []
}
```

**Error (400):**
```json
{
  "valid": false,
  "errors": [
    "Invalid bucket name",
    "Region is required"
  ]
}
```

**cURL:**
```bash
curl -X POST http://localhost:8080/api/v1/analytics/validate/storage \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "provider": "s3",
    "bucket": "analytics-data"
  }'
```

---

### 42. Validate Sync Config
- **Path:** `/validate/sync`
- **Method:** `POST`
- **Authentication:** Yes (JWT)
- **Rate Limited:** No
- **Description:** Validate sync configuration

**Request Body:**
```json
{
  "interval": "30m",
  "timeout": "30m",
  "retryAttempts": 5
}
```

**Response (200):**
```json
{
  "valid": true,
  "errors": []
}
```

**cURL:**
```bash
curl -X POST http://localhost:8080/api/v1/analytics/validate/sync \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "interval": "30m"
  }'
```

---

### 43. Validate Retention Policy
- **Path:** `/validate/retention`
- **Method:** `POST`
- **Authentication:** Yes (JWT)
- **Rate Limited:** No
- **Description:** Validate retention policy

**Request Body:**
```json
{
  "hotDataDays": 30,
  "warmDataDays": 90,
  "coldDataDays": 365
}
```

**Response (200):**
```json
{
  "valid": true,
  "errors": []
}
```

**cURL:**
```bash
curl -X POST http://localhost:8080/api/v1/analytics/validate/retention \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "hotDataDays": 30,
    "warmDataDays": 90
  }'
```

---

### 44. Validate Webhook Config
- **Path:** `/validate/webhook`
- **Method:** `POST`
- **Authentication:** Yes (JWT)
- **Rate Limited:** No
- **Description:** Validate webhook configuration

**Request Body:**
```json
{
  "url": "https://example.com/webhook",
  "events": ["event.created", "dashboard.updated"],
  "headers": {
    "Authorization": "Bearer token"
  }
}
```

**Response (200):**
```json
{
  "valid": true,
  "errors": []
}
```

**cURL:**
```bash
curl -X POST http://localhost:8080/api/v1/analytics/validate/webhook \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "url": "https://example.com/webhook",
    "events": ["event.created"]
  }'
```

---

## Webhook Endpoints (8)

### 45. List Webhooks
- **Path:** `/webhooks`
- **Method:** `GET`
- **Authentication:** Yes (JWT)
- **Rate Limited:** Yes
- **Description:** List all webhooks

**Query Parameters:**
- `page` (integer, default: 1)
- `limit` (integer, default: 50)

**Response (200):**
```json
{
  "data": [
    {
      "id": "whk_123abc",
      "url": "https://example.com/webhook",
      "events": ["event.created", "dashboard.updated"],
      "enabled": true,
      "createdAt": "2024-01-10T09:00:00Z",
      "updatedAt": "2024-01-15T10:30:45Z"
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

**cURL:**
```bash
curl -X GET "http://localhost:8080/api/v1/analytics/webhooks?page=1" \
  -H "Authorization: Bearer YOUR_JWT_TOKEN"
```

---

### 46. Create Webhook
- **Path:** `/webhooks`
- **Method:** `POST`
- **Authentication:** Yes (JWT)
- **Rate Limited:** Yes
- **Description:** Create a new webhook

**Request Body:**
```json
{
  "url": "https://example.com/webhook",
  "events": ["event.created", "dashboard.updated"],
  "headers": {
    "Authorization": "Bearer your-token"
  },
  "enabled": true
}
```

**Response (201):**
```json
{
  "id": "whk_123abc",
  "url": "https://example.com/webhook",
  "events": ["event.created", "dashboard.updated"],
  "headers": {
    "Authorization": "Bearer your-token"
  },
  "enabled": true,
  "secret": "whk_secret_123abc",
  "createdAt": "2024-01-15T10:30:45Z",
  "updatedAt": "2024-01-15T10:30:45Z"
}
```

**cURL:**
```bash
curl -X POST http://localhost:8080/api/v1/analytics/webhooks \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "url": "https://example.com/webhook",
    "events": ["event.created"],
    "enabled": true
  }'
```

---

### 47. Get Webhook
- **Path:** `/webhooks/{id}`
- **Method:** `GET`
- **Authentication:** Yes (JWT)
- **Rate Limited:** Yes
- **Description:** Get webhook details

**Path Parameters:**
- `id` (string, required) - Webhook ID

**Response (200):**
```json
{
  "id": "whk_123abc",
  "url": "https://example.com/webhook",
  "events": ["event.created", "dashboard.updated"],
  "headers": {
    "Authorization": "Bearer your-token"
  },
  "enabled": true,
  "lastDelivery": "2024-01-15T10:25:00Z",
  "deliveryCount": 42,
  "failureCount": 2,
  "createdAt": "2024-01-10T09:00:00Z",
  "updatedAt": "2024-01-15T10:30:45Z"
}
```

**cURL:**
```bash
curl -X GET http://localhost:8080/api/v1/analytics/webhooks/whk_123abc \
  -H "Authorization: Bearer YOUR_JWT_TOKEN"
```

---

### 48. Update Webhook
- **Path:** `/webhooks/{id}`
- **Method:** `PUT`
- **Authentication:** Yes (JWT)
- **Rate Limited:** Yes
- **Description:** Update webhook configuration

**Path Parameters:**
- `id` (string, required) - Webhook ID

**Request Body:**
```json
{
  "url": "https://example.com/webhook-updated",
  "events": ["event.created", "event.updated"],
  "enabled": true
}
```

**Response (200):**
```json
{
  "id": "whk_123abc",
  "url": "https://example.com/webhook-updated",
  "events": ["event.created", "event.updated"],
  "enabled": true,
  "updatedAt": "2024-01-15T10:35:20Z"
}
```

**cURL:**
```bash
curl -X PUT http://localhost:8080/api/v1/analytics/webhooks/whk_123abc \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "url": "https://example.com/webhook-updated"
  }'
```

---

### 49. Delete Webhook
- **Path:** `/webhooks/{id}`
- **Method:** `DELETE`
- **Authentication:** Yes (JWT)
- **Rate Limited:** Yes
- **Description:** Delete a webhook

**Path Parameters:**
- `id` (string, required) - Webhook ID

**Response (204):** No Content

**cURL:**
```bash
curl -X DELETE http://localhost:8080/api/v1/analytics/webhooks/whk_123abc \
  -H "Authorization: Bearer YOUR_JWT_TOKEN"
```

---

### 50. Test Webhook
- **Path:** `/webhooks/{id}/test`
- **Method:** `POST`
- **Authentication:** Yes (JWT)
- **Rate Limited:** No
- **Description:** Send a test webhook delivery

**Path Parameters:**
- `id` (string, required) - Webhook ID

**Response (200):**
```json
{
  "webhookId": "whk_123abc",
  "deliveryId": "del_123abc",
  "status": "sent",
  "responseCode": 200,
  "responseTime": "145ms",
  "timestamp": "2024-01-15T10:30:45Z"
}
```

**cURL:**
```bash
curl -X POST http://localhost:8080/api/v1/analytics/webhooks/whk_123abc/test \
  -H "Authorization: Bearer YOUR_JWT_TOKEN"
```

---

### 51. Get Webhook Delivery History
- **Path:** `/webhooks/{id}/delivery-history`
- **Method:** `GET`
- **Authentication:** Yes (JWT)
- **Rate Limited:** Yes
- **Description:** Get webhook delivery history

**Path Parameters:**
- `id` (string, required) - Webhook ID

**Query Parameters:**
- `limit` (integer, default: 50)
- `offset` (integer, default: 0)
- `status` (string) - Filter by status: "success", "failed", "pending"

**Response (200):**
```json
{
  "data": [
    {
      "deliveryId": "del_123abc",
      "timestamp": "2024-01-15T10:30:45Z",
      "status": "success",
      "responseCode": 200,
      "responseTime": "145ms"
    }
  ],
  "total": 42,
  "failed": 2
}
```

**cURL:**
```bash
curl -X GET "http://localhost:8080/api/v1/analytics/webhooks/whk_123abc/delivery-history?limit=50" \
  -H "Authorization: Bearer YOUR_JWT_TOKEN"
```

---

### 52. Replay Webhook Deliveries
- **Path:** `/webhooks/{id}/replay`
- **Method:** `POST`
- **Authentication:** Yes (JWT)
- **Rate Limited:** No
- **Description:** Replay failed webhook deliveries

**Path Parameters:**
- `id` (string, required) - Webhook ID

**Request Body:**
```json
{
  "limit": 10,
  "statusFilter": "failed"
}
```

**Response (202):** Accepted
```json
{
  "webhookId": "whk_123abc",
  "jobId": "job_123abc",
  "deliveriesQueued": 2,
  "status": "processing",
  "createdAt": "2024-01-15T10:30:45Z"
}
```

**cURL:**
```bash
curl -X POST http://localhost:8080/api/v1/analytics/webhooks/whk_123abc/replay \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "limit": 10,
    "statusFilter": "failed"
  }'
```

---

## User Preferences Endpoints (4)

### 53. Get User Preferences
- **Path:** `/user/preferences`
- **Method:** `GET`
- **Authentication:** Yes (JWT)
- **Rate Limited:** Yes
- **Description:** Get current user preferences

**Response (200):**
```json
{
  "userId": "usr_456def",
  "theme": "dark",
  "dateFormat": "YYYY-MM-DD",
  "timeZone": "UTC",
  "defaultDashboard": "dash_123abc",
  "notifications": {
    "emailAlerts": true,
    "webhookAlerts": true
  }
}
```

**cURL:**
```bash
curl -X GET http://localhost:8080/api/v1/analytics/user/preferences \
  -H "Authorization: Bearer YOUR_JWT_TOKEN"
```

---

### 54. Update User Preferences
- **Path:** `/user/preferences`
- **Method:** `PUT`
- **Authentication:** Yes (JWT)
- **Rate Limited:** Yes
- **Description:** Update user preferences

**Request Body:**
```json
{
  "theme": "light",
  "dateFormat": "DD/MM/YYYY",
  "timeZone": "America/New_York",
  "defaultDashboard": "dash_456def",
  "notifications": {
    "emailAlerts": false,
    "webhookAlerts": true
  }
}
```

**Response (200):**
```json
{
  "userId": "usr_456def",
  "theme": "light",
  "dateFormat": "DD/MM/YYYY",
  "timeZone": "America/New_York",
  "defaultDashboard": "dash_456def",
  "notifications": {
    "emailAlerts": false,
    "webhookAlerts": true
  },
  "updatedAt": "2024-01-15T10:30:45Z"
}
```

**cURL:**
```bash
curl -X PUT http://localhost:8080/api/v1/analytics/user/preferences \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "theme": "light",
    "timeZone": "America/New_York"
  }'
```

---

### 55. Add Favorite Dashboard
- **Path:** `/user/favorites/dashboards/{dashboardId}`
- **Method:** `POST`
- **Authentication:** Yes (JWT)
- **Rate Limited:** Yes
- **Description:** Add dashboard to user favorites

**Path Parameters:**
- `dashboardId` (string, required) - Dashboard ID

**Response (200):**
```json
{
  "userId": "usr_456def",
  "dashboardId": "dash_123abc",
  "addedAt": "2024-01-15T10:30:45Z"
}
```

**cURL:**
```bash
curl -X POST http://localhost:8080/api/v1/analytics/user/favorites/dashboards/dash_123abc \
  -H "Authorization: Bearer YOUR_JWT_TOKEN"
```

---

### 56. Remove Favorite Dashboard
- **Path:** `/user/favorites/dashboards/{dashboardId}`
- **Method:** `DELETE`
- **Authentication:** Yes (JWT)
- **Rate Limited:** Yes
- **Description:** Remove dashboard from user favorites

**Path Parameters:**
- `dashboardId` (string, required) - Dashboard ID

**Response (204):** No Content

**cURL:**
```bash
curl -X DELETE http://localhost:8080/api/v1/analytics/user/favorites/dashboards/dash_123abc \
  -H "Authorization: Bearer YOUR_JWT_TOKEN"
```

---

## Common Response Codes

| Code | Meaning | Example |
|------|---------|---------|
| 200 | OK | Successful GET, PUT |
| 201 | Created | Successful POST creating resource |
| 202 | Accepted | Async operation started (generate, sync, etc.) |
| 204 | No Content | Successful DELETE |
| 400 | Bad Request | Invalid parameters, malformed body |
| 401 | Unauthorized | Missing/invalid JWT token |
| 403 | Forbidden | User lacks permissions |
| 404 | Not Found | Resource doesn't exist |
| 429 | Too Many Requests | Rate limit exceeded |
| 500 | Internal Server Error | Server error |

---

## Authentication

All protected endpoints require a JWT bearer token in the Authorization header:

```
Authorization: Bearer eyJhbGciOiJIUzI1NiIs...
```

---

## Rate Limiting

Rate-limited endpoints are protected with the following limits (default):
- 100 requests per minute per user
- 1000 requests per hour per user

When rate limit is exceeded, the API returns HTTP 429 with:
```json
{
  "error": "RATE_LIMITED",
  "message": "Rate limit exceeded",
  "retryAfter": 60
}
```

---

## Pagination

List endpoints support pagination with:
- `page` - Page number (1-indexed)
- `limit` - Results per page (default: 50, max: 100)

Response includes:
```json
{
  "pagination": {
    "page": 1,
    "limit": 50,
    "total": 1542,
    "pages": 31
  }
}
```

---

## Error Handling

All errors follow a consistent format:

```json
{
  "error": "ERROR_CODE",
  "message": "Human-readable message",
  "details": "Additional technical details"
}
```

Common error codes:
- `INVALID_REQUEST` - Malformed request
- `INVALID_DASHBOARD` - Invalid dashboard data
- `NOT_FOUND` - Resource not found
- `UNAUTHORIZED` - Authentication required
- `FORBIDDEN` - Permission denied
- `QUERY_FAILED` - Query execution failed
- `RATE_LIMITED` - Rate limit exceeded
- `CONNECTION_FAILED` - External service unavailable

---

## Performance Notes

### Response Times
- Health checks: <10ms
- List operations: 50-200ms
- Single resource fetch: 20-100ms
- Complex queries: 200-5000ms
- Report generation: 30-300s

### Caching
- Health endpoints: No caching
- Dashboard configs: 5 minute cache
- Query results: 1 minute cache (unless explicitly disabled)
- User preferences: 10 minute cache

### Recommended Limits
- Batch operations: max 1000 items
- List page size: 50 items (default)
- Query timeout: 5 minutes
- Report generation timeout: 30 minutes

---

**Last Updated:** January 2024
**API Version:** 1.0.0
**Total Endpoints:** 56
