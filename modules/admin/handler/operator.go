// Package handler provides operator management handlers for the admin module.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/aegion/aegion/modules/admin/service"
	"github.com/aegion/aegion/modules/admin/store"
)

// CreateOperatorRequest is the request body for creating an operator.
type CreateOperatorRequest struct {
	IdentityID  string                 `json:"identity_id"`
	Email       string                 `json:"email"`
	Name        string                 `json:"name"`
	Password    string                 `json:"password"`
	Status      string                 `json:"status"`
	Role        string                 `json:"role"`
	Permissions map[string]interface{} `json:"permissions,omitempty"`
}

// UpdateOperatorRequest is the request body for updating an operator.
type UpdateOperatorRequest struct {
	Name        string                 `json:"name,omitempty"`
	Status      string                 `json:"status,omitempty"`
	Role        string                 `json:"role,omitempty"`
	Permissions map[string]interface{} `json:"permissions,omitempty"`
}

// CreateRoleRequest is the request body for creating a role.
type CreateRoleRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
}

// UpdateRoleRequest is the request body for updating a role.
type UpdateRoleRequest struct {
	Description *string  `json:"description"`
	Permissions []string `json:"permissions"`
}

// ListOperators handles GET /admin/operators
func (h *Handler) ListOperators(w http.ResponseWriter, r *http.Request) {
	operator := OperatorFromContext(r.Context())
	if operator == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	page, perPage, offset := h.parsePagination(r)

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

	// Convert to UI response format
	data := make([]OperatorView, 0, len(operators))
	for _, op := range operators {
		profile, _ := h.service.Store().GetIdentityProfile(r.Context(), op.IdentityID)
		data = append(data, operatorToView(op, profile, nil))
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data":        data,
		"total":       total,
		"page":        page,
		"per_page":    perPage,
		"total_pages": paginationTotalPages(total, perPage),
	})
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

	profile, err := h.service.Store().GetIdentityProfile(r.Context(), op.IdentityID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to get operator profile")
		return
	}

	writeJSON(w, http.StatusOK, operatorToView(op, profile, nil))
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

	if req.Role == "" {
		writeError(w, http.StatusBadRequest, "missing_role", "Role is required")
		return
	}

	ipAddress := IPAddressFromContext(r.Context())
	var identityID uuid.UUID

	if strings.TrimSpace(req.IdentityID) != "" {
		parsedIdentityID, err := uuid.Parse(req.IdentityID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_identity_id", "Invalid identity ID format")
			return
		}
		identityID = parsedIdentityID
	} else {
		if strings.TrimSpace(req.Email) == "" || strings.TrimSpace(req.Password) == "" {
			writeError(w, http.StatusBadRequest, "invalid_request", "email and password are required when identity_id is not provided")
			return
		}

		createdIdentityID, err := h.createOperatorIdentity(r.Context(), req)
		if err != nil {
			switch {
			case errors.Is(err, store.ErrDuplicateOperator):
				writeError(w, http.StatusConflict, "duplicate_identity", "An identity with this email already exists")
			default:
				writeError(w, http.StatusInternalServerError, "internal_error", "Failed to create identity for operator")
			}
			return
		}
		identityID = createdIdentityID
	}

	newOperator, err := h.service.CreateOperator(r.Context(), operator.ID, identityID, req.Role, req.Permissions, ipAddress)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrPermissionDenied):
			writeError(w, http.StatusForbidden, "insufficient_permissions", "You do not have permission to create operators")
		case errors.Is(err, service.ErrInvalidRole):
			writeError(w, http.StatusBadRequest, "invalid_role", "Invalid or unknown role")
		case errors.Is(err, service.ErrInvalidPermission):
			writeError(w, http.StatusBadRequest, "invalid_permissions", "One or more operator permission overrides are invalid")
		case errors.Is(err, store.ErrDuplicateOperator):
			writeError(w, http.StatusConflict, "duplicate_operator", "An operator already exists for this identity")
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to create operator")
		}
		return
	}

	profile, err := h.service.Store().GetIdentityProfile(r.Context(), newOperator.IdentityID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load operator profile")
		return
	}

	writeJSON(w, http.StatusCreated, operatorToView(newOperator, profile, nil))
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
			writeError(w, http.StatusBadRequest, "invalid_role", "Invalid or unknown role")
		case errors.Is(err, service.ErrInvalidPermission):
			writeError(w, http.StatusBadRequest, "invalid_permissions", "One or more operator permission overrides are invalid")
		case errors.Is(err, store.ErrOperatorNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Operator not found")
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to update operator")
		}
		return
	}

	if req.Name != "" || req.Status != "" {
		if err := h.updateIdentityForOperator(r.Context(), updatedOperator.IdentityID, req.Name, req.Status); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to update operator identity profile")
			return
		}
	}

	profile, err := h.service.Store().GetIdentityProfile(r.Context(), updatedOperator.IdentityID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load operator profile")
		return
	}

	writeJSON(w, http.StatusOK, operatorToView(updatedOperator, profile, nil))
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

func (h *Handler) createOperatorIdentity(ctx context.Context, req CreateOperatorRequest) (uuid.UUID, error) {
	email := strings.ToLower(strings.TrimSpace(req.Email))
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = email
	}

	state := "active"
	if strings.EqualFold(strings.TrimSpace(req.Status), "inactive") {
		state = "inactive"
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return uuid.Nil, err
	}

	tx, err := h.dbConn().Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	schemaID, err := resolveDefaultSchemaID(ctx, tx)
	if err != nil {
		return uuid.Nil, err
	}

	identityID := uuid.New()
	traits := map[string]interface{}{
		"email":        email,
		"display_name": name,
		"name":         name,
	}
	traitsJSON, err := json.Marshal(traits)
	if err != nil {
		return uuid.Nil, err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO core_identities (id, schema_id, traits, state, created_at, updated_at)
		VALUES ($1, $2, $3::jsonb, $4, NOW(), NOW())
	`, identityID, schemaID, string(traitsJSON), state)
	if err != nil {
		return uuid.Nil, err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO core_identity_addresses (id, identity_id, type, value, is_primary, verified, created_at, updated_at)
		VALUES ($1, $2, 'email', $3, TRUE, FALSE, NOW(), NOW())
	`, uuid.New(), identityID, email)
	if err != nil {
		return uuid.Nil, err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO pwd_credentials (id, identity_id, identifier, hash, created_at, updated_at)
		VALUES ($1, $2, $3, $4, NOW(), NOW())
	`, uuid.New(), identityID, email, string(hashedPassword))
	if err != nil {
		return uuid.Nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, err
	}

	return identityID, nil
}

func resolveDefaultSchemaID(ctx context.Context, tx pgx.Tx) (uuid.UUID, error) {
	var schemaID uuid.UUID
	err := tx.QueryRow(ctx, `
		SELECT id
		FROM core_identity_schemas
		ORDER BY is_default DESC, created_at ASC
		LIMIT 1
	`).Scan(&schemaID)
	if err == nil {
		return schemaID, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, err
	}

	schemaID = uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO core_identity_schemas (id, name, is_default, schema, created_at, updated_at)
		VALUES ($1, $2, TRUE, '{}'::jsonb, NOW(), NOW())
	`, schemaID, "default")
	if err != nil {
		return uuid.Nil, err
	}

	return schemaID, nil
}

func (h *Handler) updateIdentityForOperator(ctx context.Context, identityID uuid.UUID, name, status string) error {
	if strings.TrimSpace(name) == "" && strings.TrimSpace(status) == "" {
		return nil
	}

	setClauses := []string{"updated_at = NOW()"}
	args := []interface{}{}
	argPos := 1

	if strings.TrimSpace(name) != "" {
		name = strings.TrimSpace(name)
		patch := map[string]interface{}{
			"display_name": name,
			"name":         name,
		}
		patchJSON, err := json.Marshal(patch)
		if err != nil {
			return err
		}
		setClauses = append(setClauses, "traits = COALESCE(traits, '{}'::jsonb) || $"+strconv.Itoa(argPos)+"::jsonb")
		args = append(args, string(patchJSON))
		argPos++
	}

	if strings.TrimSpace(status) != "" {
		dbState := "active"
		if strings.EqualFold(strings.TrimSpace(status), "inactive") {
			dbState = "inactive"
		}
		setClauses = append(setClauses, "state = $"+strconv.Itoa(argPos))
		args = append(args, dbState)
		argPos++
	}

	query := `
		UPDATE core_identities
		SET ` + strings.Join(setClauses, ", ") + `
		WHERE id = $` + strconv.Itoa(argPos) + `
		  AND deleted_at IS NULL
	`
	args = append(args, identityID)

	_, err := h.dbConn().Exec(ctx, query, args...)
	return err
}

// ListAuditLogs handles GET /admin/audit
func (h *Handler) ListAuditLogs(w http.ResponseWriter, r *http.Request) {
	operator := OperatorFromContext(r.Context())
	if operator == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	page, perPage, offset := h.parsePagination(r)
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

	page, perPage, offset := h.parsePagination(r)

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

	writeJSON(w, http.StatusOK, roleToResponse(role))
}

// ListPermissions handles GET /admin/roles/permissions
func (h *Handler) ListPermissions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data": h.service.AvailablePermissions(),
	})
}

// CreateRole handles POST /admin/roles
func (h *Handler) CreateRole(w http.ResponseWriter, r *http.Request) {
	operator := OperatorFromContext(r.Context())
	if operator == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	var req CreateRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	role, err := h.service.CreateRole(
		r.Context(),
		operator.ID,
		req.Name,
		req.Description,
		req.Permissions,
		IPAddressFromContext(r.Context()),
	)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrPermissionDenied):
			writeError(w, http.StatusForbidden, "insufficient_permissions", "You do not have permission to create roles")
		case errors.Is(err, service.ErrInvalidRoleName):
			writeError(w, http.StatusBadRequest, "invalid_role_name", "Role name must be lowercase alphanumeric with underscores")
		case errors.Is(err, service.ErrInvalidPermission):
			writeError(w, http.StatusBadRequest, "invalid_permissions", "One or more permissions are invalid")
		case errors.Is(err, store.ErrDuplicateRole):
			writeError(w, http.StatusConflict, "conflict", "Role already exists")
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to create role")
		}
		return
	}

	writeJSON(w, http.StatusCreated, roleToResponse(role))
}

// UpdateRole handles PATCH /admin/roles/{name}
func (h *Handler) UpdateRole(w http.ResponseWriter, r *http.Request) {
	operator := OperatorFromContext(r.Context())
	if operator == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	name := strings.TrimSpace(chi.URLParam(r, "name"))
	if name == "" {
		writeError(w, http.StatusBadRequest, "missing_name", "Role name is required")
		return
	}

	var req UpdateRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	permissions := req.Permissions
	if req.Permissions == nil {
		permissions = nil
	}

	role, err := h.service.UpdateRole(
		r.Context(),
		operator.ID,
		name,
		req.Description,
		permissions,
		IPAddressFromContext(r.Context()),
	)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrPermissionDenied):
			writeError(w, http.StatusForbidden, "insufficient_permissions", "You do not have permission to update this role")
		case errors.Is(err, service.ErrInvalidRoleName):
			writeError(w, http.StatusBadRequest, "invalid_role_name", "Role name must be lowercase alphanumeric with underscores")
		case errors.Is(err, service.ErrInvalidPermission):
			writeError(w, http.StatusBadRequest, "invalid_permissions", "One or more permissions are invalid")
		case errors.Is(err, store.ErrRoleNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Role not found")
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to update role")
		}
		return
	}

	writeJSON(w, http.StatusOK, roleToResponse(role))
}

// DeleteRole handles DELETE /admin/roles/{name}
func (h *Handler) DeleteRole(w http.ResponseWriter, r *http.Request) {
	operator := OperatorFromContext(r.Context())
	if operator == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	name := strings.TrimSpace(chi.URLParam(r, "name"))
	if name == "" {
		writeError(w, http.StatusBadRequest, "missing_name", "Role name is required")
		return
	}

	if err := h.service.DeleteRole(
		r.Context(),
		operator.ID,
		name,
		IPAddressFromContext(r.Context()),
	); err != nil {
		switch {
		case errors.Is(err, service.ErrPermissionDenied):
			writeError(w, http.StatusForbidden, "insufficient_permissions", "You do not have permission to delete this role")
		case errors.Is(err, service.ErrInvalidRoleName):
			writeError(w, http.StatusBadRequest, "invalid_role_name", "Role name must be lowercase alphanumeric with underscores")
		case errors.Is(err, service.ErrRoleInUse):
			writeError(w, http.StatusConflict, "role_in_use", "Role is assigned to one or more operators")
		case errors.Is(err, store.ErrRoleNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Role not found")
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to delete role")
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func roleToResponse(role *store.Role) map[string]interface{} {
	return map[string]interface{}{
		"id":          role.ID.String(),
		"name":        role.Name,
		"description": role.Description,
		"permissions": role.Permissions,
		"is_system":   role.IsSystem,
		"created_at":  role.CreatedAt.Format("2006-01-02T15:04:05Z"),
		"updated_at":  role.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}
}
