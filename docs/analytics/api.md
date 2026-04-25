# Analytics API Reference

**Version:** 1.0  
**Last Updated:** 2026-04-24  
**Module:** `modules/analytics`

## Overview

The Aegion analytics system provides three complementary API layers for different integration needs:

| API | Protocol | Best For | Authentication |
|-----|----------|----------|----------------|
| **REST** | HTTP/JSON | Traditional web/mobile clients, webhooks, simple integrations | Bearer Token, Session |
| **GraphQL** | HTTP/JSON | Complex queries, real-time subscriptions, flexible data selection | Bearer Token, Session |
| **gRPC** | Binary/Protobuf | High-performance services, internal microservice communication | Bearer Token, mTLS |

---

## REST API

### Base URL
```
/api/v1/analytics
```

### Authentication
```
Authorization: Bearer {token}
```

Or via session cookie: `X-Session-ID`

### Rate Limiting
- **Limit:** 600 requests per minute (configurable)
- **Headers:** 
  - `X-RateLimit-Limit: 600`
  - `X-RateLimit-Remaining: 599`
  - `X-RateLimit-Reset: 1234567890`

### Common Endpoints

#### Events
```http
GET /api/v1/analytics/events
POST /api/v1/analytics/events/search
GET /api/v1/analytics/events/{id}
GET /api/v1/analytics/events/export/{format}
```

#### Dashboards
```http
GET /api/v1/analytics/dashboards
POST /api/v1/analytics/dashboards
GET /api/v1/analytics/dashboards/{id}
PUT /api/v1/analytics/dashboards/{id}
DELETE /api/v1/analytics/dashboards/{id}
```

#### Queries
```http
GET /api/v1/analytics/queries
POST /api/v1/analytics/queries
GET /api/v1/analytics/queries/{id}/execute
DELETE /api/v1/analytics/queries/{id}
```

#### Reports
```http
POST /api/v1/analytics/reports
GET /api/v1/analytics/reports/{id}
GET /api/v1/analytics/reports/{id}/download
```

#### Webhooks
```http
GET /api/v1/analytics/webhooks
POST /api/v1/analytics/webhooks
GET /api/v1/analytics/webhooks/{id}
PUT /api/v1/analytics/webhooks/{id}
DELETE /api/v1/analytics/webhooks/{id}
POST /api/v1/analytics/webhooks/{id}/test
GET /api/v1/analytics/webhooks/{id}/deliveries
POST /api/v1/analytics/webhooks/deliveries/{id}/replay
```

### Health & Monitoring
```http
GET /api/v1/analytics/health
GET /api/v1/analytics/ready
GET /api/v1/analytics/live
GET /api/v1/analytics/metrics
GET /api/v1/analytics/stats
GET /api/v1/analytics/export-formats
```

### Example: List Events
```bash
curl -H "Authorization: Bearer YOUR_TOKEN" \
  "http://localhost:8080/api/v1/analytics/events?limit=10&offset=0"
```

Response:
```json
{
  "events": [
    {
      "id": "evt_123",
      "category": "user_action",
      "eventType": "page_view",
      "data": {
        "page": "/dashboard",
        "referrer": "/home"
      },
      "userId": "user_456",
      "sessionId": "sess_789",
      "createdAt": "2026-04-24T10:30:00Z",
      "updatedAt": "2026-04-24T10:30:00Z"
    }
  ],
  "total": 1500,
  "limit": 10,
  "offset": 0
}
```

### Example: Create Dashboard
```bash
curl -X POST \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "User Engagement",
    "description": "Daily user engagement metrics",
    "isDefault": false,
    "isPublic": false,
    "config": {
      "widgets": [
        {
          "id": "w1",
          "title": "Page Views",
          "type": "line_chart",
          "queryId": "q_pageviews"
        }
      ]
    }
  }' \
  "http://localhost:8080/api/v1/analytics/dashboards"
```

### Error Handling

All errors follow this format:
```json
{
  "error": "INVALID_PARAM",
  "message": "Invalid dashboard ID",
  "requestId": "req_abc123"
}
```

Common HTTP Status Codes:
- `200` - Success
- `201` - Created
- `400` - Bad Request
- `401` - Unauthorized
- `403` - Forbidden
- `404` - Not Found
- `429` - Too Many Requests
- `500` - Internal Server Error

---

## GraphQL API

### Endpoint
```
POST /api/v1/analytics/graphql
```

### Authentication
```
Authorization: Bearer {token}
```

### Query Examples

#### Fetch Events with Filters
```graphql
query GetEvents {
  events(
    filter: {
      category: "user_action"
      startTime: "2026-04-20T00:00:00Z"
      endTime: "2026-04-24T23:59:59Z"
    }
    first: 20
    sort: { field: "createdAt", order: DESC }
  ) {
    edges {
      node {
        id
        category
        eventType
        data
        userId
        createdAt
      }
    }
    pageInfo {
      hasNextPage
      endCursor
    }
  }
}
```

#### Fetch Dashboards
```graphql
query GetDashboards {
  dashboards(isDefault: false, public: false) {
    id
    name
    description
    config
    createdAt
    updatedAt
  }
}
```

#### Subscribe to New Events
```graphql
subscription OnNewEvent {
  onNewEvent(filter: { category: "user_action" }) {
    id
    category
    eventType
    data
    userId
    createdAt
  }
}
```

### Mutations

#### Create Dashboard
```graphql
mutation CreateDashboard($input: CreateDashboardInput!) {
  createDashboard(input: $input) {
    dashboard {
      id
      name
      description
    }
    success
    errors
  }
}
```

Variables:
```json
{
  "input": {
    "name": "Sales Dashboard",
    "description": "Sales metrics for Q2",
    "isDefault": false,
    "config": {
      "widgets": []
    }
  }
}
```

#### Execute Query
```graphql
mutation ExecuteQuery($sql: String!) {
  executeQuery(sql: $sql, timeout: 30) {
    rows
    columns
    executionTime
  }
}
```

Variables:
```json
{
  "sql": "SELECT COUNT(*) as total FROM events WHERE category = 'user_action'"
}
```

### Error Handling

GraphQL errors follow this format:
```json
{
  "errors": [
    {
      "message": "Unauthorized access",
      "extensions": {
        "code": "UNAUTHENTICATED"
      }
    }
  ]
}
```

Common Error Codes:
- `UNAUTHENTICATED` - Missing or invalid token
- `FORBIDDEN` - User lacks permission
- `BAD_USER_INPUT` - Invalid input parameters
- `INTERNAL_SERVER_ERROR` - Server error
- `TIMEOUT` - Query exceeded timeout

---

## gRPC API

### Service Definition
```proto
service Analytics {
  rpc Health(HealthRequest) returns (HealthResponse);
  rpc GetEvents(GetEventsRequest) returns (GetEventsResponse);
  rpc GetDashboard(GetDashboardRequest) returns (GetDashboardResponse);
  rpc ExecuteQuery(ExecuteQueryRequest) returns (ExecuteQueryResponse);
  rpc GetWebhook(GetWebhookRequest) returns (GetWebhookResponse);
}
```

### Connection
```
host:port = localhost:50051 (default)
```

### Authentication
- Bearer token in metadata: `authorization: Bearer {token}`
- mTLS certificate-based (production)

### Example: Go Client
```go
import (
  "context"
  pb "github.com/neerajcodz/aegion/pkg/proto/analytics"
  "google.golang.org/grpc"
)

conn, err := grpc.Dial("localhost:50051")
defer conn.Close()

client := pb.NewAnalyticsClient(conn)

resp, err := client.GetEvents(context.Background(), &pb.GetEventsRequest{
  Limit:  10,
  Offset: 0,
})
```

### Example: Node.js Client
```javascript
const grpc = require('@grpc/grpc-js');
const protoLoader = require('@grpc/proto-loader');

const packageDefinition = protoLoader.loadSync('analytics.proto');
const analyticsProto = grpc.loadPackageDefinition(packageDefinition);

const client = new analyticsProto.Analytics(
  'localhost:50051',
  grpc.credentials.createInsecure()
);

client.getEvents({limit: 10, offset: 0}, (err, response) => {
  if (err) console.error(err);
  console.log(response);
});
```

---

## Choosing an API

### Use REST API when:
- Integrating traditional web/mobile applications
- Building webhooks or simple integrations
- Working with teams unfamiliar with GraphQL/gRPC
- Need simple HTTP compatibility

**Examples:**
- Dashboard client applications
- Third-party webhook integrations
- Simple data export tools

### Use GraphQL API when:
- Need flexible query capabilities
- Require real-time subscriptions
- Want to minimize over/under-fetching
- Building complex admin interfaces

**Examples:**
- Admin SPA dashboard
- Complex analytics UI with multiple widgets
- Real-time event subscriptions

### Use gRPC API when:
- High performance/throughput is critical
- Building internal microservices
- Need strict type safety
- Require streaming capabilities

**Examples:**
- Internal service-to-service communication
- High-volume data ingestion
- Real-time event streaming

---

## Authentication & Authorization

All APIs use the same authentication and authorization model:

### Authentication
1. **Bearer Token** (JWT)
   ```
   Authorization: Bearer eyJhbGciOiJIUzI1NiIs...
   ```
2. **Session Cookie**
   ```
   Cookie: X-Session-ID=sess_abc123
   ```

### Authorization
- RBAC-based (role: `admin`, `analyst`, `viewer`)
- Read-only queries allowed for all authenticated users
- Mutations require `admin` or `analyst` roles
- Dashboard/query access is scoped to user/organization

See [security.md](./security.md) for details.

---

## Rate Limiting

All APIs share the same rate limit:

**Default:** 600 requests/minute per token

Rate limit resets at the start of each minute.

### Handling Rate Limits

```bash
# REST: Check response headers
X-RateLimit-Remaining: 599

# If limit exceeded, receive 429 Conflict:
curl -i "http://localhost:8080/api/v1/analytics/events"
HTTP/1.1 429 Conflict
X-RateLimit-Reset: 1234567890

# Wait until reset time (epoch seconds) and retry
```

**GraphQL/gRPC:** Same rate limit applies; 429 error returned when exceeded.

---

## Common Patterns

### Pagination

**REST:**
```
GET /api/v1/analytics/events?limit=20&offset=40
```

**GraphQL:**
```graphql
query {
  events(first: 20, after: "cursor_123") {
    edges { node { id } }
    pageInfo { hasNextPage, endCursor }
  }
}
```

### Filtering

**REST:**
```
GET /api/v1/analytics/events?category=user_action&userId=user_456
```

**GraphQL:**
```graphql
query {
  events(filter: {
    category: "user_action"
    userId: "user_456"
  }) { ... }
}
```

### Sorting

**REST:**
```
GET /api/v1/analytics/events?sort=createdAt&order=desc
```

**GraphQL:**
```graphql
query {
  events(sort: {
    field: "createdAt"
    order: DESC
  }) { ... }
}
```

---

## Related Documentation

- [REST API Specification](./openapi.yaml)
- [GraphQL Schema](./graphql-schema.md)
- [Setup Guide](./setup.md)
- [Security Model](./security.md)
- [Webhooks](./webhooks.md)
- [Troubleshooting](./troubleshooting.md)
