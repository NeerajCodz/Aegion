package rest

import "time"

// QueryRequest represents a query request with filters and pagination
type QueryRequest struct {
	Fields    []string               `json:"fields,omitempty"`
	Filters   map[string]interface{} `json:"filters,omitempty"`
	Sort      []SortField            `json:"sort,omitempty"`
	Page      int                    `json:"page,omitempty"`
	PageSize  int                    `json:"page_size,omitempty"`
	Cursor    string                 `json:"cursor,omitempty"`
	TimeRange *TimeRange             `json:"time_range,omitempty"`
	Aggregate []AggregateField       `json:"aggregate,omitempty"`
	GroupBy   []string               `json:"group_by,omitempty"`
}

// SortField represents a field to sort by
type SortField struct {
	Field     string `json:"field"`
	Direction string `json:"direction"` // "asc" or "desc"
}

// AggregateField represents an aggregation function
type AggregateField struct {
	Function string `json:"function"` // count, sum, avg, min, max, percentile
	Field    string `json:"field"`
	Alias    string `json:"alias,omitempty"`
	Param    string `json:"param,omitempty"` // for percentile(95)
}

// TimeRange represents a time range for queries
type TimeRange struct {
	Start *time.Time `json:"start,omitempty"`
	End   *time.Time `json:"end,omitempty"`
	Unit  string     `json:"unit,omitempty"`  // h, d, w, mo
	Value int        `json:"value,omitempty"` // last N units
}

// SearchRequest represents an advanced search request
type SearchRequest struct {
	Query    string                 `json:"query"`
	Filters  map[string]interface{} `json:"filters,omitempty"`
	Page     int                    `json:"page,omitempty"`
	PageSize int                    `json:"page_size,omitempty"`
}

// Response represents a standardized API response
type Response struct {
	Data       interface{}   `json:"data"`
	Pagination *Pagination   `json:"pagination,omitempty"`
	Meta       *ResponseMeta `json:"meta"`
	Error      *ErrorDetail  `json:"error,omitempty"`
}

// Pagination contains pagination information
type Pagination struct {
	Page       int    `json:"page"`
	PageSize   int    `json:"page_size"`
	Total      int    `json:"total"`
	HasNext    bool   `json:"has_next"`
	Cursor     string `json:"cursor,omitempty"`
	TotalPages int    `json:"total_pages"`
}

// ResponseMeta contains metadata about the response
type ResponseMeta struct {
	QueryTimeMs  int64  `json:"query_time_ms"`
	ExportedAt   string `json:"exported_at"`
	ResultCount  int    `json:"result_count"`
	CachedResult bool   `json:"cached_result,omitempty"`
}

// ErrorDetail represents error details in response
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details string `json:"details,omitempty"`
}

// DashboardRequest represents a dashboard creation/update request
type DashboardRequest struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Config      map[string]interface{} `json:"config"`
	Public      bool                   `json:"public"`
}

// QuerySaveRequest represents a query save request
type QuerySaveRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	SQL         string `json:"sql"`
}

// ReportRequest represents a report generation request
type ReportRequest struct {
	Title     string    `json:"title"`
	Queries   []string  `json:"queries"`
	Format    string    `json:"format"` // pdf, html, json
	DateRange TimeRange `json:"date_range,omitempty"`
}

// ExportRequest represents an export request
type ExportRequest struct {
	Format   string       `json:"format"` // csv, json, parquet
	Query    QueryRequest `json:"query,omitempty"`
	Filename string       `json:"filename,omitempty"`
}

// HealthResponse represents the health check response
type HealthResponse struct {
	Status string          `json:"status"`
	Ready  bool            `json:"ready"`
	DuckDB bool            `json:"duckdb"`
	Checks map[string]bool `json:"checks"`
	Time   time.Time       `json:"time"`
}

// StatsResponse represents system statistics
type StatsResponse struct {
	Events         int64     `json:"events_total"`
	Queries        int64     `json:"queries_total"`
	Dashboards     int64     `json:"dashboards_total"`
	CacheHits      int64     `json:"cache_hits"`
	CacheMisses    int64     `json:"cache_misses"`
	QueryTimeAvgMs float64   `json:"query_time_avg_ms"`
	LastQueryTime  time.Time `json:"last_query_time"`
}

// ExportFormatsResponse lists available export formats
type ExportFormatsResponse struct {
	Formats []ExportFormat `json:"formats"`
}

// ExportFormat represents an export format
type ExportFormat struct {
	Name        string `json:"name"`
	MimeType    string `json:"mime_type"`
	Extension   string `json:"extension"`
	Description string `json:"description,omitempty"`
}

// AuditLogEntry represents an audit log entry
type AuditLogEntry struct {
	ID           string                 `json:"id"`
	Timestamp    string                 `json:"timestamp"`
	UserID       string                 `json:"user_id"`
	EventType    string                 `json:"event_type"`
	Action       string                 `json:"action"`
	ResourceType string                 `json:"resource_type,omitempty"`
	ResourceID   string                 `json:"resource_id,omitempty"`
	Status       string                 `json:"status"`
	ErrorMsg     string                 `json:"error_message,omitempty"`
	Details      map[string]interface{} `json:"details,omitempty"`
	IPAddress    string                 `json:"ip_address,omitempty"`
}

// UserRole represents a user's role assignment
type UserRole struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}

// PermissionRequest represents a permission grant/revoke request
type PermissionRequest struct {
	UserID     string `json:"user_id"`
	Permission string `json:"permission"`
}
