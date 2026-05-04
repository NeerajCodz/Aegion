package graphql

import (
	"time"
)

// EventConnection holds event edges and pagination info for cursor-based pagination.
type EventConnection struct {
	Edges      []*EventEdge `json:"edges"`
	PageInfo   *PageInfo    `json:"pageInfo"`
	TotalCount int          `json:"totalCount"`
}

// EventEdge represents a single event with its cursor.
type EventEdge struct {
	Cursor string     `json:"cursor"`
	Node   *EventNode `json:"node"`
}

// EventNode is the GraphQL representation of an Event.
type EventNode struct {
	ID        string                 `json:"id"`
	Category  string                 `json:"category"`
	EventType string                 `json:"eventType"`
	Data      map[string]interface{} `json:"data"`
	UserID    *string                `json:"userId,omitempty"`
	SessionID *string                `json:"sessionId,omitempty"`
	CreatedAt time.Time              `json:"createdAt"`
	UpdatedAt time.Time              `json:"updatedAt"`
}

// PageInfo holds pagination information.
type PageInfo struct {
	HasNextPage     bool    `json:"hasNextPage"`
	HasPreviousPage bool    `json:"hasPreviousPage"`
	StartCursor     *string `json:"startCursor,omitempty"`
	EndCursor       *string `json:"endCursor,omitempty"`
	TotalCount      int     `json:"totalCount"`
}

// DashboardNode is the GraphQL representation of a Dashboard.
type DashboardNode struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description *string                `json:"description,omitempty"`
	Config      map[string]interface{} `json:"config"`
	OwnerID     string                 `json:"ownerId"`
	Public      bool                   `json:"public"`
	CreatedAt   time.Time              `json:"createdAt"`
	UpdatedAt   time.Time              `json:"updatedAt"`
	QueryStats  *QueryStats            `json:"queryStats,omitempty"`
}

// QueryStats holds query execution statistics.
type QueryStats struct {
	LastRun         *time.Time `json:"lastRun,omitempty"`
	ExecutionTimeMs int        `json:"executionTimeMs"`
	RowsReturned    int        `json:"rowsReturned"`
}

// SavedQueryNode is the GraphQL representation of a SavedQuery.
type SavedQueryNode struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description *string   `json:"description,omitempty"`
	SQL         string    `json:"sql"`
	OwnerID     string    `json:"ownerId"`
	IsPublic    bool      `json:"isPublic"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// MetricNode is the GraphQL representation of a Metric.
type MetricNode struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Category  string    `json:"category"`
	Value     float64   `json:"value"`
	Unit      *string   `json:"unit,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// HealthStatusNode is the GraphQL representation of health status.
type HealthStatusNode struct {
	IsHealthy  bool       `json:"isHealthy"`
	DuckDB     bool       `json:"duckdb"`
	Storage    bool       `json:"storage"`
	Migrations bool       `json:"migrations"`
	LastSync   *time.Time `json:"lastSync,omitempty"`
	Lag        *int       `json:"lag,omitempty"`
	Details    *string    `json:"details,omitempty"`
}

// SystemStatsNode is the GraphQL representation of system statistics.
type SystemStatsNode struct {
	EventsTotal     int     `json:"eventsTotal"`
	DashboardsTotal int     `json:"dashboardsTotal"`
	QueriesTotal    int     `json:"queriesTotal"`
	QueryTimeAvgMs  float64 `json:"queryTimeAvgMs"`
	CacheHitRate    float64 `json:"cacheHitRate"`
	Uptime          int     `json:"uptime"`
}

// WebhookNode is the GraphQL representation of a Webhook.
type WebhookNode struct {
	ID        string    `json:"id"`
	URL       string    `json:"url"`
	EventType string    `json:"eventType"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ErrorNode represents an error in a GraphQL response.
type ErrorNode struct {
	Message string  `json:"message"`
	Code    *string `json:"code,omitempty"`
}

// ==================== Input Types ====================

// EventFilter is used to filter events in queries.
type EventFilter struct {
	EventType *string         `json:"eventType,omitempty"`
	Category  *string         `json:"category,omitempty"`
	UserID    *string         `json:"userId,omitempty"`
	After     *string         `json:"after,omitempty"`
	Before    *string         `json:"before,omitempty"`
	TimeRange *TimeRangeInput `json:"timeRange,omitempty"`
}

// SortInput is used to specify sorting in queries.
type SortInput struct {
	Field string    `json:"field"`
	Order SortOrder `json:"order"`
}

// SortOrder defines sort direction.
type SortOrder string

const (
	SortOrderAsc  SortOrder = "ASC"
	SortOrderDesc SortOrder = "DESC"
)

// TimeRangeInput specifies a time range.
type TimeRangeInput struct {
	Start *time.Time `json:"start,omitempty"`
	End   *time.Time `json:"end,omitempty"`
	Unit  *TimeUnit  `json:"unit,omitempty"`
	Value *int       `json:"value,omitempty"`
}

// TimeUnit defines time units.
type TimeUnit string

const (
	TimeUnitHour  TimeUnit = "HOUR"
	TimeUnitDay   TimeUnit = "DAY"
	TimeUnitWeek  TimeUnit = "WEEK"
	TimeUnitMonth TimeUnit = "MONTH"
	TimeUnitYear  TimeUnit = "YEAR"
)

// CreateDashboardInput is the input for creating a dashboard.
type CreateDashboardInput struct {
	Name        string                 `json:"name"`
	Description *string                `json:"description,omitempty"`
	Config      map[string]interface{} `json:"config"`
	Public      *bool                  `json:"public,omitempty"`
}

// UpdateDashboardInput is the input for updating a dashboard.
type UpdateDashboardInput struct {
	Name        *string                `json:"name,omitempty"`
	Description *string                `json:"description,omitempty"`
	Config      map[string]interface{} `json:"config,omitempty"`
	Public      *bool                  `json:"public,omitempty"`
}

// SaveQueryInput is the input for saving a query.
type SaveQueryInput struct {
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	SQL         string  `json:"sql"`
	IsPublic    *bool   `json:"isPublic,omitempty"`
}

// CreateReportInput is the input for creating a report.
type CreateReportInput struct {
	Title     string          `json:"title"`
	QueryIds  []string        `json:"queryIds"`
	Format    ReportFormat    `json:"format"`
	DateRange *TimeRangeInput `json:"dateRange,omitempty"`
}

// ReportFormat defines report output format.
type ReportFormat string

const (
	ReportFormatPDF  ReportFormat = "PDF"
	ReportFormatHTML ReportFormat = "HTML"
	ReportFormatJSON ReportFormat = "JSON"
)

// CreateWebhookInput is the input for creating a webhook.
type CreateWebhookInput struct {
	URL       string `json:"url"`
	EventType string `json:"eventType"`
	Active    *bool  `json:"active,omitempty"`
}

// ==================== Payloads ====================

// CreateDashboardPayload is the result of creating a dashboard.
type CreateDashboardPayload struct {
	Dashboard *DashboardNode `json:"dashboard,omitempty"`
	Errors    []*ErrorNode   `json:"errors,omitempty"`
}

// UpdateDashboardPayload is the result of updating a dashboard.
type UpdateDashboardPayload struct {
	Dashboard *DashboardNode `json:"dashboard,omitempty"`
	Errors    []*ErrorNode   `json:"errors,omitempty"`
}

// DeleteDashboardPayload is the result of deleting a dashboard.
type DeleteDashboardPayload struct {
	Success bool         `json:"success"`
	Errors  []*ErrorNode `json:"errors,omitempty"`
}

// SaveQueryPayload is the result of saving a query.
type SaveQueryPayload struct {
	Query  *SavedQueryNode `json:"query,omitempty"`
	Errors []*ErrorNode    `json:"errors,omitempty"`
}

// DeleteQueryPayload is the result of deleting a query.
type DeleteQueryPayload struct {
	Success bool         `json:"success"`
	Errors  []*ErrorNode `json:"errors,omitempty"`
}

// CreateReportPayload is the result of creating a report.
type CreateReportPayload struct {
	ReportURL *string      `json:"reportUrl,omitempty"`
	Errors    []*ErrorNode `json:"errors,omitempty"`
}

// CreateWebhookPayload is the result of creating a webhook.
type CreateWebhookPayload struct {
	Webhook *WebhookNode `json:"webhook,omitempty"`
	Errors  []*ErrorNode `json:"errors,omitempty"`
}

// ExecuteQueryPayload is the result of executing a query.
type ExecuteQueryPayload struct {
	Rows            []map[string]interface{} `json:"rows"`
	RowCount        int                      `json:"rowCount"`
	ExecutionTimeMs int                      `json:"executionTimeMs"`
	Errors          []*ErrorNode             `json:"errors,omitempty"`
}
