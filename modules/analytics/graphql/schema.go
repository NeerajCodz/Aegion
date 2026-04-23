package graphql

import (
	"context"
	"fmt"
)

// Schema represents the GraphQL schema definition as an SDL string.
const SchemaDefinition = `
directive @auth(required: Boolean = true) on FIELD_DEFINITION
directive @cache(ttl: Int) on FIELD_DEFINITION
directive @deprecated(reason: String) on FIELD_DEFINITION | ENUM_VALUE

type Query {
	# Retrieve events with filtering, pagination, and sorting
	events(
		filter: EventFilter
		first: Int
		after: String
		sort: SortInput
	): EventConnection!

	# Retrieve a single event by ID
	event(id: ID!): Event

	# Retrieve dashboards (user scoped by default)
	dashboards(isDefault: Boolean, public: Boolean): [Dashboard!]!

	# Retrieve a single dashboard by ID
	dashboard(id: ID!): Dashboard

	# Retrieve saved queries
	queries(limit: Int, offset: Int): [SavedQuery!]!

	# Retrieve a single saved query
	query(id: ID!): SavedQuery

	# Get health status of analytics system
	health: HealthStatus!

	# Get system statistics
	stats: SystemStats!

	# Get metrics
	metrics(
		category: String
		timeRange: TimeRangeInput
	): [Metric!]!
}

type Mutation {
	# Create a new dashboard
	createDashboard(input: CreateDashboardInput!): CreateDashboardPayload!

	# Update an existing dashboard
	updateDashboard(id: ID!, input: UpdateDashboardInput!): UpdateDashboardPayload!

	# Delete a dashboard
	deleteDashboard(id: ID!): DeleteDashboardPayload!

	# Save a new query
	saveQuery(input: SaveQueryInput!): SaveQueryPayload!

	# Delete a saved query
	deleteQuery(id: ID!): DeleteQueryPayload!

	# Create a report from saved queries
	createReport(input: CreateReportInput!): CreateReportPayload!

	# Create a webhook for event notifications
	createWebhook(input: CreateWebhookInput!): CreateWebhookPayload!

	# Execute an arbitrary SQL query (requires auth)
	executeQuery(sql: String!, timeout: Int): ExecuteQueryPayload!
}

type Subscription {
	# Subscribe to new events matching filter
	onNewEvent(filter: EventFilter): Event!

	# Subscribe to metric updates
	onMetricUpdate(category: String): Metric!

	# Subscribe to dashboard data changes
	onDashboardChange(dashboardId: ID!): Dashboard!
}

# ==================== Types ====================

# Event represents an analytics event
type Event {
	id: ID!
	category: String!
	eventType: String!
	data: JSON!
	userId: String
	sessionId: String
	createdAt: DateTime!
	updatedAt: DateTime!
}

# EventConnection provides cursor-based pagination
type EventConnection {
	edges: [EventEdge!]!
	pageInfo: PageInfo!
	totalCount: Int!
}

type EventEdge {
	cursor: String!
	node: Event!
}

# Dashboard represents a saved analytics dashboard
type Dashboard {
	id: ID!
	name: String!
	description: String
	config: JSON!
	ownerId: String!
	public: Boolean!
	createdAt: DateTime!
	updatedAt: DateTime!
	# Nested query stats
	queryStats: QueryStats
}

type QueryStats {
	lastRun: DateTime
	executionTimeMs: Int
	rowsReturned: Int
}

# SavedQuery represents a saved analytics query
type SavedQuery {
	id: ID!
	name: String!
	description: String
	sql: String!
	ownerId: String!
	isPublic: Boolean!
	createdAt: DateTime!
	updatedAt: DateTime!
}

# Metric represents an aggregated metric
type Metric {
	id: ID!
	name: String!
	category: String!
	value: Float!
	unit: String
	createdAt: DateTime!
	updatedAt: DateTime!
}

# HealthStatus represents system health
type HealthStatus {
	isHealthy: Boolean!
	duckdb: Boolean!
	storage: Boolean!
	migrations: Boolean!
	lastSync: DateTime
	lag: Int  # milliseconds
	details: String
}

# SystemStats represents system statistics
type SystemStats {
	eventsTotal: Int!
	dashboardsTotal: Int!
	queriesTotal: Int!
	queryTimeAvgMs: Float!
	cacheHitRate: Float!
	uptime: Int!  # seconds
}

# ==================== Input Types ====================

input EventFilter {
	eventType: String
	category: String
	userId: String
	after: String  # e.g., "1h", "24h", ISO8601 timestamp
	before: String
	timeRange: TimeRangeInput
}

input SortInput {
	field: String!
	order: SortOrder! = DESC
}

enum SortOrder {
	ASC
	DESC
}

input TimeRangeInput {
	start: DateTime
	end: DateTime
	unit: TimeUnit
	value: Int
}

enum TimeUnit {
	HOUR
	DAY
	WEEK
	MONTH
	YEAR
}

input CreateDashboardInput {
	name: String!
	description: String
	config: JSON!
	public: Boolean
}

input UpdateDashboardInput {
	name: String
	description: String
	config: JSON
	public: Boolean
}

input SaveQueryInput {
	name: String!
	description: String
	sql: String!
	isPublic: Boolean
}

input CreateReportInput {
	title: String!
	queryIds: [ID!]!
	format: ReportFormat!
	dateRange: TimeRangeInput
}

enum ReportFormat {
	PDF
	HTML
	JSON
}

input CreateWebhookInput {
	url: String!
	eventType: String!
	active: Boolean
}

# ==================== Payloads ====================

type CreateDashboardPayload {
	dashboard: Dashboard
	errors: [Error!]
}

type UpdateDashboardPayload {
	dashboard: Dashboard
	errors: [Error!]
}

type DeleteDashboardPayload {
	success: Boolean!
	errors: [Error!]
}

type SaveQueryPayload {
	query: SavedQuery
	errors: [Error!]
}

type DeleteQueryPayload {
	success: Boolean!
	errors: [Error!]
}

type CreateReportPayload {
	reportUrl: String
	errors: [Error!]
}

type CreateWebhookPayload {
	webhook: Webhook
	errors: [Error!]
}

type ExecuteQueryPayload {
	rows: [JSON!]!
	rowCount: Int!
	executionTimeMs: Int!
	errors: [Error!]
}

type Webhook {
	id: ID!
	url: String!
	eventType: String!
	active: Boolean!
	createdAt: DateTime!
	updatedAt: DateTime!
}

type Error {
	message: String!
	code: String
}

# ==================== Common Types ====================

type PageInfo {
	hasNextPage: Boolean!
	hasPreviousPage: Boolean!
	startCursor: String
	endCursor: String
	totalCount: Int!
}

scalar DateTime
scalar JSON
`

// SchemaBuilder builds the executable schema.
type SchemaBuilder struct {
	resolver *Resolver
}

// NewSchemaBuilder creates a new schema builder.
func NewSchemaBuilder(resolver *Resolver) *SchemaBuilder {
	return &SchemaBuilder{
		resolver: resolver,
	}
}

// Build returns the complete schema as a string.
func (sb *SchemaBuilder) Build(ctx context.Context) (string, error) {
	if sb.resolver == nil {
		return "", fmt.Errorf("resolver not initialized")
	}
	return SchemaDefinition, nil
}
