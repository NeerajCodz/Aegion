// Package handler provides operator management handlers for the admin module.
package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/aegion/aegion/modules/admin/service"
	"github.com/aegion/aegion/modules/admin/store"
)

// OperatorResponse represents an operator in API responses.
type OperatorResponse struct {
	ID          string                 `json:"id"`
	IdentityID  string                 `json:"identity_id"`
	Role        string                 `json:"role"`
	Permissions map[string]interface{} `json:"permissions,omitempty"`
	CreatedAt   string                 `json:"created_at"`
	UpdatedAt   string                 `json:"updated_at"`
}

// CreateOperatorRequest is the request body for creating an operator.
type CreateOperatorRequest struct {
	IdentityID  string                 `json:"identity_id"`
	Role        string                 `json:"role"`
	Permissions map[string]interface{} `json:"permissions,omitempty"`
}

// UpdateOperatorRequest is the request body for updating an operator.
type UpdateOperatorRequest struct {
	Role        string                 `json:"role,omitempty"`
	Permissions map[string]interface{} `json:"permissions,omitempty"`
}

// operatorToResponse converts a store operator to an API response.
func operatorToResponse(op *store.Operator) OperatorResponse {
	return OperatorResponse{
		ID:          op.ID.String(),
		IdentityID:  op.IdentityID.String(),
		Role:        op.Role,
		Permissions: op.Permissions,
		CreatedAt:   op.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:   op.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}
}

// ListOperators handles GET /admin/operators
func (h *Handler) ListOperators(w http.ResponseWriter, r *http.Request) {
	operator := OperatorFromContext(r.Context())
	if operator == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	page, perPage, offset := parsePagination(r)

	// List operators
	operators, total, err := h.service.ListOperators(r.Context(), operator.ID, perPage, offset)
	if err != nil {
		if errors.Is(err, service.ErrPermissionDenied) {
			writeError(w, http.StatusForbidden, "insufficient_permissions", "You do not have permission to list operators")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to list operators")
		return
	}

	// Convert to response format
	items := make([]OperatorResponse, len(operators))
	for i, op := range operators {
		items[i] = operatorToResponse(op)
	}

	resp := ListResponse{
		Items:      items,
		Pagination: buildPaginationMeta(page, perPage, total),
	}
	writeJSON(w, http.StatusOK, resp)
}

// GetOperator handles GET /admin/operators/{id}
func (h *Handler) GetOperator(w http.ResponseWriter, r *http.Request) {
	operator := OperatorFromContext(r.Context())
	if operator == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	idStr := chi.URLParam(r, "id")
	operatorID, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "Invalid operator ID format")
		return
	}

	// Get operator
	op, err := h.service.GetOperator(r.Context(), operator.ID, operatorID)
	if err != nil {
		if errors.Is(err, service.ErrPermissionDenied) {
			writeError(w, http.StatusForbidden, "insufficient_permissions", "You do not have permission to view this operator")
			return
		}
		if errors.Is(err, store.ErrOperatorNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Operator not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to get operator")
		return
	}

	writeJSON(w, http.StatusOK, operatorToResponse(op))
}

// CreateOperator handles POST /admin/operators
func (h *Handler) CreateOperator(w http.ResponseWriter, r *http.Request) {
	operator := OperatorFromContext(r.Context())
	if operator == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	var req CreateOperatorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	// Validate required fields
	if req.IdentityID == "" {
		writeError(w, http.StatusBadRequest, "missing_identity_id", "Identity ID is required")
		return
	}

	if req.Role == "" {
		writeError(w, http.StatusBadRequest, "missing_role", "Role is required")
		return
	}

	// Parse identity ID
	identityID, err := uuid.Parse(req.IdentityID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_identity_id", "Invalid identity ID format")
		return
	}

	// Get IP address for audit logging
	ipAddress := IPAddressFromContext(r.Context())

	// Create operator
	newOperator, err := h.service.CreateOperator(r.Context(), operator.ID, identityID, req.Role, req.Permissions, ipAddress)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrPermissionDenied):
			writeError(w, http.StatusForbidden, "insufficient_permissions", "You do not have permission to create operators")
		case errors.Is(err, service.ErrInvalidRole):
			writeError(w, http.StatusBadRequest, "invalid_role", "Invalid role. Valid roles are: super_admin, admin, operator, viewer")
		case errors.Is(err, store.ErrDuplicateOperator):
			writeError(w, http.StatusConflict, "duplicate_operator", "An operator already exists for this identity")
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to create operator")
		}
		return
	}

	writeJSON(w, http.StatusCreated, operatorToResponse(newOperator))
}

// UpdateOperator handles PATCH /admin/operators/{id}
func (h *Handler) UpdateOperator(w http.ResponseWriter, r *http.Request) {
	operator := OperatorFromContext(r.Context())
	if operator == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	idStr := chi.URLParam(r, "id")
	operatorID, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "Invalid operator ID format")
		return
	}

	var req UpdateOperatorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	// Get IP address for audit logging
	ipAddress := IPAddressFromContext(r.Context())

	// Update operator
	updatedOperator, err := h.service.UpdateOperator(r.Context(), operator.ID, operatorID, req.Role, req.Permissions, ipAddress)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrPermissionDenied):
			writeError(w, http.StatusForbidden, "insufficient_permissions", "You do not have permission to update this operator")
		case errors.Is(err, service.ErrSelfDemotion):
			writeError(w, http.StatusForbidden, "self_demotion", "You cannot demote your own super_admin account")
		case errors.Is(err, service.ErrInvalidRole):
			writeError(w, http.StatusBadRequest, "invalid_role", "Invalid role. Valid roles are: super_admin, admin, operator, viewer")
		case errors.Is(err, store.ErrOperatorNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Operator not found")
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to update operator")
		}
		return
	}

	writeJSON(w, http.StatusOK, operatorToResponse(updatedOperator))
}

// DeleteOperator handles DELETE /admin/operators/{id}
func (h *Handler) DeleteOperator(w http.ResponseWriter, r *http.Request) {
	operator := OperatorFromContext(r.Context())
	if operator == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	idStr := chi.URLParam(r, "id")
	operatorID, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "Invalid operator ID format")
		return
	}

	// Get IP address for audit logging
	ipAddress := IPAddressFromContext(r.Context())

	// Delete operator
	if err := h.service.DeleteOperator(r.Context(), operator.ID, operatorID, ipAddress); err != nil {
		switch {
		case errors.Is(err, service.ErrPermissionDenied):
			writeError(w, http.StatusForbidden, "insufficient_permissions", "You do not have permission to delete this operator")
		case errors.Is(err, service.ErrSelfDeletion):
			writeError(w, http.StatusForbidden, "self_deletion", "You cannot delete your own operator account")
		case errors.Is(err, store.ErrOperatorNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Operator not found")
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to delete operator")
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ListAuditLogs handles GET /admin/audit
func (h *Handler) ListAuditLogs(w http.ResponseWriter, r *http.Request) {
	operator := OperatorFromContext(r.Context())
	if operator == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	page, perPage, offset := parsePagination(r)
	if perPage > 500 {
		perPage = 500
	}

	// Build filter from query parameters
	filter := store.AuditFilter{}

	if opID := r.URL.Query().Get("operator_id"); opID != "" {
		if parsed, err := uuid.Parse(opID); err == nil {
			filter.OperatorID = &parsed
		}
	}

	if action := r.URL.Query().Get("action"); action != "" {
		filter.Action = action
	}

	if resourceType := r.URL.Query().Get("resource_type"); resourceType != "" {
		filter.ResourceType = resourceType
	}

	if resourceID := r.URL.Query().Get("resource_id"); resourceID != "" {
		filter.ResourceID = resourceID
	}

	// List audit logs
	entries, total, err := h.service.ListAuditLogs(r.Context(), operator.ID, filter, perPage, offset)
	if err != nil {
		if errors.Is(err, service.ErrPermissionDenied) {
			writeError(w, http.StatusForbidden, "insufficient_permissions", "You do not have permission to view audit logs")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to list audit logs")
		return
	}

	// Convert to response format
	items := make([]map[string]interface{}, len(entries))
	for i, entry := range entries {
		items[i] = map[string]interface{}{
			"id":            entry.ID.String(),
			"operator_id":   nil,
			"action":        entry.Action,
			"resource_type": entry.ResourceType,
			"resource_id":   entry.ResourceID,
			"details":       entry.Details,
			"ip_address":    entry.IPAddress,
			"created_at":    entry.CreatedAt.Format("2006-01-02T15:04:05Z"),
		}
		if entry.OperatorID != nil {
			items[i]["operator_id"] = entry.OperatorID.String()
		}
	}

	resp := ListResponse{
		Items:      items,
		Pagination: buildPaginationMeta(page, perPage, total),
	}
	writeJSON(w, http.StatusOK, resp)
}

// ListRoles handles GET /admin/roles
func (h *Handler) ListRoles(w http.ResponseWriter, r *http.Request) {
	operator := OperatorFromContext(r.Context())
	if operator == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	page, perPage, offset := parsePagination(r)

	// List roles
	roles, total, err := h.service.ListRoles(r.Context(), operator.ID, perPage, offset)
	if err != nil {
		if errors.Is(err, service.ErrPermissionDenied) {
			writeError(w, http.StatusForbidden, "insufficient_permissions", "You do not have permission to view roles")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to list roles")
		return
	}

	// Convert to response format
	items := make([]map[string]interface{}, len(roles))
	for i, role := range roles {
		items[i] = map[string]interface{}{
			"id":          role.ID.String(),
			"name":        role.Name,
			"description": role.Description,
			"permissions": role.Permissions,
			"is_system":   role.IsSystem,
			"created_at":  role.CreatedAt.Format("2006-01-02T15:04:05Z"),
			"updated_at":  role.UpdatedAt.Format("2006-01-02T15:04:05Z"),
		}
	}

	resp := ListResponse{
		Items:      items,
		Pagination: buildPaginationMeta(page, perPage, total),
	}
	writeJSON(w, http.StatusOK, resp)
}

// GetRole handles GET /admin/roles/{name}
func (h *Handler) GetRole(w http.ResponseWriter, r *http.Request) {
	operator := OperatorFromContext(r.Context())
	if operator == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	name := chi.URLParam(r, "name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "missing_name", "Role name is required")
		return
	}

	// Get role
	role, err := h.service.GetRole(r.Context(), operator.ID, name)
	if err != nil {
		if errors.Is(err, service.ErrPermissionDenied) {
			writeError(w, http.StatusForbidden, "insufficient_permissions", "You do not have permission to view this role")
			return
		}
		if errors.Is(err, store.ErrRoleNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Role not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to get role")
		return
	}

	resp := map[string]interface{}{
		"id":          role.ID.String(),
		"name":        role.Name,
		"description": role.Description,
		"permissions": role.Permissions,
		"is_system":   role.IsSystem,
		"created_at":  role.CreatedAt.Format("2006-01-02T15:04:05Z"),
		"updated_at":  role.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}
	writeJSON(w, http.StatusOK, resp)
}
