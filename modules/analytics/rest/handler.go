package rest

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	analytics "github.com/aegion/aegion/modules/analytics"
	"github.com/aegion/aegion/modules/analytics/webhooks"
)

// Handler handles REST API requests for analytics
type Handler struct {
	logger         *slog.Logger
	config         Config
	queries        QueryBuilder
	exports        ExportBuilder
	cache          ResultCache
	validator      *Validator
	webhookManager WebhookManager
}

// Config holds REST API configuration
type Config struct {
	BasePath              string
	RateLimit             int
	QueryTimeoutSeconds   int
	ResultCacheTTLMinutes int
	MaxPageSize           int
	DefaultPageSize       int
}

// HandlerDeps holds dependencies for the handler
type HandlerDeps struct {
	Logger         *slog.Logger
	Config         Config
	Queries        QueryBuilder
	Exports        ExportBuilder
	Cache          ResultCache
	Validator      *Validator
	WebhookManager WebhookManager
}

// WebhookManager provides webhook operations used by REST handlers.
type WebhookManager interface {
	RegisterWebhook(ctx context.Context, userID string, req *webhooks.WebhookRequest) (*analytics.Webhook, error)
	UpdateWebhook(ctx context.Context, userID, webhookID string, req *webhooks.WebhookRequest) (*analytics.Webhook, error)
	DeleteWebhook(ctx context.Context, userID, webhookID string) error
	ListWebhooks(ctx context.Context, userID string) ([]*analytics.Webhook, error)
	GetWebhook(ctx context.Context, webhookID string) (*analytics.Webhook, error)
	TestWebhook(ctx context.Context, webhookID string) (string, error)
	GetDeliveryHistory(ctx context.Context, webhookID string, limit int) ([]*analytics.WebhookDelivery, error)
	GetDelivery(ctx context.Context, deliveryID string) (*analytics.WebhookDelivery, error)
	ReplayEvent(ctx context.Context, deliveryID string) error
}

// QueryBuilder interface for building queries
type QueryBuilder interface {
	BuildQuery(ctx context.Context, req QueryRequest, table string) (string, error)
	ExecuteQuery(ctx context.Context, sql string) ([]map[string]interface{}, error)
	ExecuteCount(ctx context.Context, sql string) (int, error)
}

// ExportBuilder interface for building exports
type ExportBuilder interface {
	ExportCSV(ctx context.Context, sql string, w http.ResponseWriter) error
	ExportJSON(ctx context.Context, sql string, w http.ResponseWriter) error
	ExportParquet(ctx context.Context, sql string, w http.ResponseWriter) error
}

// ResultCache interface for caching results
type ResultCache interface {
	Get(ctx context.Context, key string) (interface{}, bool, error)
	Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
}

// NewHandler creates a new REST handler
func NewHandler(deps HandlerDeps) *Handler {
	v := deps.Validator
	if v == nil {
		v = NewValidator()
	}
	return &Handler{
		logger:         deps.Logger,
		config:         deps.Config,
		queries:        deps.Queries,
		exports:        deps.Exports,
		cache:          deps.Cache,
		validator:      v,
		webhookManager: deps.WebhookManager,
	}
}

// ListEvents handles GET /events
func (h *Handler) ListEvents(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(h.config.QueryTimeoutSeconds)*time.Second)
	defer cancel()

	var req QueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && r.Body != http.NoBody {
		h.writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body", err.Error())
		return
	}

	// Apply defaults
	if req.PageSize == 0 {
		req.PageSize = h.config.DefaultPageSize
	}
	if req.PageSize > h.config.MaxPageSize {
		req.PageSize = h.config.MaxPageSize
	}
	if req.Page == 0 {
		req.Page = 1
	}

	start := time.Now()

	if err := h.validator.ValidateQueryRequest(req); err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_QUERY", "invalid query parameters", err.Error())
		return
	}

	// Build SQL query
	sql, err := h.queries.BuildQuery(ctx, req, "events")
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "QUERY_ERROR", "failed to build query", err.Error())
		return
	}

	// Try cache first
	cacheKey := fmt.Sprintf("events:%s", hashQuery(sql))
	if cached, found, _ := h.cache.Get(ctx, cacheKey); found && cached != nil {
		data := cached.([]map[string]interface{})
		h.writeResponse(w, http.StatusOK, data, nil, int64(time.Since(start).Milliseconds()), true)
		return
	}

	// Execute query
	rows, err := h.queries.ExecuteQuery(ctx, sql)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "QUERY_FAILED", "failed to execute query", err.Error())
		return
	}

	// Get total count
	countSQL := fmt.Sprintf("SELECT COUNT(*) as count FROM (%s) as subq", sql)
	total, err := h.queries.ExecuteCount(ctx, countSQL)
	if err != nil {
		h.logger.Warn("failed to get count", "error", err)
		total = len(rows)
	}

	// Cache result
	_ = h.cache.Set(ctx, cacheKey, rows, time.Duration(h.config.ResultCacheTTLMinutes)*time.Minute)

	// Build pagination
	totalPages := (total + req.PageSize - 1) / req.PageSize
	hasNext := req.Page < totalPages

	pagination := &Pagination{
		Page:       req.Page,
		PageSize:   req.PageSize,
		Total:      total,
		HasNext:    hasNext,
		TotalPages: totalPages,
	}

	h.writeResponse(w, http.StatusOK, rows, pagination, int64(time.Since(start).Milliseconds()), false)
}

// GetEvent handles GET /events/:id
func (h *Handler) GetEvent(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(h.config.QueryTimeoutSeconds)*time.Second)
	defer cancel()

	id := r.PathValue("id")
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "MISSING_PARAM", "missing id parameter", "")
		return
	}

	start := time.Now()

	sql := fmt.Sprintf(`SELECT * FROM events WHERE id = '%s' LIMIT 1`, sanitizeID(id))
	rows, err := h.queries.ExecuteQuery(ctx, sql)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "QUERY_FAILED", "failed to fetch event", err.Error())
		return
	}

	if len(rows) == 0 {
		h.writeError(w, http.StatusNotFound, "NOT_FOUND", "event not found", "")
		return
	}

	h.writeResponse(w, http.StatusOK, rows[0], nil, int64(time.Since(start).Milliseconds()), false)
}

// SearchEvents handles POST /events/search
func (h *Handler) SearchEvents(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(h.config.QueryTimeoutSeconds)*time.Second)
	defer cancel()

	var req SearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body", err.Error())
		return
	}

	if err := h.validator.ValidateSearchRequest(req); err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_SEARCH", "invalid search request", err.Error())
		return
	}

	start := time.Now()

	// Build search SQL (simplified FTS)
	sql := fmt.Sprintf(`
		SELECT * FROM events 
		WHERE data ->> 'search_field' ILIKE '%%%s%%'
		ORDER BY created_at DESC
		LIMIT %d OFFSET %d
	`, sanitizeInput(req.Query), req.PageSize, (req.Page-1)*req.PageSize)

	rows, err := h.queries.ExecuteQuery(ctx, sql)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "QUERY_FAILED", "search failed", err.Error())
		return
	}

	h.writeResponse(w, http.StatusOK, rows, nil, int64(time.Since(start).Milliseconds()), false)
}

// ExportEvents handles GET /events/export/:format
func (h *Handler) ExportEvents(w http.ResponseWriter, r *http.Request) {
	format := r.PathValue("format")
	if format == "" {
		h.writeError(w, http.StatusBadRequest, "MISSING_FORMAT", "missing format parameter", "")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(h.config.QueryTimeoutSeconds)*time.Second)
	defer cancel()

	// Parse query from request
	var req QueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && r.Body != http.NoBody {
		h.writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body", err.Error())
		return
	}

	if err := h.validator.ValidateQueryRequest(req); err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_QUERY", "invalid query parameters", err.Error())
		return
	}

	sql, err := h.queries.BuildQuery(ctx, req, "events")
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "QUERY_ERROR", "failed to build query", err.Error())
		return
	}

	switch format {
	case "csv":
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", "attachment; filename=events.csv")
		if err := h.exports.ExportCSV(ctx, sql, w); err != nil {
			h.logger.Error("csv export failed", "error", err)
		}
	case "json":
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", "attachment; filename=events.json")
		if err := h.exports.ExportJSON(ctx, sql, w); err != nil {
			h.logger.Error("json export failed", "error", err)
		}
	case "parquet":
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", "attachment; filename=events.parquet")
		if err := h.exports.ExportParquet(ctx, sql, w); err != nil {
			h.logger.Error("parquet export failed", "error", err)
		}
	default:
		h.writeError(w, http.StatusBadRequest, "INVALID_FORMAT", "unsupported format", fmt.Sprintf("format must be csv, json, or parquet, got %s", format))
	}
}

// ListDashboards handles GET /dashboards
func (h *Handler) ListDashboards(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Get current user from context (would be set by auth middleware)
	userID, ok := userIDFromContext(r.Context())
	if !ok {
		h.writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing user identity", "")
		return
	}

	sql := fmt.Sprintf(`
		SELECT id, name, description, config, owner_id, public, pinned, created_at, updated_at
		FROM analytics_dashboards 
		WHERE public = true OR owner_id = '%s'
		ORDER BY updated_at DESC
		LIMIT 100
	`, sanitizeID(userID))

	rows, err := h.queries.ExecuteQuery(ctx, sql)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "QUERY_FAILED", "failed to fetch dashboards", err.Error())
		return
	}

	h.writeResponse(w, http.StatusOK, rows, nil, 0, false)
}

// GetDashboard handles GET /dashboards/:id
func (h *Handler) GetDashboard(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	id := r.PathValue("id")
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "MISSING_PARAM", "missing id parameter", "")
		return
	}

	userID, ok := userIDFromContext(r.Context())
	if !ok {
		h.writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing user identity", "")
		return
	}
	sql := fmt.Sprintf(`
		SELECT id, name, description, config, owner_id, public, pinned, created_at, updated_at
		FROM analytics_dashboards 
		WHERE id = '%s' AND (public = true OR owner_id = '%s')
		LIMIT 1
	`, sanitizeID(id), sanitizeID(userID))

	rows, err := h.queries.ExecuteQuery(ctx, sql)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "QUERY_FAILED", "failed to fetch dashboard", err.Error())
		return
	}

	if len(rows) == 0 {
		h.writeError(w, http.StatusNotFound, "NOT_FOUND", "dashboard not found", "")
		return
	}

	h.writeResponse(w, http.StatusOK, rows[0], nil, 0, false)
}

// CreateDashboard handles POST /dashboards
func (h *Handler) CreateDashboard(w http.ResponseWriter, r *http.Request) {
	var req DashboardRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body", err.Error())
		return
	}

	if err := h.validator.ValidateDashboardRequest(req); err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_DASHBOARD", "invalid dashboard request", err.Error())
		return
	}

	userID, ok := userIDFromContext(r.Context())
	if !ok {
		h.writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing user identity", "")
		return
	}

	configJSON, err := marshalDashboardConfig(req.Config)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_DASHBOARD", "invalid dashboard config", err.Error())
		return
	}

	sql := fmt.Sprintf(`
		INSERT INTO analytics_dashboards (name, description, config, owner_id, public, pinned, created_at, updated_at)
		VALUES ('%s', '%s', '%s', '%s', %t, false, NOW(), NOW())
		RETURNING id, name, description, config, owner_id, public, pinned, created_at, updated_at
	`,
		sanitizeSQLLiteral(req.Name),
		sanitizeSQLLiteral(req.Description),
		sanitizeSQLLiteral(configJSON),
		sanitizeID(userID),
		req.Public,
	)

	rows, err := h.queries.ExecuteQuery(r.Context(), sql)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "QUERY_FAILED", "failed to create dashboard", err.Error())
		return
	}
	if len(rows) == 0 {
		h.writeError(w, http.StatusInternalServerError, "QUERY_FAILED", "dashboard creation returned no rows", "")
		return
	}

	h.writeResponse(w, http.StatusCreated, rows[0], nil, 0, false)
}

// Stats handles GET /stats
func (h *Handler) Stats(w http.ResponseWriter, r *http.Request) {
	response := StatsResponse{
		Events:         0,
		Queries:        0,
		Dashboards:     0,
		CacheHits:      0,
		CacheMisses:    0,
		QueryTimeAvgMs: 0,
		LastQueryTime:  time.Now(),
	}

	h.writeResponse(w, http.StatusOK, response, nil, 0, false)
}

// ExportFormats handles GET /export-formats
func (h *Handler) ExportFormats(w http.ResponseWriter, r *http.Request) {
	formats := []ExportFormat{
		{
			Name:        "CSV",
			MimeType:    "text/csv",
			Extension:   "csv",
			Description: "Comma-separated values",
		},
		{
			Name:        "JSON",
			MimeType:    "application/json",
			Extension:   "json",
			Description: "JSON array format",
		},
		{
			Name:        "Parquet",
			MimeType:    "application/octet-stream",
			Extension:   "parquet",
			Description: "Apache Parquet binary format",
		},
	}

	response := ExportFormatsResponse{Formats: formats}
	h.writeResponse(w, http.StatusOK, response, nil, 0, false)
}

// writeResponse writes a JSON response
func (h *Handler) writeResponse(w http.ResponseWriter, status int, data interface{}, pagination *Pagination, queryTimeMs int64, cached bool) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	meta := &ResponseMeta{
		QueryTimeMs:  queryTimeMs,
		ExportedAt:   time.Now().UTC().Format(time.RFC3339),
		CachedResult: cached,
	}

	if rows, ok := data.([]map[string]interface{}); ok {
		meta.ResultCount = len(rows)
	}

	response := Response{
		Data:       data,
		Pagination: pagination,
		Meta:       meta,
	}

	json.NewEncoder(w).Encode(response)
}

// writeError writes an error response
func (h *Handler) writeError(w http.ResponseWriter, status int, code, message, details string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	response := Response{
		Error: &ErrorDetail{
			Code:    code,
			Message: message,
			Details: details,
		},
		Meta: &ResponseMeta{
			ExportedAt: time.Now().UTC().Format(time.RFC3339),
		},
	}

	json.NewEncoder(w).Encode(response)
}

// Helper functions

func hashQuery(sql string) string {
	// Simple hash for caching - in production use a proper hash function
	return fmt.Sprintf("%x", len(sql))
}

func sanitizeID(id string) string {
	// Basic sanitization - prevent SQL injection
	// In production, use prepared statements
	return strings.ReplaceAll(id, "'", "''")
}

func sanitizeInput(input string) string {
	// Basic sanitization
	return strings.ReplaceAll(input, "'", "''")
}

func sanitizeSQLLiteral(input string) string {
	return strings.ReplaceAll(input, "'", "''")
}

func marshalDashboardConfig(config map[string]interface{}) (string, error) {
	if config == nil {
		config = map[string]interface{}{}
	}

	body, err := json.Marshal(config)
	if err != nil {
		return "", err
	}
	return string(body), nil
}
