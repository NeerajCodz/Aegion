# Analytics Webhooks Guide

**Version:** 1.0  
**Last Updated:** 2026-04-24  
**Module:** `modules/analytics`

## Overview

Webhooks enable real-time event notifications to external systems. When events matching your filters occur, Aegion automatically sends HTTP POST requests to your configured webhook URLs.

**Use Cases:**
- Alert external systems when specific events occur
- Trigger downstream processes (reporting, ML pipelines)
- Integrate with chat/notification systems (Slack, Teams)
- Archive events to external storage
- Real-time event replication

---

## Webhook Configuration

### Creating a Webhook

#### REST API

```bash
curl -X POST \
  -H "Authorization: Bearer ${AEGION_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{
    "url": "https://your-api.example.com/webhooks/events",
    "events": ["event.created", "event.updated"],
    "filters": {
      "categories": ["user_action", "system_event"],
      "eventTypes": ["page_view", "click"],
      "minSeverity": "INFO"
    },
    "retryPolicy": {
      "maxRetries": 3,
      "backoffMultiplier": 2.0
    },
    "headers": [
      {
        "name": "X-Custom-Header",
        "value": "my-value"
      }
    ],
    "isActive": true
  }' \
  http://localhost:8080/api/v1/analytics/webhooks
```

Response:
```json
{
  "id": "wh_abc123",
  "url": "https://your-api.example.com/webhooks/events",
  "events": ["event.created", "event.updated"],
  "filters": {
    "categories": ["user_action", "system_event"]
  },
  "isActive": true,
  "createdAt": "2026-04-24T10:30:00Z",
  "secret": "example-webhook-secret"
}
```

**Save the `secret`** - You'll need it to verify webhook signatures.

#### GraphQL Mutation

```graphql
mutation RegisterWebhook {
  createWebhook(input: {
    url: "https://your-api.example.com/webhooks/events"
    events: ["event.created"]
    filters: {
      categories: ["user_action"]
    }
    retryPolicy: {
      maxRetries: 3
      backoffMultiplier: 2.0
    }
    isActive: true
  }) {
    webhook {
      id
      url
      secret
    }
    success
    errors
  }
}
```

### Webhook Properties

| Property | Type | Description |
|----------|------|-------------|
| `url` | String | HTTPS endpoint (required) |
| `events` | Array | Event types to subscribe to |
| `filters` | Object | Filter rules (category, eventType, severity) |
| `retryPolicy` | Object | Retry configuration |
| `headers` | Array | Custom HTTP headers to send |
| `isActive` | Boolean | Enable/disable without deleting |
| `secret` | String | HMAC signing key (auto-generated) |

---

## Event Types

### Available Events

```
event.created       - New event ingested
event.updated       - Event data modified
event.deleted       - Event deleted
dashboard.created   - Dashboard created
dashboard.updated   - Dashboard modified
dashboard.deleted   - Dashboard removed
query.executed      - Query run manually
webhook.test        - Test webhook triggered
```

### Subscribe to Multiple Events

```json
{
  "events": [
    "event.created",
    "event.updated",
    "event.deleted"
  ]
}
```

---

## Filtering

### Category Filter

```json
{
  "filters": {
    "categories": ["user_action", "system_event"]
  }
}
```

Only events with these categories trigger the webhook.

### Event Type Filter

```json
{
  "filters": {
    "eventTypes": ["page_view", "click", "form_submit"]
  }
}
```

### Severity Filter

```json
{
  "filters": {
    "minSeverity": "WARN"
  }
}
```

Levels: `DEBUG < INFO < WARN < ERROR < CRITICAL`

### Combined Filters

```json
{
  "filters": {
    "categories": ["user_action"],
    "eventTypes": ["page_view"],
    "minSeverity": "INFO"
  }
}
```

Event must match ALL filters to trigger.

---

## Webhook Payload

### Standard Payload Format

```json
{
  "id": "wh_delivery_123",
  "webhookId": "wh_abc123",
  "event": "event.created",
  "data": {
    "id": "evt_xyz",
    "category": "user_action",
    "eventType": "page_view",
    "data": {
      "page": "/dashboard",
      "referrer": "/home"
    },
    "userId": "user_123",
    "sessionId": "sess_456",
    "createdAt": "2026-04-24T10:30:00Z"
  },
  "timestamp": "2026-04-24T10:30:01Z",
  "delivery": {
    "attempt": 1,
    "maxAttempts": 3,
    "url": "https://your-api.example.com/webhooks/events"
  }
}
```

### Payload Types

#### Event Created
```json
{
  "event": "event.created",
  "data": {
    "id": "evt_123",
    "category": "user_action",
    "eventType": "page_view",
    "userId": "user_123",
    ...
  }
}
```

#### Dashboard Updated
```json
{
  "event": "dashboard.updated",
  "data": {
    "id": "dash_456",
    "name": "Sales Dashboard",
    "updatedBy": "user_789",
    "updatedAt": "2026-04-24T10:30:00Z"
  }
}
```

#### Query Executed
```json
{
  "event": "query.executed",
  "data": {
    "queryId": "q_123",
    "executionTime": 250,
    "rowsReturned": 1500,
    "executedBy": "user_456"
  }
}
```

---

## Request & Response

### HTTP Request Details

- **Method:** POST
- **Headers:**
  ```
  Content-Type: application/json
  User-Agent: Aegion-Webhook/1.0
  X-Webhook-ID: wh_abc123
  X-Delivery-ID: wh_delivery_123
  X-Webhook-Signature: sha256=abc123...
  X-Timestamp: 2026-04-24T10:30:01Z
  ```
- **Body:** Webhook payload (JSON)
- **Timeout:** 30 seconds

### Expected Response

**Success (200-299):**
```http
HTTP/1.1 200 OK
Content-Type: application/json

{ "received": true }
```

**Retry on Failure (5xx, timeout):**
```http
HTTP/1.1 500 Internal Server Error

{error": "Database error"}
```

---

## Signature Verification (HMAC-SHA256)

All webhooks are signed with HMAC-SHA256. Verify the signature to ensure authenticity.

### Verification Process

```python
import hmac
import hashlib
import json
from flask import request

WEBHOOK_SECRET = "example-webhook-secret"

@app.route('/webhooks/events', methods=['POST'])
def handle_webhook():
    # 1. Get signature from header
    signature = request.headers.get('X-Webhook-Signature', '')
    
    # 2. Get raw request body
    body = request.get_data(as_text=True)
    
    # 3. Compute expected signature
    expected_signature = "sha256=" + hmac.new(
        WEBHOOK_SECRET.encode(),
        body.encode(),
        hashlib.sha256
    ).hexdigest()
    
    # 4. Verify signature (timing-safe comparison)
    if not hmac.compare_digest(signature, expected_signature):
        return {"error": "Unauthorized"}, 401
    
    # 5. Parse and process webhook
    payload = json.loads(body)
    process_event(payload)
    
    return {"received": True}, 200
```

### Verification in JavaScript

```javascript
const crypto = require('crypto');

const WEBHOOK_SECRET = 'example-webhook-secret';

app.post('/webhooks/events', (req, res) => {
  // 1. Get signature
  const signature = req.headers['x-webhook-signature'];
  
  // 2. Get raw body (must not be parsed JSON yet)
  const body = JSON.stringify(req.body);
  
  // 3. Compute expected signature
  const expected = 'sha256=' + crypto
    .createHmac('sha256', WEBHOOK_SECRET)
    .update(body)
    .digest('hex');
  
  // 4. Verify
  if (!crypto.timingSafeEqual(signature, expected)) {
    return res.status(401).json({ error: 'Unauthorized' });
  }
  
  // 5. Process
  processEvent(req.body);
  res.json({ received: true });
});
```

### Verification in Go

```go
import (
  "crypto/hmac"
  "crypto/sha256"
  "encoding/hex"
  "io"
  "net/http"
)

const WEBHOOK_SECRET = "example-webhook-secret"

func HandleWebhook(w http.ResponseWriter, r *http.Request) {
  // 1. Get signature
  signature := r.Header.Get("X-Webhook-Signature")
  
  // 2. Read body
  body, _ := io.ReadAll(r.Body)
  
  // 3. Compute expected signature
  mac := hmac.New(sha256.New, []byte(WEBHOOK_SECRET))
  mac.Write(body)
  expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
  
  // 4. Verify
  if !hmac.Equal([]byte(signature), []byte(expected)) {
    http.Error(w, "Unauthorized", http.StatusUnauthorized)
    return
  }
  
  // 5. Process
  ProcessEvent(body)
  json.NewEncoder(w).Encode(map[string]bool{"received": true})
}
```

---

## Retry Logic

### Exponential Backoff

Default retry policy:
```
Attempt 1: Immediate
Attempt 2: 5 seconds delay
Attempt 3: 30 seconds delay (5 * 2^1)
Attempt 4: 5 minutes delay (5 * 2^2)
Failed:    Move to Dead Letter Queue
```

### Custom Retry Policy

```json
{
  "retryPolicy": {
    "maxRetries": 5,
    "backoffMultiplier": 3.0,
    "baseDelaySeconds": 10
  }
}
```

Calculated delays:
- Attempt 1: Immediate
- Attempt 2: 10 seconds
- Attempt 3: 30 seconds (10 * 3)
- Attempt 4: 90 seconds (10 * 3^2)
- Attempt 5: 270 seconds (10 * 3^3)
- Attempt 6: Failed → DLQ

### Retrieve Delivery History

```bash
curl -H "Authorization: Bearer token" \
  http://localhost:8080/api/v1/analytics/webhooks/{webhookId}/deliveries

Response:
{
  "deliveries": [
    {
      "id": "wh_del_1",
      "status": "success",
      "statusCode": 200,
      "attempt": 1,
      "sentAt": "2026-04-24T10:30:01Z",
      "responseTime": 123
    },
    {
      "id": "wh_del_2",
      "status": "failed",
      "statusCode": 500,
      "attempt": 2,
      "sentAt": "2026-04-24T10:30:35Z",
      "nextRetryAt": "2026-04-24T10:35:35Z"
    }
  ]
}
```

---

## Dead Letter Queue (DLQ)

Failed deliveries after max retries go to the DLQ.

### Access DLQ Events

```bash
curl -H "Authorization: Bearer token" \
  http://localhost:8080/api/v1/analytics/webhooks/{webhookId}/dlq

Response:
{
  "dlqEvents": [
    {
      "id": "dlq_123",
      "webhookId": "wh_abc123",
      "event": {
        "id": "evt_xyz",
        ...
      },
      "lastError": "Connection timeout",
      "failureCount": 3,
      "firstFailureAt": "2026-04-24T10:30:00Z",
      "lastFailureAt": "2026-04-24T10:35:00Z"
    }
  ]
}
```

### Replay from DLQ

```bash
curl -X POST \
  -H "Authorization: Bearer token" \
  http://localhost:8080/api/v1/analytics/webhooks/deliveries/{deliveryId}/replay

Response:
{
  "status": "requeued",
  "nextAttempt": "2026-04-24T10:40:00Z"
}
```

---

## Testing Webhooks

### Manual Test

```bash
curl -X POST \
  -H "Authorization: Bearer token" \
  http://localhost:8080/api/v1/analytics/webhooks/{webhookId}/test

Response:
{
  "status": "success",
  "statusCode": 200,
  "responseTime": 123,
  "payload": {
    "event": "webhook.test",
    "data": { "test": true }
  }
}
```

### Test in Admin SPA

1. Navigate to **Analytics → Webhooks**
2. Click webhook to edit
3. Click **Send Test** button
4. View response in **Delivery History**

### Monitor in Real-Time

```bash
# Watch webhook deliveries
curl -H "Authorization: Bearer token" \
  "http://localhost:8080/api/v1/analytics/webhooks/{webhookId}/deliveries?limit=50"
```

---

## Best Practices

### 1. Verify Signatures
Always verify HMAC signatures to ensure requests are from Aegion.

### 2. Idempotent Endpoints
Make webhook endpoints idempotent - handle duplicate deliveries gracefully:
```python
# Store delivery IDs in database
if DeliveryRecord.exists(delivery_id):
  return 200  # Already processed
DeliveryRecord.create(delivery_id)
process_event(payload)
```

### 3. Fast Response Times
Return `200 OK` immediately, process event asynchronously:
```python
@app.route('/webhooks/events', methods=['POST'])
def handle_webhook():
  # Verify and validate
  payload = verify_and_parse(request)
  
  # Queue for async processing
  background_job.enqueue('process_event', payload)
  
  # Return immediately
  return {"received": True}, 200
```

### 4. Handle Retries
Expect duplicate deliveries with different attempt numbers:
```python
delivery = payload.get('delivery', {})
attempt = delivery.get('attempt', 1)
max_attempts = delivery.get('maxAttempts', 3)
```

### 5. Monitor Webhook Health

```bash
# Get webhook statistics
curl -H "Authorization: Bearer token" \
  http://localhost:8080/api/v1/analytics/webhooks/{webhookId}

Response:
{
  "id": "wh_abc123",
  "stats": {
    "totalDeliveries": 15234,
    "successCount": 15200,
    "failureCount": 34,
    "dlqCount": 5,
    "averageLatencyMs": 125,
    "lastDeliveryAt": "2026-04-24T10:30:00Z"
  }
}
```

---

## Troubleshooting

### Webhook Not Triggering

1. **Check webhook is active:**
   ```bash
   curl http://localhost:8080/api/v1/analytics/webhooks/{webhookId}
   # isActive should be true
   ```

2. **Check filters:**
   - Event may not match category/eventType/severity filters
   - Test with no filters first

3. **Send test webhook:**
   ```bash
   curl -X POST http://localhost:8080/api/v1/analytics/webhooks/{webhookId}/test
   ```

### Delivery Failures

1. **Check delivery history:**
   ```bash
   curl http://localhost:8080/api/v1/analytics/webhooks/{webhookId}/deliveries
   ```

2. **Common issues:**
   - **Timeout:** URL takes > 30 seconds to respond
   - **Connection refused:** Endpoint not reachable
   - **500 error:** Your endpoint returned error
   - **Signature mismatch:** Verification failing on your side

3. **Check DLQ:**
   ```bash
   curl http://localhost:8080/api/v1/analytics/webhooks/{webhookId}/dlq
   ```

---

## Related Documentation

- [API Reference](./api.md)
- [Security](./security.md)
- [Integration Guide](./integration.md)
- [Troubleshooting](./troubleshooting.md)
