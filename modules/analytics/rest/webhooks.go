package rest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	analytics "github.com/aegion/aegion/modules/analytics"
	"github.com/aegion/aegion/modules/analytics/webhooks"
)

// RegisterWebhookRequest is the API request for registering a webhook.
type RegisterWebhookRequest struct {
	URL          string                 `json:"url"`
	EventFilter  webhooks.EventFilter   `json:"event_filter"`
	Secret       string                 `json:"secret,omitempty"`
	Active       bool                   `json:"active,omitempty"`
}

// ListWebhooksResponse wraps webhook responses.
type ListWebhooksResponse struct {
	Data  []*webhooks.WebhookResponse `json:"data"`
	Total int                         `json:"total"`
}

// Handler has a reference to the webhook manager.
type WebhookHandlerDeps struct {
	Manager WebhookManager
	Logger  interface{} // Can be any logger with basic methods
}

func routeParam(r *http.Request, key string) string {
	if value := chi.URLParam(r, key); value != "" {
		return value
	}
	return r.PathValue(key)
}

func (h *Handler) requireWebhookManager(w http.ResponseWriter) WebhookManager {
	if h.webhookManager == nil {
		h.writeError(w, http.StatusServiceUnavailable, "WEBHOOKS_UNAVAILABLE", "webhook manager not configured", "")
		return nil
	}
	return h.webhookManager
}

func (h *Handler) requireUserID(w http.ResponseWriter, r *http.Request) (string, bool) {
	userID, ok := userIDFromContext(r.Context())
	if !ok || userID == "" {
		h.writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "user not authenticated", "")
		return "", false
	}
	return userID, true
}

func webhookResponseFromModel(webhook *analytics.Webhook) *webhooks.WebhookResponse {
	if webhook == nil {
		return nil
	}

	return &webhooks.WebhookResponse{
		ID:  webhook.ID,
		URL: webhook.URL,
		EventFilter: webhooks.EventFilter{
			EventTypes:   webhook.EventTypes,
			Categories:   webhook.Categories,
			CustomFilter: webhook.CustomFilter,
		},
		Active:       webhook.Active,
		FailureCount: webhook.FailureCount,
		CreatedAt:    webhook.CreatedAt,
		UpdatedAt:    webhook.UpdatedAt,
	}
}

func writeWebhookError(w http.ResponseWriter, h *Handler, err error) {
	switch {
	case err == nil:
		return
	case errors.Is(err, webhooks.ErrWebhookNotFound), errors.Is(err, webhooks.ErrDeliveryNotFound):
		h.writeError(w, http.StatusNotFound, "NOT_FOUND", err.Error(), "")
	case errors.Is(err, webhooks.ErrMaxWebhooksReached), errors.Is(err, webhooks.ErrRateLimited):
		h.writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", err.Error(), "")
	case errors.Is(err, webhooks.ErrWebhookDisabled):
		h.writeError(w, http.StatusConflict, "WEBHOOK_DISABLED", err.Error(), "")
	case errors.Is(err, webhooks.ErrInvalidURL), errors.Is(err, webhooks.ErrInvalidEventTypes), errors.Is(err, webhooks.ErrInvalidFilter):
		h.writeError(w, http.StatusBadRequest, "INVALID_WEBHOOK", err.Error(), "")
	default:
		h.writeError(w, http.StatusInternalServerError, "WEBHOOK_ERROR", "webhook operation failed", err.Error())
	}
}

// RegisterWebhook handles POST /api/v1/analytics/webhooks
func (h *Handler) RegisterWebhook(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	manager := h.requireWebhookManager(w)
	if manager == nil {
		return
	}

	var req RegisterWebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body", err.Error())
		return
	}

	// Validate request
	webhookReq := &webhooks.WebhookRequest{
		URL:         req.URL,
		EventFilter: req.EventFilter,
		Secret:      req.Secret,
		Active:      req.Active,
	}

	if webhookReq.Active == false && (req.Active != false) {
		webhookReq.Active = true
	}

	userID, ok := h.requireUserID(w, r)
	if !ok {
		return
	}

	webhook, err := manager.RegisterWebhook(ctx, userID, webhookReq)
	if err != nil {
		writeWebhookError(w, h, err)
		return
	}

	h.writeResponse(w, http.StatusCreated, webhookResponseFromModel(webhook), nil, 0, false)
}

// ListWebhooks handles GET /api/v1/analytics/webhooks
func (h *Handler) ListWebhooks(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	manager := h.requireWebhookManager(w)
	if manager == nil {
		return
	}

	userID, ok := h.requireUserID(w, r)
	if !ok {
		return
	}

	items, err := manager.ListWebhooks(ctx, userID)
	if err != nil {
		writeWebhookError(w, h, err)
		return
	}

	resp := make([]*webhooks.WebhookResponse, 0, len(items))
	for _, item := range items {
		resp = append(resp, webhookResponseFromModel(item))
	}

	h.writeResponse(w, http.StatusOK, ListWebhooksResponse{Data: resp, Total: len(resp)}, nil, 0, false)
}

// GetWebhook handles GET /api/v1/analytics/webhooks/:id
func (h *Handler) GetWebhook(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	manager := h.requireWebhookManager(w)
	if manager == nil {
		return
	}

	webhookID := routeParam(r, "id")
	if webhookID == "" {
		h.writeError(w, http.StatusBadRequest, "MISSING_PARAM", "missing webhook id", "")
		return
	}

	userID, ok := h.requireUserID(w, r)
	if !ok {
		return
	}

	webhook, err := manager.GetWebhook(ctx, webhookID)
	if err != nil {
		writeWebhookError(w, h, err)
		return
	}

	if webhook.UserID != userID {
		h.writeError(w, http.StatusForbidden, "FORBIDDEN", "cannot access webhook owned by another user", "")
		return
	}

	h.writeResponse(w, http.StatusOK, webhookResponseFromModel(webhook), nil, 0, false)
}

// UpdateWebhook handles PUT /api/v1/analytics/webhooks/:id
func (h *Handler) UpdateWebhook(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	manager := h.requireWebhookManager(w)
	if manager == nil {
		return
	}

	webhookID := routeParam(r, "id")
	if webhookID == "" {
		h.writeError(w, http.StatusBadRequest, "MISSING_PARAM", "missing webhook id", "")
		return
	}

	var req RegisterWebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body", err.Error())
		return
	}

	userID, ok := h.requireUserID(w, r)
	if !ok {
		return
	}

	webhookReq := &webhooks.WebhookRequest{
		URL:         req.URL,
		EventFilter: req.EventFilter,
		Secret:      req.Secret,
		Active:      req.Active,
	}

	webhook, err := manager.UpdateWebhook(ctx, userID, webhookID, webhookReq)
	if err != nil {
		writeWebhookError(w, h, err)
		return
	}

	h.writeResponse(w, http.StatusOK, webhookResponseFromModel(webhook), nil, 0, false)
}

// DeleteWebhook handles DELETE /api/v1/analytics/webhooks/:id
func (h *Handler) DeleteWebhook(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	manager := h.requireWebhookManager(w)
	if manager == nil {
		return
	}

	webhookID := routeParam(r, "id")
	if webhookID == "" {
		h.writeError(w, http.StatusBadRequest, "MISSING_PARAM", "missing webhook id", "")
		return
	}

	userID, ok := h.requireUserID(w, r)
	if !ok {
		return
	}

	if err := manager.DeleteWebhook(ctx, userID, webhookID); err != nil {
		writeWebhookError(w, h, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// TestWebhook handles POST /api/v1/analytics/webhooks/:id/test
func (h *Handler) TestWebhook(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	manager := h.requireWebhookManager(w)
	if manager == nil {
		return
	}

	webhookID := routeParam(r, "id")
	if webhookID == "" {
		h.writeError(w, http.StatusBadRequest, "MISSING_PARAM", "missing webhook id", "")
		return
	}

	userID, ok := h.requireUserID(w, r)
	if !ok {
		return
	}

	webhook, err := manager.GetWebhook(ctx, webhookID)
	if err != nil {
		writeWebhookError(w, h, err)
		return
	}

	if webhook.UserID != userID {
		h.writeError(w, http.StatusForbidden, "FORBIDDEN", "cannot test webhook owned by another user", "")
		return
	}

	deliveryID, err := manager.TestWebhook(ctx, webhookID)
	if err != nil {
		writeWebhookError(w, h, err)
		return
	}

	h.writeResponse(w, http.StatusOK, map[string]string{
		"delivery_id": deliveryID,
		"status":      "queued_test_delivery",
	}, nil, 0, false)
}

// GetWebhookDeliveries handles GET /api/v1/analytics/webhooks/:id/deliveries
func (h *Handler) GetWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	manager := h.requireWebhookManager(w)
	if manager == nil {
		return
	}

	webhookID := routeParam(r, "id")
	if webhookID == "" {
		h.writeError(w, http.StatusBadRequest, "MISSING_PARAM", "missing webhook id", "")
		return
	}

	userID, ok := h.requireUserID(w, r)
	if !ok {
		return
	}

	webhook, err := manager.GetWebhook(ctx, webhookID)
	if err != nil {
		writeWebhookError(w, h, err)
		return
	}

	if webhook.UserID != userID {
		h.writeError(w, http.StatusForbidden, "FORBIDDEN", "cannot access deliveries for webhook owned by another user", "")
		return
	}

	limit := 50
	if value := r.URL.Query().Get("limit"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	deliveries, err := manager.GetDeliveryHistory(ctx, webhookID, limit)
	if err != nil {
		writeWebhookError(w, h, err)
		return
	}

	h.writeResponse(w, http.StatusOK, map[string]interface{}{
		"data":  deliveries,
		"total": len(deliveries),
	}, nil, 0, false)
}

// ReplayDelivery handles POST /api/v1/analytics/webhooks/deliveries/:id/replay
func (h *Handler) ReplayDelivery(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	manager := h.requireWebhookManager(w)
	if manager == nil {
		return
	}

	deliveryID := routeParam(r, "id")
	if deliveryID == "" {
		h.writeError(w, http.StatusBadRequest, "MISSING_PARAM", "missing delivery id", "")
		return
	}

	userID, ok := h.requireUserID(w, r)
	if !ok {
		return
	}

	delivery, err := manager.GetDelivery(ctx, deliveryID)
	if err != nil {
		writeWebhookError(w, h, err)
		return
	}

	webhook, err := manager.GetWebhook(ctx, delivery.WebhookID)
	if err != nil {
		writeWebhookError(w, h, err)
		return
	}

	if webhook.UserID != userID {
		h.writeError(w, http.StatusForbidden, "FORBIDDEN", "cannot replay delivery for webhook owned by another user", "")
		return
	}

	if err := manager.ReplayEvent(ctx, deliveryID); err != nil {
		writeWebhookError(w, h, err)
		return
	}

	h.writeResponse(w, http.StatusAccepted, map[string]string{
		"delivery_id": deliveryID,
		"status":      "replay_queued",
	}, nil, 0, false)
}


