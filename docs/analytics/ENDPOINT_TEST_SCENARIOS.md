# Aegion Analytics - Endpoint Test Scenarios

Complete end-to-end testing scenarios for the REST API with curl commands.

**Prerequisites:**
- Service running at `http://localhost:8080/api/v1/analytics`
- Valid JWT token in `$TOKEN` environment variable
- `curl` command-line tool installed
- `jq` for JSON parsing (optional but recommended)

---

## Scenario 1: Verify Service Health and Get System Stats

**Goal:** Verify the service is running and healthy

**Steps:**

1. **Check Health Status**
```bash
curl -X GET http://localhost:8080/api/v1/analytics/health
```

Expected: `{"status":"healthy","version":"1.0.0"}`

2. **Check Readiness**
```bash
curl -X GET http://localhost:8080/api/v1/analytics/ready
```

Expected: All dependencies ready

3. **Check System Statistics**
```bash
curl -X GET http://localhost:8080/api/v1/analytics/stats
```

Expected: System metrics (eventsProcessed, dashboardsCreated, etc.)

4. **Get Available Export Formats**
```bash
curl -X GET http://localhost:8080/api/v1/analytics/export-formats
```

Expected: List of formats (csv, json, xlsx, parquet)

**Validation:** All endpoints return 200 OK with expected data

---

## Scenario 2: Create Dashboard and Add Component with Query

**Goal:** Complete dashboard creation workflow

**Prerequisites:**
- Valid JWT token in `$TOKEN`

**Steps:**

1. **Create New Dashboard**
```bash
DASHBOARD=$(curl -s -X POST http://localhost:8080/api/v1/analytics/dashboards \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Sales Analytics Q1",
    "description": "Q1 2024 sales performance",
    "config": {
      "theme": "light",
      "layout": "grid",
      "refreshInterval": 60
    },
    "isPublic": false
  }')

DASHBOARD_ID=$(echo $DASHBOARD | jq -r '.id')
echo "Created dashboard: $DASHBOARD_ID"
```

Expected: 201 Created, returns dashboard object with ID

2. **Save a Query for Dashboard**
```bash
QUERY=$(curl -s -X POST http://localhost:8080/api/v1/analytics/queries \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Daily Revenue",
    "description": "Total revenue per day",
    "sql": "SELECT DATE(created_at) as day, SUM(amount) as revenue FROM events WHERE category = '\''sales'\'' GROUP BY day ORDER BY day DESC"
  }')

QUERY_ID=$(echo $QUERY | jq -r '.id')
echo "Saved query: $QUERY_ID"
```

Expected: 201 Created, returns query object with ID

3. **Retrieve Dashboard**
```bash
curl -s -X GET "http://localhost:8080/api/v1/analytics/dashboards/$DASHBOARD_ID" \
  -H "Authorization: Bearer $TOKEN" | jq '.'
```

Expected: 200 OK, returns full dashboard config

4. **Execute Dashboard Component Query**
```bash
EXECUTION=$(curl -s -X POST "http://localhost:8080/api/v1/analytics/dashboards/$DASHBOARD_ID/components/chart_1/execute" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "parameters": {
      "timeRange": "7d"
    }
  }')

echo $EXECUTION | jq '.'
```

Expected: 200 OK, returns query results

5. **Share Dashboard with Another User**
```bash
curl -s -X POST "http://localhost:8080/api/v1/analytics/dashboards/$DASHBOARD_ID/share" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "userId": "usr_789ghi",
    "permission": "view"
  }' | jq '.'
```

Expected: 200 OK, dashboard shared

6. **List All Dashboards**
```bash
curl -s -X GET "http://localhost:8080/api/v1/analytics/dashboards?limit=10" \
  -H "Authorization: Bearer $TOKEN" | jq '.'
```

Expected: 200 OK, list includes newly created dashboard

**Validation:** 
- Dashboard created successfully
- Query saved successfully
- Dashboard can be retrieved
- Component query executes
- Dashboard can be shared
- Dashboard appears in list

---

## Scenario 3: Work with Events and Search

**Goal:** Create, search, and export events

**Prerequisites:**
- Valid JWT token in `$TOKEN`

**Steps:**

1. **List Recent Events**
```bash
curl -s -X GET "http://localhost:8080/api/v1/analytics/events?page=1&limit=20" \
  -H "Authorization: Bearer $TOKEN" | jq '.'
```

Expected: 200 OK, returns paginated event list

2. **Search Events by Category**
```bash
SEARCH_RESULT=$(curl -s -X POST http://localhost:8080/api/v1/analytics/events/search \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "user action",
    "filters": {
      "category": "user_action",
      "dateRange": {
        "start": "2024-01-01T00:00:00Z",
        "end": "2024-01-31T23:59:59Z"
      }
    },
    "page": 1,
    "limit": 50
  }')

EVENT_ID=$(echo $SEARCH_RESULT | jq -r '.data[0].id')
echo "Found event: $EVENT_ID"
```

Expected: 200 OK, returns search results

3. **Get Single Event Details**
```bash
curl -s -X GET "http://localhost:8080/api/v1/analytics/events/$EVENT_ID" \
  -H "Authorization: Bearer $TOKEN" | jq '.'
```

Expected: 200 OK, returns event details

4. **Get Related Events**
```bash
curl -s -X GET "http://localhost:8080/api/v1/analytics/events/$EVENT_ID/related?limit=10" \
  -H "Authorization: Bearer $TOKEN" | jq '.'
```

Expected: 200 OK, returns related events

5. **Export Events to CSV**
```bash
curl -s -X POST http://localhost:8080/api/v1/analytics/events/export \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "format": "csv",
    "filters": {
      "category": "user_action",
      "startDate": "2024-01-01T00:00:00Z",
      "endDate": "2024-01-31T23:59:59Z"
    },
    "includeColumns": ["id", "category", "eventType", "userId", "createdAt"]
  }' > events_export.csv

wc -l events_export.csv
```

Expected: CSV file created with event data

6. **Export Events to JSON**
```bash
curl -s -X POST http://localhost:8080/api/v1/analytics/events/export \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "format": "json",
    "filters": {
      "category": "user_action"
    }
  }' > events_export.json

jq '.data | length' events_export.json
```

Expected: JSON file created with event data

**Validation:**
- Events listed successfully
- Search with filters works
- Individual event retrieved
- Related events found
- CSV export generated
- JSON export generated

---

## Scenario 4: Set Up Webhooks and Monitor Deliveries

**Goal:** Create webhooks, test delivery, and monitor history

**Prerequisites:**
- Valid JWT token in `$TOKEN`
- Webhook endpoint available (can use webhook.site for testing)

**Steps:**

1. **Create New Webhook**
```bash
WEBHOOK=$(curl -s -X POST http://localhost:8080/api/v1/analytics/webhooks \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "url": "https://webhook.site/your-unique-id",
    "events": ["event.created", "dashboard.updated", "report.generated"],
    "headers": {
      "X-Custom-Header": "analytics-webhook"
    },
    "enabled": true
  }')

WEBHOOK_ID=$(echo $WEBHOOK | jq -r '.id')
echo "Created webhook: $WEBHOOK_ID"
```

Expected: 201 Created, returns webhook with secret

2. **Validate Webhook Configuration**
```bash
curl -s -X POST http://localhost:8080/api/v1/analytics/validate/webhook \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "url": "https://webhook.site/your-unique-id",
    "events": ["event.created"]
  }' | jq '.'
```

Expected: 200 OK, validation passed

3. **Get Webhook Details**
```bash
curl -s -X GET "http://localhost:8080/api/v1/analytics/webhooks/$WEBHOOK_ID" \
  -H "Authorization: Bearer $TOKEN" | jq '.'
```

Expected: 200 OK, returns webhook details

4. **Test Webhook Delivery**
```bash
TEST_RESULT=$(curl -s -X POST "http://localhost:8080/api/v1/analytics/webhooks/$WEBHOOK_ID/test" \
  -H "Authorization: Bearer $TOKEN")

echo $TEST_RESULT | jq '.'
```

Expected: 200 OK, test delivery sent

5. **Check Webhook Delivery History**
```bash
curl -s -X GET "http://localhost:8080/api/v1/analytics/webhooks/$WEBHOOK_ID/delivery-history?limit=20" \
  -H "Authorization: Bearer $TOKEN" | jq '.'
```

Expected: 200 OK, shows delivery attempts

6. **Update Webhook**
```bash
curl -s -X PUT "http://localhost:8080/api/v1/analytics/webhooks/$WEBHOOK_ID" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "events": ["event.created", "event.updated", "dashboard.created"],
    "enabled": true
  }' | jq '.'
```

Expected: 200 OK, webhook updated

7. **Replay Failed Deliveries**
```bash
curl -s -X POST "http://localhost:8080/api/v1/analytics/webhooks/$WEBHOOK_ID/replay" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "limit": 10,
    "statusFilter": "failed"
  }' | jq '.'
```

Expected: 202 Accepted, replay job started

**Validation:**
- Webhook created successfully
- Webhook configuration validated
- Test delivery sent
- Delivery history accessible
- Webhook can be updated
- Failed deliveries can be replayed

---

## Scenario 5: Configure Storage and Retention

**Goal:** Configure storage, test connection, and set retention policies

**Prerequisites:**
- Valid JWT token in `$TOKEN`
- S3 or compatible storage credentials available

**Steps:**

1. **Get Current Storage Configuration**
```bash
curl -s -X GET http://localhost:8080/api/v1/analytics/config/storage \
  -H "Authorization: Bearer $TOKEN" | jq '.'
```

Expected: 200 OK, returns current storage config

2. **Validate Storage Configuration**
```bash
curl -s -X POST http://localhost:8080/api/v1/analytics/validate/storage \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "provider": "s3",
    "endpoint": "s3.amazonaws.com",
    "bucket": "analytics-backup",
    "region": "us-east-1"
  }' | jq '.'
```

Expected: 200 OK, validation passed or errors listed

3. **Update Storage Configuration**
```bash
curl -s -X PUT http://localhost:8080/api/v1/analytics/config/storage \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "provider": "s3",
    "endpoint": "s3.amazonaws.com",
    "bucket": "analytics-backup",
    "region": "us-east-1",
    "retentionDays": 90
  }' | jq '.'
```

Expected: 200 OK, storage config updated

4. **Test Storage Connection**
```bash
TEST=$(curl -s -X POST http://localhost:8080/api/v1/analytics/config/storage/test \
  -H "Authorization: Bearer $TOKEN")

echo $TEST | jq '.'
```

Expected: 200 OK if connected, or error message

5. **Get Retention Policy**
```bash
curl -s -X GET http://localhost:8080/api/v1/analytics/config/retention \
  -H "Authorization: Bearer $TOKEN" | jq '.'
```

Expected: 200 OK, returns retention settings

6. **Update Retention Policy**
```bash
curl -s -X PUT http://localhost:8080/api/v1/analytics/config/retention \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "hotDataDays": 30,
    "warmDataDays": 90,
    "coldDataDays": 365,
    "archiveEnabled": true,
    "archiveInterval": "monthly"
  }' | jq '.'
```

Expected: 200 OK, retention policy updated

7. **Validate Retention Policy**
```bash
curl -s -X POST http://localhost:8080/api/v1/analytics/validate/retention \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "hotDataDays": 30,
    "warmDataDays": 90,
    "coldDataDays": 365
  }' | jq '.'
```

Expected: 200 OK, validation passed

8. **Trigger Data Archival**
```bash
ARCHIVE=$(curl -s -X POST http://localhost:8080/api/v1/analytics/config/retention/archive \
  -H "Authorization: Bearer $TOKEN")

JOB_ID=$(echo $ARCHIVE | jq -r '.jobId')
echo "Archive job started: $JOB_ID"
```

Expected: 202 Accepted, returns job ID

9. **Get Archive History**
```bash
curl -s -X GET "http://localhost:8080/api/v1/analytics/config/retention/archive-history?limit=10" \
  -H "Authorization: Bearer $TOKEN" | jq '.'
```

Expected: 200 OK, shows archive operations

**Validation:**
- Storage config retrieved and updated
- Storage connection tested
- Retention policy configured
- Policy validation works
- Archival can be triggered
- Archive history available

---

## Scenario 6: Generate and Download Reports

**Goal:** Create report template, generate, and download

**Prerequisites:**
- Valid JWT token in `$TOKEN`
- Saved query ID from previous scenario

**Steps:**

1. **List Existing Reports**
```bash
curl -s -X GET "http://localhost:8080/api/v1/analytics/reports?limit=10" \
  -H "Authorization: Bearer $TOKEN" | jq '.'
```

Expected: 200 OK, returns reports list

2. **Create Report Template**
```bash
REPORT=$(curl -s -X POST http://localhost:8080/api/v1/analytics/reports \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Weekly Performance Report",
    "description": "Weekly analytics summary",
    "queryId": "'$QUERY_ID'",
    "format": "pdf",
    "schedule": "weekly",
    "recipients": ["analyst@company.com"]
  }')

REPORT_ID=$(echo $REPORT | jq -r '.id')
echo "Created report: $REPORT_ID"
```

Expected: 201 Created, returns report template

3. **Get Report Details**
```bash
curl -s -X GET "http://localhost:8080/api/v1/analytics/reports/$REPORT_ID" \
  -H "Authorization: Bearer $TOKEN" | jq '.'
```

Expected: 200 OK, returns report details

4. **Generate Report Now (Manual Trigger)**
```bash
GEN=$(curl -s -X POST "http://localhost:8080/api/v1/analytics/reports/$REPORT_ID/generate" \
  -H "Authorization: Bearer $TOKEN")

echo $GEN | jq '.'
```

Expected: 202 Accepted, report generation started

5. **Update Report Configuration**
```bash
curl -s -X PUT "http://localhost:8080/api/v1/analytics/reports/$REPORT_ID" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "schedule": "daily",
    "format": "xlsx",
    "recipients": ["analyst@company.com", "manager@company.com"]
  }' | jq '.'
```

Expected: 200 OK, report updated

6. **Download Generated Report**
```bash
curl -s -X GET "http://localhost:8080/api/v1/analytics/reports/$REPORT_ID/download" \
  -H "Authorization: Bearer $TOKEN" \
  -o report_download.pdf

file report_download.pdf
ls -lh report_download.pdf
```

Expected: Report file downloaded (PDF, XLSX, or other format)

7. **List All Reports**
```bash
curl -s -X GET "http://localhost:8080/api/v1/analytics/reports?page=1&limit=20" \
  -H "Authorization: Bearer $TOKEN" | jq '.data | length'
```

Expected: 200 OK, shows report count

8. **Delete Report**
```bash
curl -s -X DELETE "http://localhost:8080/api/v1/analytics/reports/$REPORT_ID" \
  -H "Authorization: Bearer $TOKEN" -w "\nStatus: %{http_code}\n"
```

Expected: 204 No Content

**Validation:**
- Report created successfully
- Report details retrieved
- Report generation triggered
- Report configuration updated
- Report downloaded
- Report deleted

---

## Scenario 7: Manage Sync Configuration

**Goal:** Configure and monitor data synchronization

**Prerequisites:**
- Valid JWT token in `$TOKEN`

**Steps:**

1. **Get Current Sync Configuration**
```bash
curl -s -X GET http://localhost:8080/api/v1/analytics/config/sync \
  -H "Authorization: Bearer $TOKEN" | jq '.'
```

Expected: 200 OK, returns sync settings

2. **Validate Sync Configuration**
```bash
curl -s -X POST http://localhost:8080/api/v1/analytics/validate/sync \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "interval": "15m",
    "timeout": "30m",
    "retryAttempts": 3
  }' | jq '.'
```

Expected: 200 OK, validation passed

3. **Update Sync Configuration**
```bash
curl -s -X PUT http://localhost:8080/api/v1/analytics/config/sync \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "enabled": true,
    "interval": "15m",
    "timeout": "30m",
    "retryAttempts": 5
  }' | jq '.'
```

Expected: 200 OK, sync config updated

4. **Trigger Manual Sync**
```bash
SYNC=$(curl -s -X POST http://localhost:8080/api/v1/analytics/config/sync/trigger \
  -H "Authorization: Bearer $TOKEN")

SYNC_ID=$(echo $SYNC | jq -r '.syncId')
echo "Sync started: $SYNC_ID"
```

Expected: 202 Accepted, returns sync job ID

5. **Monitor Sync Progress**
```bash
for i in {1..5}; do
  sleep 2
  curl -s -X GET "http://localhost:8080/api/v1/analytics/config/sync/$SYNC_ID/status" \
    -H "Authorization: Bearer $TOKEN" | jq '{status, recordsSynced, errors}'
done
```

Expected: 200 OK, status progresses from "running" to "completed"

**Validation:**
- Sync configuration retrieved and updated
- Sync can be triggered manually
- Sync status can be monitored
- Validation works before updating config

---

## Scenario 8: User Preferences and Favorites

**Goal:** Manage user-specific settings and dashboard favorites

**Prerequisites:**
- Valid JWT token in `$TOKEN`
- Dashboard ID from earlier scenario

**Steps:**

1. **Get Current User Preferences**
```bash
curl -s -X GET http://localhost:8080/api/v1/analytics/user/preferences \
  -H "Authorization: Bearer $TOKEN" | jq '.'
```

Expected: 200 OK, returns user preferences

2. **Update User Preferences**
```bash
curl -s -X PUT http://localhost:8080/api/v1/analytics/user/preferences \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "theme": "dark",
    "dateFormat": "YYYY-MM-DD",
    "timeZone": "America/New_York",
    "defaultDashboard": "'$DASHBOARD_ID'",
    "notifications": {
      "emailAlerts": true,
      "webhookAlerts": true
    }
  }' | jq '.'
```

Expected: 200 OK, preferences updated

3. **Add Dashboard to Favorites**
```bash
curl -s -X POST "http://localhost:8080/api/v1/analytics/user/favorites/dashboards/$DASHBOARD_ID" \
  -H "Authorization: Bearer $TOKEN" | jq '.'
```

Expected: 200 OK, dashboard added to favorites

4. **Get Updated Preferences (Verify Changes)**
```bash
curl -s -X GET http://localhost:8080/api/v1/analytics/user/preferences \
  -H "Authorization: Bearer $TOKEN" | jq '{theme, timeZone, defaultDashboard}'
```

Expected: 200 OK, shows updated preferences

5. **Remove Dashboard from Favorites**
```bash
curl -s -X DELETE "http://localhost:8080/api/v1/analytics/user/favorites/dashboards/$DASHBOARD_ID" \
  -H "Authorization: Bearer $TOKEN" -w "\nStatus: %{http_code}\n"
```

Expected: 204 No Content

**Validation:**
- User preferences retrieved
- Preferences can be updated
- Dashboards can be marked as favorite
- Preferences persist
- Favorites can be removed

---

## Scenario 9: Error Handling and Edge Cases

**Goal:** Verify proper error handling

**Prerequisites:**
- Valid JWT token in `$TOKEN`

**Steps:**

1. **Missing Authentication Token**
```bash
curl -s -X GET http://localhost:8080/api/v1/analytics/dashboards | jq '.'
```

Expected: 401 Unauthorized

2. **Invalid Dashboard ID**
```bash
curl -s -X GET http://localhost:8080/api/v1/analytics/dashboards/invalid_id \
  -H "Authorization: Bearer $TOKEN" | jq '.'
```

Expected: 404 Not Found

3. **Malformed Request Body**
```bash
curl -s -X POST http://localhost:8080/api/v1/analytics/dashboards \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{invalid json' | jq '.'
```

Expected: 400 Bad Request

4. **Invalid Query (SQL Error)**
```bash
curl -s -X POST http://localhost:8080/api/v1/analytics/queries \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Bad Query",
    "sql": "SELECT * FROM nonexistent_table"
  }' | jq '.'
```

Expected: 400 Bad Request with error details

5. **Missing Required Field**
```bash
curl -s -X POST http://localhost:8080/api/v1/analytics/dashboards \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"description": "Missing name field"}' | jq '.'
```

Expected: 400 Bad Request

6. **Rate Limit Test (Optional)**
```bash
for i in {1..150}; do
  curl -s -X GET http://localhost:8080/api/v1/analytics/dashboards \
    -H "Authorization: Bearer $TOKEN" > /dev/null
done

curl -s -X GET http://localhost:8080/api/v1/analytics/dashboards \
  -H "Authorization: Bearer $TOKEN" | jq '.error'
```

Expected: Eventually 429 Too Many Requests

**Validation:**
- 401 for missing auth
- 404 for non-existent resources
- 400 for invalid input
- Proper error messages provided
- Rate limiting enforced

---

## Testing Checklist

- [ ] All health endpoints return 200 OK
- [ ] Authentication required for protected endpoints
- [ ] Dashboard CRUD operations work
- [ ] Query creation and execution work
- [ ] Report generation and download work
- [ ] Events can be searched and exported
- [ ] Webhooks can be created and tested
- [ ] Configuration can be read and updated
- [ ] Retention and archival policies work
- [ ] User preferences persist
- [ ] Sync can be triggered and monitored
- [ ] Error responses are properly formatted
- [ ] Pagination works on list endpoints
- [ ] Rate limiting is enforced
- [ ] Timestamps are in ISO 8601 format

---

## Performance Baselines

| Operation | Expected Time | Acceptance Criteria |
|-----------|----------------|-------------------|
| Health check | <10ms | <50ms |
| List endpoints | 50-200ms | <1000ms |
| Get single resource | 20-100ms | <500ms |
| Create resource | 100-500ms | <2000ms |
| Execute query | 200-5000ms | <30000ms |
| Export data | 500-5000ms | <60000ms |
| Generate report | 30-300s | <600s |

---

**Last Updated:** January 2024 | **Test Scenarios:** 9 | **Total Test Steps:** 60+
