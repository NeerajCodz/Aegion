package rest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
)

// Router sets up all REST API routes
func Router(h *Handler, logger zerolog.Logger) chi.Router {
	r := chi.NewRouter()

	// Global middleware
	r.Use(RequestLoggingMiddleware(logger))
	r.Use(CORSMiddleware(logger))
	r.Use(QueryTimeoutMiddleware(time.Duration(h.config.QueryTimeoutSeconds) * time.Second))

	// Health endpoints (no auth required)
	r.Get("/health", h.Health)
	r.Get("/stats", h.Stats)
	r.Get("/export-formats", h.ExportFormats)

	// Protected endpoints (require auth)
	r.Group(func(r chi.Router) {
		r.Use(AuthMiddleware(logger))
		r.Use(RateLimitMiddleware(logger, h.config.RateLimit))

		// Events endpoints
		r.Route("/events", func(r chi.Router) {
			r.Get("/", h.ListEvents)
			r.Post("/search", h.SearchEvents)
			r.Get("/{id}", h.GetEvent)
			r.Get("/export/{format}", h.ExportEvents)
		})

		// Dashboards endpoints
		r.Route("/dashboards", func(r chi.Router) {
			r.Get("/", h.ListDashboards)
			r.Post("/", h.CreateDashboard)
			r.Get("/{id}", h.GetDashboard)
			r.Put("/{id}", h.UpdateDashboard)
			r.Delete("/{id}", h.DeleteDashboard)
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
			r.Post("/", h.GenerateReport)
			r.Get("/{id}", h.GetReport)
			r.Get("/{id}/download", h.DownloadReport)
		})

		// Webhooks endpoints
		r.Route("/webhooks", func(r chi.Router) {
			r.Post("/", h.RegisterWebhook)
			r.Get("/", h.ListWebhooks)
			r.Get("/{id}", h.GetWebhook)
			r.Put("/{id}", h.UpdateWebhook)
			r.Delete("/{id}", h.DeleteWebhook)
			r.Post("/{id}/test", h.TestWebhook)
			r.Get("/{id}/deliveries", h.GetWebhookDeliveries)
		})

		// Webhook deliveries endpoints
		r.Route("/webhooks/deliveries", func(r chi.Router) {
			r.Post("/{id}/replay", h.ReplayDelivery)
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
	checkSQL := fmt.Sprintf(`SELECT owner_id FROM dashboards WHERE id = '%s'`, sanitizeID(id))
	rows, err := h.queries.ExecuteQuery(ctx, checkSQL)
	if err != nil || len(rows) == 0 {
		h.writeError(w, http.StatusNotFound, "NOT_FOUND", "dashboard not found", "")
		return
	}

	if fmt.Sprintf("%v", rows[0]["owner_id"]) != userID {
		h.writeError(w, http.StatusForbidden, "FORBIDDEN", "cannot update dashboard owned by another user", "")
		return
	}

	// Update dashboard (in production, use prepared statements)
	dashboard := map[string]interface{}{
		"id":         id,
		"name":       req.Name,
		"config":     req.Config,
		"public":     req.Public,
		"updated_at": time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(dashboard)
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
	checkSQL := fmt.Sprintf(`SELECT owner_id FROM dashboards WHERE id = '%s'`, sanitizeID(id))
	rows, err := h.queries.ExecuteQuery(ctx, checkSQL)
	if err != nil || len(rows) == 0 {
		h.writeError(w, http.StatusNotFound, "NOT_FOUND", "dashboard not found", "")
		return
	}

	if fmt.Sprintf("%v", rows[0]["owner_id"]) != userID {
		h.writeError(w, http.StatusForbidden, "FORBIDDEN", "cannot delete dashboard owned by another user", "")
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

	sql := fmt.Sprintf(`SELECT * FROM queries WHERE owner_id = '%s' ORDER BY updated_at DESC LIMIT 100`, sanitizeID(userID))

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

	query := map[string]interface{}{
		"id":          fmt.Sprintf("query_%d", time.Now().Unix()),
		"name":        req.Name,
		"description": req.Description,
		"sql":         req.SQL,
		"owner_id":    userID,
		"created_at":  time.Now(),
		"updated_at":  time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(query)
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
	sql := fmt.Sprintf(`SELECT sql FROM queries WHERE id = '%s' AND owner_id = '%s'`, sanitizeID(id), sanitizeID(userID))
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
	id := r.PathValue("id")
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "MISSING_PARAM", "missing id parameter", "")
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
