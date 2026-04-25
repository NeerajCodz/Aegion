# GraphQL Schema Documentation

**Version:** 1.0  
**Last Updated:** 2026-04-24  
**Module:** `modules/analytics/graphql`

## Overview

The Aegion analytics GraphQL API provides a flexible, strongly-typed interface for querying and managing analytics data. This document describes the complete schema.

**Endpoint:** `POST /api/v1/analytics/graphql`

**Authentication:** Bearer token in `Authorization` header

---

## Query Types

### Root Query

```graphql
type Query {
  # Event queries
  events(
    filter: EventFilter
    first: Int          # Items per page (max 100)
    after: String       # Pagination cursor
    sort: SortInput
  ): EventConnection!
  
  event(id: ID!): Event
  
  # Dashboard queries
  dashboards(
    isDefault: Boolean
    public: Boolean
  ): [Dashboard!]!
  
  dashboard(id: ID!): Dashboard
  
  # Query queries
  queries(
    limit: Int
    offset: Int
  ): [SavedQuery!]!
  
  query(id: ID!): SavedQuery
  
  # System status
  health: HealthStatus!
  stats: SystemStats!
  
  # Metrics
  metrics(
    category: String
    timeRange: TimeRangeInput
  ): [Metric!]!
}
```

### Event Queries

#### List Events with Pagination

```graphql
query GetEvents {
  events(
    filter: {
      category: "user_action"
      startTime: "2026-04-20T00:00:00Z"
      endTime: "2026-04-24T23:59:59Z"
      userId: "user_123"
    }
    first: 20
    after: "cursor_abc123"
    sort: { field: "createdAt", order: DESC }
  ) {
    edges {
      cursor
      node {
        id
        category
        eventType
        data
        userId
        sessionId
        createdAt
        updatedAt
      }
    }
    pageInfo {
      hasNextPage
      endCursor
      totalCount
    }
  }
}
```

#### Get Single Event

```graphql
query GetEvent($id: ID!) {
  event(id: $id) {
    id
    category
    eventType
    data
    userId
    sessionId
    metadata {
      ipAddress
      userAgent
    }
    createdAt
    updatedAt
  }
}
```

### Dashboard Queries

#### List Dashboards

```graphql
query GetDashboards {
  dashboards(isDefault: false, public: false) {
    id
    name
    description
    ownerId
    config {
      widgets {
        id
        title
        type
        queryId
        settings
      }
      layout {
        columns
        gap
      }
    }
    isDefault
    isPublic
    isPinned
    createdAt
    updatedAt
  }
}
```

#### Get Dashboard with Widget Data

```graphql
query GetDashboard($id: ID!) {
  dashboard(id: $id) {
    id
    name
    description
    config {
      widgets {
        id
        title
        type
        query {
          id
          name
          sql
          params {
            name
            value
            type
          }
        }
      }
    }
    createdAt
    updatedAt
  }
}
```

### Query Queries

#### List Saved Queries

```graphql
query GetSavedQueries {
  queries(limit: 20, offset: 0) {
    id
    name
    description
    sql
    params {
      name
      type
      required
      defaultValue
    }
    category
    createdAt
    updatedAt
  }
}
```

#### Get Query Details

```graphql
query GetQuery($id: ID!) {
  query(id: $id) {
    id
    name
    description
    sql
    params {
      name
      type
      required
      defaultValue
    }
    lastExecutedAt
    executionCount
  }
}
```

### System Status

#### Health Status

```graphql
query GetHealth {
  health {
    status        # HEALTHY, DEGRADED, UNHEALTHY
    timestamp
    components {
      name: String
      status: String
      message: String
      lastCheckedAt: DateTime
    }
    latencyMs: Int
  }
}
```

#### System Stats

```graphql
query GetSystemStats {
  stats {
    eventsCount: Int
    dashboardsCount: Int
    queriesCount: Int
    webhooksCount: Int
    syncStatus: String
    syncLatencyMs: Int
    uptime: String
    version: String
  }
}
```

#### Metrics

```graphql
query GetMetrics {
  metrics(
    category: "query_performance"
    timeRange: {
      startTime: "2026-04-20T00:00:00Z"
      endTime: "2026-04-24T23:59:59Z"
      interval: ONE_HOUR
    }
  ) {
    name
    category
    value
    unit          # "ms", "count", "percent", etc.
    tags {
      name: String
      value: String
    }
    timestamp
  }
}
```

---

## Mutation Types

### Dashboard Mutations

#### Create Dashboard

```graphql
mutation CreateDashboard($input: CreateDashboardInput!) {
  createDashboard(input: $input) {
    dashboard {
      id
      name
      description
    }
    success: Boolean!
    errors: [String!]
  }
}
```

Input:
```graphql
input CreateDashboardInput {
  name: String!
  description: String
  isDefault: Boolean
  isPublic: Boolean
  config: DashboardConfigInput!
}

input DashboardConfigInput {
  widgets: [WidgetInput!]!
  layout: LayoutInput
}

input WidgetInput {
  title: String!
  type: String!        # line_chart, bar_chart, table, gauge, etc.
  queryId: ID
  settings: JSON
}
```

Example variables:
```json
{
  "input": {
    "name": "Q2 Sales",
    "description": "Sales metrics for Q2 2026",
    "isDefault": false,
    "isPublic": false,
    "config": {
      "widgets": [
        {
          "title": "Revenue",
          "type": "gauge",
          "queryId": "q_revenue",
          "settings": { "minValue": 0, "maxValue": 100000 }
        }
      ],
      "layout": { "columns": 2 }
    }
  }
}
```

#### Update Dashboard

```graphql
mutation UpdateDashboard($id: ID!, $input: UpdateDashboardInput!) {
  updateDashboard(id: $id, input: $input) {
    dashboard {
      id
      name
    }
    success: Boolean!
    errors: [String!]
  }
}

input UpdateDashboardInput {
  name: String
  description: String
  config: DashboardConfigInput
  isDefault: Boolean
  isPublic: Boolean
}
```

#### Delete Dashboard

```graphql
mutation DeleteDashboard($id: ID!) {
  deleteDashboard(id: $id) {
    success: Boolean!
    errors: [String!]
  }
}
```

### Query Mutations

#### Save Query

```graphql
mutation SaveQuery($input: SaveQueryInput!) {
  saveQuery(input: $input) {
    query {
      id
      name
      sql
    }
    success: Boolean!
    errors: [String!]
  }
}

input SaveQueryInput {
  name: String!
  description: String
  sql: String!
  params: [QueryParamInput!]
  category: String
}

input QueryParamInput {
  name: String!
  type: String!        # STRING, INT, DATETIME, etc.
  required: Boolean
  defaultValue: String
}
```

#### Execute Query

```graphql
mutation ExecuteQuery($sql: String!, $timeout: Int) {
  executeQuery(sql: $sql, timeout: $timeout) {
    rows: [JSON!]!
    columns: [ColumnDefinition!]!
    executionTime: Int   # milliseconds
    rowCount: Int
    success: Boolean!
    error: String
  }
}

type ColumnDefinition {
  name: String!
  type: String!        # STRING, INT, FLOAT, DATETIME
}
```

Example:
```graphql
mutation {
  executeQuery(
    sql: "SELECT userId, COUNT(*) as count FROM events GROUP BY userId LIMIT 10"
    timeout: 30
  ) {
    rows
    columns { name type }
    executionTime
  }
}
```

#### Delete Query

```graphql
mutation DeleteQuery($id: ID!) {
  deleteQuery(id: $id) {
    success: Boolean!
    errors: [String!]
  }
}
```

### Report Mutations

#### Create Report

```graphql
mutation CreateReport($input: CreateReportInput!) {
  createReport(input: $input) {
    report {
      id
      name
      title
    }
    success: Boolean!
    errors: [String!]
  }
}

input CreateReportInput {
  name: String!
  title: String!
  description: String
  queryIds: [ID!]!
  format: String        # pdf, excel, json
  schedule: String      # Cron expression or null for on-demand
}
```

### Webhook Mutations

#### Create Webhook

```graphql
mutation CreateWebhook($input: CreateWebhookInput!) {
  createWebhook(input: $input) {
    webhook {
      id
      url
      events
      isActive
    }
    success: Boolean!
    errors: [String!]
  }
}

input CreateWebhookInput {
  url: String!
  events: [String!]!    # e.g., ["event.created", "event.updated"]
  filters: WebhookFilterInput
  retryPolicy: RetryPolicyInput
  headers: [HeaderInput!]
  isActive: Boolean
}

input WebhookFilterInput {
  categories: [String!]
  eventTypes: [String!]
  minSeverity: String
}

input RetryPolicyInput {
  maxRetries: Int
  backoffMultiplier: Float
}

input HeaderInput {
  name: String!
  value: String!
}
```

#### Update Webhook

```graphql
mutation UpdateWebhook($id: ID!, $input: UpdateWebhookInput!) {
  updateWebhook(id: $id, input: $input) {
    webhook {
      id
      url
      isActive
    }
    success: Boolean!
    errors: [String!]
  }
}

input UpdateWebhookInput {
  url: String
  events: [String!]
  filters: WebhookFilterInput
  isActive: Boolean
}
```

#### Delete Webhook

```graphql
mutation DeleteWebhook($id: ID!) {
  deleteWebhook(id: $id) {
    success: Boolean!
    errors: [String!]
  }
}
```

---

## Subscription Types

### Subscribe to New Events

```graphql
subscription OnNewEvent {
  onNewEvent(filter: EventFilter) {
    id
    category
    eventType
    data
    userId
    createdAt
  }
}
```

Usage (WebSocket):
```javascript
const subscription = gql`
  subscription {
    onNewEvent(filter: { category: "user_action" }) {
      id
      category
      eventType
      userId
      createdAt
    }
  }
`;

client.subscribe({ query: subscription })
  .subscribe({
    next: (event) => console.log('New event:', event),
    error: (err) => console.error(err),
    complete: () => console.log('Done')
  });
```

### Subscribe to Metric Updates

```graphql
subscription OnMetricUpdate {
  onMetricUpdate(category: "query_performance") {
    name
    value
    unit
    timestamp
  }
}
```

### Subscribe to Dashboard Changes

```graphql
subscription OnDashboardChange($dashboardId: ID!) {
  onDashboardChange(dashboardId: $dashboardId) {
    id
    name
    config {
      widgets {
        id
        title
      }
    }
    updatedAt
  }
}
```

---

## Input Types & Filters

### EventFilter

```graphql
input EventFilter {
  # Time range
  startTime: DateTime
  endTime: DateTime
  
  # Event matching
  category: String
  eventType: String
  
  # User/session matching
  userId: String
  sessionId: String
  
  # Data matching
  dataFilter: JSON      # JSONPath filter
  
  # Advanced
  fullText: String      # Full-text search
  severity: String      # ERROR, WARN, INFO, DEBUG
}
```

Example:
```graphql
{
  category: "user_action"
  eventType: "page_view"
  startTime: "2026-04-20T00:00:00Z"
  endTime: "2026-04-24T23:59:59Z"
  dataFilter: { page: "/dashboard" }
}
```

### SortInput

```graphql
input SortInput {
  field: String!        # Field name
  order: SortOrder!     # ASC or DESC
}

enum SortOrder {
  ASC
  DESC
}
```

### TimeRangeInput

```graphql
input TimeRangeInput {
  startTime: DateTime!
  endTime: DateTime!
  interval: String      # ONE_HOUR, ONE_DAY, ONE_WEEK
}
```

---

## Output Types

### Event

```graphql
type Event {
  id: ID!
  category: String!
  eventType: String!
  data: JSON!
  userId: String
  sessionId: String
  severity: String
  metadata: EventMetadata
  createdAt: DateTime!
  updatedAt: DateTime!
}

type EventMetadata {
  ipAddress: String
  userAgent: String
  location: String
}
```

### EventConnection (Paginated)

```graphql
type EventConnection {
  edges: [EventEdge!]!
  pageInfo: PageInfo!
}

type EventEdge {
  cursor: String!
  node: Event!
}

type PageInfo {
  hasNextPage: Boolean!
  endCursor: String
  totalCount: Int
}
```

### Dashboard

```graphql
type Dashboard {
  id: ID!
  name: String!
  description: String
  ownerId: ID!
  config: DashboardConfig!
  isDefault: Boolean!
  isPublic: Boolean!
  isPinned: Boolean!
  createdAt: DateTime!
  updatedAt: DateTime!
}

type DashboardConfig {
  widgets: [Widget!]!
  layout: Layout
}

type Widget {
  id: ID!
  title: String!
  type: String!
  query: SavedQuery
  settings: JSON
}
```

### SavedQuery

```graphql
type SavedQuery {
  id: ID!
  name: String!
  description: String
  sql: String!
  params: [QueryParam!]
  category: String
  lastExecutedAt: DateTime
  executionCount: Int
  createdAt: DateTime!
  updatedAt: DateTime!
}

type QueryParam {
  name: String!
  type: String!
  required: Boolean!
  defaultValue: String
}
```

### Webhook

```graphql
type Webhook {
  id: ID!
  url: String!
  events: [String!]!
  filters: WebhookFilter
  retryPolicy: RetryPolicy
  isActive: Boolean!
  deliveryStats: WebhookStats
  createdAt: DateTime!
  updatedAt: DateTime!
}

type WebhookStats {
  totalDeliveries: Int
  successCount: Int
  failureCount: Int
  averageLatencyMs: Int
}
```

### HealthStatus

```graphql
type HealthStatus {
  status: String!       # HEALTHY, DEGRADED, UNHEALTHY
  timestamp: DateTime!
  components: [ComponentStatus!]!
  latencyMs: Int!
}

type ComponentStatus {
  name: String!
  status: String!
  message: String
  lastCheckedAt: DateTime!
}
```

---

## Directives

### @auth

Requires authentication:
```graphql
type Query {
  adminOnly: String @auth(required: true)
}
```

### @cache

Enables caching with TTL:
```graphql
type Query {
  dashboards: [Dashboard!] @cache(ttl: 300)
}
```

### @deprecated

Marks field as deprecated:
```graphql
type Event {
  userId: ID! @deprecated(reason: "Use userRef instead")
  userRef: ID!
}
```

---

## Error Handling

GraphQL errors follow a standard format:

```json
{
  "errors": [
    {
      "message": "Unauthorized access",
      "extensions": {
        "code": "UNAUTHENTICATED",
        "timestamp": "2026-04-24T10:30:00Z"
      }
    }
  ]
}
```

Common error codes:
- `UNAUTHENTICATED` - Missing/invalid token
- `FORBIDDEN` - Permission denied
- `BAD_USER_INPUT` - Invalid input
- `NOT_FOUND` - Resource not found
- `INTERNAL_SERVER_ERROR` - Server error
- `QUERY_TIMEOUT` - Query exceeded timeout
- `RATE_LIMITED` - Rate limit exceeded

---

## Related Documentation

- [API Reference](./api.md)
- [Security](./security.md)
- [Performance Tuning](./performance.md)
