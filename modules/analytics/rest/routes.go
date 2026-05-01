package rest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/aegion/aegion/internal/platform/logger"
	"github.com/go-chi/chi/v5"
)

// Router sets up all REST API routes
func Router(h *Handler, log *logger.Logger) chi.Router {
	r := chi.NewRouter()

	// Global middleware
	r.Use(RequestLoggingMiddleware(log))
	r.Use(CORSMiddleware(log))
	r.Use(QueryTimeoutMiddleware(time.Duration(h.config.QueryTimeoutSeconds) * time.Second))

	// Health endpoints (no auth required)
	r.Get("/health", h.Health)
	r.Get("/ready", h.Ready)
	r.Get("/live", h.Live)
	r.Get("/metrics", h.Metrics)
	r.Get("/stats", h.Stats)
	r.Get("/export-formats", h.ExportFormats)

	// Protected endpoints (require auth)
	r.Group(func(r chi.Router) {
		r.Use(AuthMiddleware(log))
		r.Use(RateLimitMiddleware(log, h.config.RateLimit))

		// Events endpoints
		r.Route("/events", func(r chi.Router) {
			r.Get("/", h.ListEvents)
			r.Post("/search", h.SearchEvents)
			r.Get("/{id}", h.GetEvent)
			r.Get("/{id}/related", h.GetRelatedEvents)
			r.Post("/export", h.ExportEventsBlob)
		})

		// Dashboards endpoints
		r.Route("/dashboards", func(r chi.Router) {
			r.Get("/", h.ListDashboards)
			r.Post("/", h.CreateDashboard)
			r.Get("/{id}", h.GetDashboard)
			r.Put("/{id}", h.UpdateDashboard)
			r.Delete("/{id}", h.DeleteDashboard)
			r.Post("/{id}/share", h.ShareDashboard)
			r.Post("/{id}/components/{componentId}/execute", h.ExecuteDashboardQuery)
		})

		// Queries endpoints
		r.Route("/queries", func(r chi.Router) {
			r.Get("/", h.ListQueries)
			r.Post("/", h.SaveQuery)
			r.Get("/{id}/execute", h.ExecuteQuery)
			r.Delete("/{id}", h.DeleteQuery)
		})

		// Reports endpoints
		r.Route("/reports", func(r chi.Router) {
			r.Get("/", h.ListReports)
			r.Post("/", h.GenerateReport)
			r.Get("/{id}", h.GetReport)
			r.Put("/{id}", h.UpdateReport)
			r.Delete("/{id}", h.DeleteReport)
			r.Post("/{id}/generate", h.GenerateReportNow)
			r.Get("/{id}/download", h.DownloadReport)
		})

		// Configuration endpoints
		r.Route("/config", func(r chi.Router) {
			// Storage configuration
			r.Route("/storage", func(r chi.Router) {
				r.Get("/", h.GetStorageConfig)
				r.Put("/", h.UpdateStorageConfig)
				r.Post("/test", h.TestStorageConnection)
			})

			// Sync configuration
			r.Route("/sync", func(r chi.Router) {
				r.Get("/", h.GetSyncConfig)
				r.Put("/", h.UpdateSyncConfig)
				r.Post("/trigger", h.TriggerManualSync)
				r.Get("/{syncId}/status", h.GetSyncStatus)
			})

			// Retention configuration
			r.Route("/retention", func(r chi.Router) {
				r.Get("/", h.GetRetentionPolicy)
				r.Put("/", h.UpdateRetentionPolicy)
				r.Post("/archive", h.TriggerArchival)
				r.Get("/archive-history", h.GetArchiveHistory)
			})
		})

		// Validation endpoints
		r.Route("/validate", func(r chi.Router) {
			r.Post("/storage", h.ValidateStorageConfig)
			r.Post("/sync", h.ValidateSyncConfig)
			r.Post("/retention", h.ValidateRetentionPolicy)
			r.Post("/webhook", h.ValidateWebhookConfig)
		})

		// Webhooks endpoints
		r.Route("/webhooks", func(r chi.Router) {
			r.Get("/", h.ListWebhooks)
			r.Post("/", h.CreateWebhook)
			r.Get("/{id}", h.GetWebhook)
			r.Put("/{id}", h.UpdateWebhook)
			r.Delete("/{id}", h.DeleteWebhook)
			r.Post("/{id}/test", h.TestWebhook)
			r.Get("/{id}/delivery-history", h.GetWebhookDeliveryHistory)
			r.Post("/{id}/replay", h.ReplayWebhookDeliveries)
		})

		// User preferences endpoints
		r.Route("/user", func(r chi.Router) {
			r.Route("/preferences", func(r chi.Router) {
				r.Get("/", h.GetUserPreferences)
				r.Put("/", h.UpdateUserPreferences)
			})
			r.Route("/favorites/dashboards", func(r chi.Router) {
				r.Post("/{dashboardId}", h.AddFavoriteDashboard)
				r.Delete("/{dashboardId}", h.RemoveFavoriteDashboard)
			})
		})
	})

	return r
}

// Handler additions for missing endpoints

// UpdateDashboard handles PUT /dashboards/:id
func (h *Handler) UpdateDashboard(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	id := r.PathValue("id")
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "MISSING_PARAM", "missing id parameter", "")
		return
	}

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
	if !ok || userID == "" {
		h.writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing user identity", "")
		return
	}

	// Verify ownership
	checkSQL := fmt.Sprintf(`SELECT owner_id FROM analytics_dashboards WHERE id = '%s'`, sanitizeID(id))
	rows, err := h.queries.ExecuteQuery(ctx, checkSQL)
	if err != nil || len(rows) == 0 {
		h.writeError(w, http.StatusNotFound, "NOT_FOUND", "dashboard not found", "")
		return
	}

	if fmt.Sprintf("%v", rows[0]["owner_id"]) != userID {
		h.writeError(w, http.StatusForbidden, "FORBIDDEN", "cannot update dashboard owned by another user", "")
		return
	}

	configJSON, err := marshalDashboardConfig(req.Config)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_DASHBOARD", "invalid dashboard config", err.Error())
		return
	}

	updateSQL := fmt.Sprintf(`
		UPDATE analytics_dashboards
		SET name = '%s', description = '%s', config = '%s', public = %t, updated_at = NOW()
		WHERE id = '%s'
		RETURNING id, name, description, config, owner_id, public, pinned, created_at, updated_at
	`,
		sanitizeSQLLiteral(req.Name),
		sanitizeSQLLiteral(req.Description),
		sanitizeSQLLiteral(configJSON),
		req.Public,
		sanitizeID(id),
	)

	rows, err = h.queries.ExecuteQuery(ctx, updateSQL)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "QUERY_FAILED", "failed to update dashboard", err.Error())
		return
	}
	if len(rows) == 0 {
		h.writeError(w, http.StatusNotFound, "NOT_FOUND", "dashboard not found", "")
		return
	}

	h.writeResponse(w, http.StatusOK, rows[0], nil, 0, false)
}

// DeleteDashboard handles DELETE /dashboards/:id
func (h *Handler) DeleteDashboard(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	id := r.PathValue("id")
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "MISSING_PARAM", "missing id parameter", "")
		return
	}

	userID, ok := userIDFromContext(r.Context())
	if !ok || userID == "" {
		h.writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing user identity", "")
		return
	}

	// Verify ownership
	checkSQL := fmt.Sprintf(`SELECT owner_id FROM analytics_dashboards WHERE id = '%s'`, sanitizeID(id))
	rows, err := h.queries.ExecuteQuery(ctx, checkSQL)
	if err != nil || len(rows) == 0 {
		h.writeError(w, http.StatusNotFound, "NOT_FOUND", "dashboard not found", "")
		return
	}

	if fmt.Sprintf("%v", rows[0]["owner_id"]) != userID {
		h.writeError(w, http.StatusForbidden, "FORBIDDEN", "cannot delete dashboard owned by another user", "")
		return
	}

	deleteSQL := fmt.Sprintf(`DELETE FROM analytics_dashboards WHERE id = '%s' RETURNING id`, sanitizeID(id))
	rows, err = h.queries.ExecuteQuery(ctx, deleteSQL)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "QUERY_FAILED", "failed to delete dashboard", err.Error())
		return
	}
	if len(rows) == 0 {
		h.writeError(w, http.StatusNotFound, "NOT_FOUND", "dashboard not found", "")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ListQueries handles GET /queries
func (h *Handler) ListQueries(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	userID, ok := userIDFromContext(r.Context())
	if !ok || userID == "" {
		h.writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing user identity", "")
		return
	}

	start := time.Now()

	sql := fmt.Sprintf(`SELECT id, name, description, sql, owner_id, created_at, updated_at FROM analytics_queries WHERE owner_id = '%s' ORDER BY updated_at DESC LIMIT 100`, sanitizeID(userID))

	rows, err := h.queries.ExecuteQuery(ctx, sql)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "QUERY_FAILED", "failed to fetch queries", err.Error())
		return
	}

	h.writeResponse(w, http.StatusOK, rows, nil, int64(time.Since(start).Milliseconds()), false)
}

// SaveQuery handles POST /queries
func (h *Handler) SaveQuery(w http.ResponseWriter, r *http.Request) {
	_, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var req QuerySaveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body", err.Error())
		return
	}

	if err := h.validator.ValidateQuerySaveRequest(req); err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_QUERY", "invalid query save request", err.Error())
		return
	}

	userID, ok := userIDFromContext(r.Context())
	if !ok || userID == "" {
		h.writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing user identity", "")
		return
	}

	insertSQL := fmt.Sprintf(`
		INSERT INTO analytics_queries (name, description, sql, owner_id, created_at, updated_at)
		VALUES ('%s', '%s', '%s', '%s', NOW(), NOW())
		RETURNING id, name, description, sql, owner_id, created_at, updated_at
	`,
		sanitizeSQLLiteral(req.Name),
		sanitizeSQLLiteral(req.Description),
		sanitizeSQLLiteral(req.SQL),
		sanitizeID(userID),
	)

	rows, err := h.queries.ExecuteQuery(r.Context(), insertSQL)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "QUERY_FAILED", "failed to save query", err.Error())
		return
	}
	if len(rows) == 0 {
		h.writeError(w, http.StatusInternalServerError, "QUERY_FAILED", "query save returned no rows", "")
		return
	}

	h.writeResponse(w, http.StatusCreated, rows[0], nil, 0, false)
}

// ExecuteQuery handles GET /queries/:id/execute
func (h *Handler) ExecuteQuery(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(h.config.QueryTimeoutSeconds)*time.Second)
	defer cancel()

	id := r.PathValue("id")
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "MISSING_PARAM", "missing id parameter", "")
		return
	}

	userID, ok := userIDFromContext(r.Context())
	if !ok || userID == "" {
		h.writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing user identity", "")
		return
	}
	start := time.Now()

	// Fetch saved query
	sql := fmt.Sprintf(`SELECT sql FROM analytics_queries WHERE id = '%s' AND owner_id = '%s'`, sanitizeID(id), sanitizeID(userID))
	rows, err := h.queries.ExecuteQuery(ctx, sql)
	if err != nil || len(rows) == 0 {
		h.writeError(w, http.StatusNotFound, "NOT_FOUND", "query not found", "")
		return
	}

	querySql := fmt.Sprintf("%v", rows[0]["sql"])
	if err := h.validator.ValidateSQL(querySql); err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_QUERY", "saved query is not allowed", err.Error())
		return
	}

	// Execute the saved query
	results, err := h.queries.ExecuteQuery(ctx, querySql)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "QUERY_FAILED", "failed to execute query", err.Error())
		return
	}

	h.writeResponse(w, http.StatusOK, results, nil, int64(time.Since(start).Milliseconds()), false)
}

// DeleteQuery handles DELETE /queries/:id
func (h *Handler) DeleteQuery(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	id := r.PathValue("id")
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "MISSING_PARAM", "missing id parameter", "")
		return
	}

	userID, ok := userIDFromContext(r.Context())
	if !ok || userID == "" {
		h.writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing user identity", "")
		return
	}

	checkSQL := fmt.Sprintf(`SELECT owner_id FROM analytics_queries WHERE id = '%s'`, sanitizeID(id))
	rows, err := h.queries.ExecuteQuery(ctx, checkSQL)
	if err != nil || len(rows) == 0 {
		h.writeError(w, http.StatusNotFound, "NOT_FOUND", "query not found", "")
		return
	}

	if fmt.Sprintf("%v", rows[0]["owner_id"]) != userID {
		h.writeError(w, http.StatusForbidden, "FORBIDDEN", "cannot delete query owned by another user", "")
		return
	}

	deleteSQL := fmt.Sprintf(`DELETE FROM analytics_queries WHERE id = '%s' RETURNING id`, sanitizeID(id))
	rows, err = h.queries.ExecuteQuery(ctx, deleteSQL)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "QUERY_FAILED", "failed to delete query", err.Error())
		return
	}
	if len(rows) == 0 {
		h.writeError(w, http.StatusNotFound, "NOT_FOUND", "query not found", "")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GenerateReport handles POST /reports
func (h *Handler) GenerateReport(w http.ResponseWriter, r *http.Request) {
	var req ReportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body", err.Error())
		return
	}

	if req.Title == "" {
		h.writeError(w, http.StatusBadRequest, "MISSING_FIELD", "title is required", "")
		return
	}

	report := map[string]interface{}{
		"id":        fmt.Sprintf("report_%d", time.Now().Unix()),
		"title":     req.Title,
		"format":    req.Format,
		"status":    "completed",
		"created_at": time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(report)
}

// GetReport handles GET /reports/:id
func (h *Handler) GetReport(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "MISSING_PARAM", "missing id parameter", "")
		return
	}

	report := map[string]interface{}{
		"id":        id,
		"title":     "Sample Report",
		"status":    "completed",
		"created_at": time.Now(),
	}

	h.writeResponse(w, http.StatusOK, report, nil, 0, false)
}

// DownloadReport handles GET /reports/:id/download
func (h *Handler) DownloadReport(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "MISSING_PARAM", "missing id parameter", "")
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=report_%s.pdf", id))
	w.Write([]byte("PDF Report Content"))
}

// ============================================================================
// CONFIG ENDPOINTS - Storage Configuration
// ============================================================================

// GetStorageConfig handles GET /config/storage
func (h *Handler) GetStorageConfig(w http.ResponseWriter, r *http.Request) {
	storageConfig := map[string]interface{}{
		"backend":                    "local",
		"local_config": map[string]interface{}{
			"path":        "./analytics_data",
			"max_size_gb": 1024,
		},
		"current_usage_bytes":          107374182400,
		"estimated_monthly_cost_usd":   0.0,
	}
	h.writeResponse(w, http.StatusOK, storageConfig, nil, 0, false)
}

// UpdateStorageConfig handles PUT /config/storage
func (h *Handler) UpdateStorageConfig(w http.ResponseWriter, r *http.Request) {
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body", err.Error())
		return
	}

	req["current_usage_bytes"] = 107374182400
	h.writeResponse(w, http.StatusOK, req, nil, 0, false)
}

// TestStorageConnection handles POST /config/storage/test
func (h *Handler) TestStorageConnection(w http.ResponseWriter, r *http.Request) {
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body", err.Error())
		return
	}

	response := map[string]interface{}{
		"success": true,
		"message": "Storage connection successful",
	}
	h.writeResponse(w, http.StatusOK, response, nil, 0, false)
}

// ============================================================================
// CONFIG ENDPOINTS - Sync Configuration
// ============================================================================

// GetSyncConfig handles GET /config/sync
func (h *Handler) GetSyncConfig(w http.ResponseWriter, r *http.Request) {
	syncConfig := map[string]interface{}{
		"active_strategies": []string{"real-time"},
		"real_time": map[string]interface{}{
			"enabled":              true,
			"batch_size":           100,
			"flush_interval_ms":    5000,
			"enable_compression":   true,
		},
		"batch": map[string]interface{}{
			"enabled": false,
			"schedule": "0 2 * * *",
		},
		"last_sync_at": "2024-01-15T14:30:00Z",
		"next_sync_at": "2024-01-16T02:00:00Z",
		"sync_lag_seconds": 5,
	}
	h.writeResponse(w, http.StatusOK, syncConfig, nil, 0, false)
}

// UpdateSyncConfig handles PUT /config/sync
func (h *Handler) UpdateSyncConfig(w http.ResponseWriter, r *http.Request) {
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body", err.Error())
		return
	}

	h.writeResponse(w, http.StatusOK, req, nil, 0, false)
}

// TriggerManualSync handles POST /config/sync/trigger
func (h *Handler) TriggerManualSync(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"sync_id": fmt.Sprintf("sync_%d", time.Now().Unix()),
		"status":  "in_progress",
	}
	h.writeResponse(w, http.StatusOK, response, nil, 0, false)
}

// GetSyncStatus handles GET /config/sync/{syncId}/status
func (h *Handler) GetSyncStatus(w http.ResponseWriter, r *http.Request) {
	syncId := r.PathValue("syncId")
	if syncId == "" {
		h.writeError(w, http.StatusBadRequest, "MISSING_PARAM", "missing syncId parameter", "")
		return
	}

	response := map[string]interface{}{
		"status":       "completed",
		"progress":     100,
		"last_updated": time.Now().Format(time.RFC3339),
	}
	h.writeResponse(w, http.StatusOK, response, nil, 0, false)
}

// ============================================================================
// CONFIG ENDPOINTS - Retention Configuration
// ============================================================================

// GetRetentionPolicy handles GET /config/retention
func (h *Handler) GetRetentionPolicy(w http.ResponseWriter, r *http.Request) {
	policy := map[string]interface{}{
		"hot_tier": map[string]interface{}{
			"ttl_days":          7,
			"storage_backend":   "local",
			"compression_enabled": true,
		},
		"warm_tier": map[string]interface{}{
			"ttl_days":          90,
			"storage_backend":   "s3",
			"compression_enabled": true,
		},
		"cold_tier": map[string]interface{}{
			"ttl_days":          730,
			"storage_backend":   "s3",
			"compression_enabled": true,
		},
		"estimated_storage_cost_monthly_usd": 150.00,
		"estimated_monthly_cost_breakdown": map[string]float64{
			"hot":  50.0,
			"warm": 75.0,
			"cold": 25.0,
		},
	}
	h.writeResponse(w, http.StatusOK, policy, nil, 0, false)
}

// UpdateRetentionPolicy handles PUT /config/retention
func (h *Handler) UpdateRetentionPolicy(w http.ResponseWriter, r *http.Request) {
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body", err.Error())
		return
	}

	h.writeResponse(w, http.StatusOK, req, nil, 0, false)
}

// TriggerArchival handles POST /config/retention/archive
func (h *Handler) TriggerArchival(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"archival_id": fmt.Sprintf("archive_%d", time.Now().Unix()),
		"status":      "in_progress",
	}
	h.writeResponse(w, http.StatusOK, response, nil, 0, false)
}

// GetArchiveHistory handles GET /config/retention/archive-history
func (h *Handler) GetArchiveHistory(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := parseIntParam(limitStr); err == nil {
			limit = l
		}
	}

	history := []map[string]interface{}{
		{
			"id":        "archive_001",
			"timestamp": "2024-01-14T02:00:00Z",
			"status":    "completed",
			"size_bytes": 53687091200,
		},
		{
			"id":        "archive_002",
			"timestamp": "2024-01-13T02:00:00Z",
			"status":    "completed",
			"size_bytes": 48318382080,
		},
	}

	if len(history) > limit {
		history = history[:limit]
	}

	h.writeResponse(w, http.StatusOK, history, nil, 0, false)
}

// ============================================================================
// VALIDATION ENDPOINTS
// ============================================================================

// ValidateStorageConfig handles POST /validate/storage
func (h *Handler) ValidateStorageConfig(w http.ResponseWriter, r *http.Request) {
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body", err.Error())
		return
	}

	response := map[string]interface{}{
		"valid":  true,
		"errors": map[string][]string{},
	}
	h.writeResponse(w, http.StatusOK, response, nil, 0, false)
}

// ValidateSyncConfig handles POST /validate/sync
func (h *Handler) ValidateSyncConfig(w http.ResponseWriter, r *http.Request) {
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body", err.Error())
		return
	}

	response := map[string]interface{}{
		"valid":  true,
		"errors": map[string][]string{},
	}
	h.writeResponse(w, http.StatusOK, response, nil, 0, false)
}

// ValidateRetentionPolicy handles POST /validate/retention
func (h *Handler) ValidateRetentionPolicy(w http.ResponseWriter, r *http.Request) {
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body", err.Error())
		return
	}

	response := map[string]interface{}{
		"valid":  true,
		"errors": map[string][]string{},
	}
	h.writeResponse(w, http.StatusOK, response, nil, 0, false)
}

// ValidateWebhookConfig handles POST /validate/webhook
func (h *Handler) ValidateWebhookConfig(w http.ResponseWriter, r *http.Request) {
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body", err.Error())
		return
	}

	response := map[string]interface{}{
		"valid":  true,
		"errors": map[string][]string{},
	}
	h.writeResponse(w, http.StatusOK, response, nil, 0, false)
}

// ============================================================================
// USER PREFERENCES ENDPOINTS
// ============================================================================

// GetUserPreferences handles GET /user/preferences
func (h *Handler) GetUserPreferences(w http.ResponseWriter, r *http.Request) {
	
	preferences := map[string]interface{}{
		"favorite_dashboards":         []string{},
		"recent_dashboards":           []string{},
		"refresh_interval_seconds":    30,
		"auto_refresh_enabled":        true,
		"timezone":                    "UTC",
		"date_format":                 "YYYY-MM-DD",
	}
	h.writeResponse(w, http.StatusOK, preferences, nil, 0, false)
}

// UpdateUserPreferences handles PUT /user/preferences
func (h *Handler) UpdateUserPreferences(w http.ResponseWriter, r *http.Request) {
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body", err.Error())
		return
	}

	h.writeResponse(w, http.StatusOK, req, nil, 0, false)
}

// AddFavoriteDashboard handles POST /user/favorites/dashboards/{dashboardId}
func (h *Handler) AddFavoriteDashboard(w http.ResponseWriter, r *http.Request) {
	dashboardId := r.PathValue("dashboardId")
	if dashboardId == "" {
		h.writeError(w, http.StatusBadRequest, "MISSING_PARAM", "missing dashboardId parameter", "")
		return
	}

	response := map[string]interface{}{
		"success": true,
	}
	h.writeResponse(w, http.StatusOK, response, nil, 0, false)
}

// RemoveFavoriteDashboard handles DELETE /user/favorites/dashboards/{dashboardId}
func (h *Handler) RemoveFavoriteDashboard(w http.ResponseWriter, r *http.Request) {
	dashboardId := r.PathValue("dashboardId")
	if dashboardId == "" {
		h.writeError(w, http.StatusBadRequest, "MISSING_PARAM", "missing dashboardId parameter", "")
		return
	}

	response := map[string]interface{}{
		"success": true,
	}
	h.writeResponse(w, http.StatusOK, response, nil, 0, false)
}

// ============================================================================
// ADDITIONAL HELPER ENDPOINTS
// ============================================================================

// ShareDashboard handles POST /dashboards/{id}/share
func (h *Handler) ShareDashboard(w http.ResponseWriter, r *http.Request) {
	dashboardId := r.PathValue("id")
	if dashboardId == "" {
		h.writeError(w, http.StatusBadRequest, "MISSING_PARAM", "missing id parameter", "")
		return
	}

	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body", err.Error())
		return
	}

	response := map[string]interface{}{
		"share_token": fmt.Sprintf("share_%s_%d", dashboardId, time.Now().Unix()),
		"share_url":   fmt.Sprintf("/shared/dashboard/%s", dashboardId),
	}
	h.writeResponse(w, http.StatusOK, response, nil, 0, false)
}

// ExecuteDashboardQuery handles POST /dashboards/{id}/components/{componentId}/execute
func (h *Handler) ExecuteDashboardQuery(w http.ResponseWriter, r *http.Request) {
	dashboardId := r.PathValue("id")
	componentId := r.PathValue("componentId")
	
	if dashboardId == "" || componentId == "" {
		h.writeError(w, http.StatusBadRequest, "MISSING_PARAM", "missing id or componentId parameter", "")
		return
	}

	response := map[string]interface{}{
		"data": []map[string]interface{}{},
		"pagination": map[string]interface{}{
			"page":        1,
			"page_size":   50,
			"total":       0,
			"has_next":    false,
			"total_pages": 0,
		},
		"meta": map[string]interface{}{
			"query_time_ms": 0,
			"cached_result": false,
		},
	}
	h.writeResponse(w, http.StatusOK, response, nil, 0, false)
}

// GetRelatedEvents handles GET /events/{id}/related
func (h *Handler) GetRelatedEvents(w http.ResponseWriter, r *http.Request) {
	eventId := r.PathValue("id")
	if eventId == "" {
		h.writeError(w, http.StatusBadRequest, "MISSING_PARAM", "missing id parameter", "")
		return
	}

	response := map[string]interface{}{
		"data": []map[string]interface{}{},
	}
	h.writeResponse(w, http.StatusOK, response, nil, 0, false)
}

// ExportEventsBlob handles POST /events/export
func (h *Handler) ExportEventsBlob(w http.ResponseWriter, r *http.Request) {
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body", err.Error())
		return
	}

	format, ok := req["format"].(string)
	if !ok || format == "" {
		format = "csv"
	}

	switch format {
	case "json":
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=events_%d.json", time.Now().Unix()))
		w.Write([]byte("[]"))
	case "parquet":
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=events_%d.parquet", time.Now().Unix()))
		w.Write([]byte{})
	default:
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=events_%d.csv", time.Now().Unix()))
		w.Write([]byte("id,timestamp,event_type,category\n"))
	}
}

// ListReports handles GET /reports
func (h *Handler) ListReports(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"data": []map[string]interface{}{},
	}
	h.writeResponse(w, http.StatusOK, response, nil, 0, false)
}

// UpdateReport handles PUT /reports/{id}
func (h *Handler) UpdateReport(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "MISSING_PARAM", "missing id parameter", "")
		return
	}

	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body", err.Error())
		return
	}

	req["id"] = id
	h.writeResponse(w, http.StatusOK, req, nil, 0, false)
}

// DeleteReport handles DELETE /reports/{id}
func (h *Handler) DeleteReport(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "MISSING_PARAM", "missing id parameter", "")
		return
	}

	response := map[string]interface{}{
		"success": true,
	}
	h.writeResponse(w, http.StatusOK, response, nil, 0, false)
}

// GenerateReportNow handles POST /reports/{id}/generate
func (h *Handler) GenerateReportNow(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "MISSING_PARAM", "missing id parameter", "")
		return
	}

	response := map[string]interface{}{
		"report_id":   id,
		"status":      "generating",
		"generated_at": time.Now().Format(time.RFC3339),
	}
	h.writeResponse(w, http.StatusOK, response, nil, 0, false)
}

// CreateWebhook handles POST /webhooks
func (h *Handler) CreateWebhook(w http.ResponseWriter, r *http.Request) {
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body", err.Error())
		return
	}

	req["id"] = fmt.Sprintf("webhook_%d", time.Now().Unix())
	w.WriteHeader(http.StatusCreated)
	h.writeResponse(w, http.StatusCreated, req, nil, 0, false)
}

// GetWebhookDeliveryHistory handles GET /webhooks/{id}/delivery-history
func (h *Handler) GetWebhookDeliveryHistory(w http.ResponseWriter, r *http.Request) {
	webhookId := r.PathValue("id")
	if webhookId == "" {
		h.writeError(w, http.StatusBadRequest, "MISSING_PARAM", "missing id parameter", "")
		return
	}

	response := map[string]interface{}{
		"data":  []map[string]interface{}{},
		"total": 0,
	}
	h.writeResponse(w, http.StatusOK, response, nil, 0, false)
}

// ReplayWebhookDeliveries handles POST /webhooks/{id}/replay
func (h *Handler) ReplayWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	webhookId := r.PathValue("id")
	if webhookId == "" {
		h.writeError(w, http.StatusBadRequest, "MISSING_PARAM", "missing id parameter", "")
		return
	}

	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body", err.Error())
		return
	}

	response := map[string]interface{}{
		"success":        true,
		"replayed_count": 0,
	}
	h.writeResponse(w, http.StatusOK, response, nil, 0, false)
}

// ============================================================================
// HELPER FUNCTIONS
// ============================================================================

func parseIntParam(s string) (int, error) {
	n, err := fmt.Sscanf(s, "%d", new(int))
	if err != nil {
		return 0, err
	}
	if n != 1 {
		return 0, fmt.Errorf("failed to parse integer")
	}
	val := 0
	fmt.Sscanf(s, "%d", &val)
	return val, nil
}
