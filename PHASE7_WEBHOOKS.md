# Phase 7 - Webhook System Implementation

## Overview

The Aegion Webhook System provides real-time event notifications to external systems through HTTP callbacks. Users can subscribe to analytics events and receive signed payloads with delivery guarantees.

## Components

### 1. Core Modules

#### Manager (`manager.go`)
Orchestrates webhook registration, event matching, and delivery coordination.

**Key Methods:**
- `RegisterWebhook()` - Create a new webhook subscription
- `UpdateWebhook()` - Modify webhook configuration
- `DeleteWebhook()` - Deactivate a webhook
- `PublishEvent()` - Publish an event to matching webhooks
- `DispatchEvent()` - Queue deliveries for webhooks matching an event
- `TestWebhook()` - Send a test event to a webhook

#### Store (`store.go`)
Persists webhooks, delivery history, and DLQ events to DuckDB.

**Tables:**
- `webhooks` - Webhook subscriptions
- `webhook_deliveries` - Delivery attempt history
- `webhook_dlq` - Dead letter queue for failed events

#### Dispatcher (`dispatcher.go`)
Handles HTTP delivery of webhook payloads with timeout and error handling.

**Features:**
- HTTP POST requests to webhook URLs
- Response body capture (1KB limit)
- Timeout configuration (default 30s)
- Retryable error classification

#### Matcher (`matcher.go`)
Matches events against webhook filters using glob patterns and custom expressions.

**Filtering Options:**
- Event type patterns: `auth.*`, `user.created`
- Category matching: `authentication`, `user`, `session`
- Custom filters: `$and`, `$or`, `$not`, `$eq`, `$in`, `$contains`, `$exists`

#### Signature (`signature.go`)
Generates and verifies HMAC-SHA256 signatures for webhook payloads.

**Payload Structure:**
```json
{
  "id": "webhook-event-12345",
  "timestamp": "2026-04-23T20:10:37Z",
  "event_type": "authentication.login_failed",
  "category": "authentication",
  "data": {...},
  "attempts": 1,
  "signatures": {
    "sha256": "sha256=abc123..."
  }
}
```

#### Retry Policy (`retry.go`)
Implements exponential backoff with jitter and circuit breaker pattern.

**Backoff Sequence:**
- Attempt 1: 1s
- Attempt 2: 2s
- Attempt 3: 4s
- Attempt 4: 8s
- Attempt 5: 16s

**Circuit Breaker:**
- Disables webhook after 5 consecutive failures
- Prevents repeated hammering of bad endpoints
- Moves events to DLQ

#### Delivery Queue (`queue.go`)
In-memory job queue for webhook deliveries.

**Features:**
- Non-blocking enqueue/dequeue
- Configurable capacity
- Timeout support

#### Delivery Worker (`worker.go`)
Processes webhook delivery jobs with retry logic.

**Workflow:**
1. Dequeue job
2. Fetch webhook configuration
3. Attempt delivery
4. On success: reset failure count, remove from queue
5. On retryable failure: calculate backoff, re-queue
6. On non-retryable failure: move to DLQ, increment failure count

### 2. REST API Endpoints

#### Webhook Management
```
POST   /api/v1/analytics/webhooks              - Register webhook
GET    /api/v1/analytics/webhooks              - List user's webhooks
GET    /api/v1/analytics/webhooks/:id          - Get webhook details
PUT    /api/v1/analytics/webhooks/:id          - Update webhook
DELETE /api/v1/analytics/webhooks/:id          - Deactivate webhook
POST   /api/v1/analytics/webhooks/:id/test     - Send test event
```

#### Delivery History
```
GET  /api/v1/analytics/webhooks/:id/deliveries           - Get delivery history
POST /api/v1/analytics/webhooks/deliveries/:id/replay    - Replay event
```

### 3. Registration Request Format

```json
{
  "url": "https://example.com/webhooks",
  "event_filter": {
    "event_types": ["auth.*", "user.created"],
    "categories": ["authentication", "user"],
    "custom_filter": {
      "$and": [
        {
          "user_id": {
            "$exists": true
          }
        },
        {
          "status": {
            "$ne": "guest"
          }
        }
      ]
    }
  },
  "secret": "whsec_1234567890abcdef",
  "active": true
}
```

### 4. Webhook Delivery Headers

```
X-Aegion-Event-ID: webhook-event-12345
X-Aegion-Event-Type: authentication.login_failed
X-Aegion-Delivery-ID: delivery-67890
X-Aegion-Signature: sha256=abc123...
X-Aegion-Timestamp: 2026-04-23T20:10:37Z
Content-Type: application/json
```

### 5. Configuration (aegion.yaml)

```yaml
analytics:
  webhooks:
    enabled: true
    max_per_user: 50                    # Webhooks per user
    max_retries: 5                      # Retry attempts
    retry_backoff_base_ms: 1000         # Initial backoff (ms)
    timeout_seconds: 30                 # HTTP timeout
    batch_size: 100                     # Queue capacity
    worker_threads: 10                  # Concurrent workers
    store_delivery_history_days: 30     # History retention
```

## Event Matching Examples

### Example 1: All auth events
```json
{
  "event_types": ["auth.*"]
}
```
Matches: `auth.login`, `auth.logout`, `auth.mfa_challenge`

### Example 2: Specific categories
```json
{
  "categories": ["user", "session"]
}
```
Matches: Any event in user or session category

### Example 3: Advanced custom filter
```json
{
  "event_types": ["auth.*"],
  "custom_filter": {
    "$or": [
      {
        "status": {"$eq": "failed"}
      },
      {
        "attempts": {"$in": [3, 4, 5]}
      }
    ]
  }
}
```
Matches: Auth events that failed OR had 3-5 attempts

## Signature Verification (Client-side)

Receivers should verify webhook signatures:

```go
import "crypto/hmac"
import "crypto/sha256"
import "fmt"

func VerifySignature(payload []byte, signature string, secret string) bool {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write(payload)
	expected := fmt.Sprintf("sha256=%x", h.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}
```

## Delivery Guarantees

- **At-least-once delivery**: Events are retried with exponential backoff
- **Non-blocking publishing**: Sync manager publishes immediately, delivery is async
- **Ordered per webhook**: Events for a webhook are processed sequentially
- **Circuit breaker**: Disabled webhooks prevent cascading failures

## Monitoring

### Webhook Health
- Failure count per webhook
- Last delivery timestamp
- Active/inactive status
- Circuit breaker state

### Delivery Metrics
- Success/failure rates
- Retry counts
- Average latency
- Response status codes

### DLQ Monitoring
- Count of failed events
- Reason for failure
- Event replay capability

## Integration with Sync Manager

The sync manager publishes events through the webhook manager:

```go
// In sync manager event publishing
if webhookMgr != nil {
	err := webhookMgr.DispatchEvent(ctx, eventID, eventType, category, data, webhooks)
	// Non-blocking: error doesn't affect sync
}
```

## Testing

### Unit Tests Cover:
- Event filtering with various patterns
- Signature generation and verification
- Retry backoff calculation
- Rate limiting
- Queue operations
- Webhook validation

### Integration Tests Cover:
- End-to-end webhook registration
- Event matching and filtering
- Delivery and retry flow
- DLQ transitions
- Test event delivery

## Error Handling

### Retryable Errors
- Network timeouts
- Connection refused
- 5xx server errors
- 408 Request Timeout
- 429 Too Many Requests

### Non-Retryable Errors
- 400 Bad Request
- 401 Unauthorized
- 403 Forbidden
- 404 Not Found
- Validation errors

## Security Considerations

1. **HTTPS Enforcement**: Webhooks require HTTPS (except localhost)
2. **Secret Management**: Secrets stored encrypted in database
3. **Signature Verification**: All payloads signed with secret
4. **Rate Limiting**: Per-user webhook creation limits
5. **Timeout Protection**: 30s timeout prevents hanging requests
6. **Circuit Breaker**: Prevents repeated failures

## Performance Characteristics

- **Throughput**: 10 concurrent workers × batch rate
- **Latency**: < 100ms for delivery job queueing
- **Memory**: O(queue_size) for pending jobs
- **Retry Memory**: Minimal with jitter-based backoff

## Future Enhancements

1. **Redis-backed Queue**: For distributed deployments
2. **Webhook Templates**: Pre-configured event subscriptions
3. **Webhook Signatures**: Multiple signature algorithms
4. **Batch Deliveries**: Group events in batches
5. **Conditional Logic**: More complex filter expressions
6. **Webhook Webhooks**: Notify on webhook delivery status
7. **Analytics**: Webhook usage metrics and dashboards
