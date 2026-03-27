// Package scim provides SCIM 2.0 HTTP handlers.
package scim

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

// Handler handles SCIM 2.0 HTTP requests.
type Handler struct {
	service *Service
}

// NewHandler creates a new SCIM handler.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// RegisterRoutes registers SCIM 2.0 routes.
func (h *Handler) RegisterRoutes(r chi.Router) {
	// Apply SCIM authentication middleware
	r.Use(h.authMiddleware)

	// Service Provider Config
	r.Get("/ServiceProviderConfig", h.GetServiceProviderConfig)

	// Schemas
	r.Get("/Schemas", h.GetSchemas)
	r.Get("/Schemas/{id}", h.GetSchema)

	// Users
	r.Route("/Users", func(r chi.Router) {
		r.Get("/", h.requirePermission("users:read")(h.ListUsers))
		r.Post("/", h.requirePermission("users:write")(h.CreateUser))
		r.Get("/{id}", h.requirePermission("users:read")(h.GetUser))
		r.Put("/{id}", h.requirePermission("users:write")(h.UpdateUser))
		r.Patch("/{id}", h.requirePermission("users:write")(h.PatchUser))
		r.Delete("/{id}", h.requirePermission("users:write")(h.DeleteUser))
	})

	// Groups
	r.Route("/Groups", func(r chi.Router) {
		r.Get("/", h.requirePermission("groups:read")(h.ListGroups))
		r.Post("/", h.requirePermission("groups:write")(h.CreateGroup))
		r.Get("/{id}", h.requirePermission("groups:read")(h.GetGroup))
		r.Put("/{id}", h.requirePermission("groups:write")(h.UpdateGroup))
		r.Patch("/{id}", h.requirePermission("groups:write")(h.PatchGroup))
		r.Delete("/{id}", h.requirePermission("groups:write")(h.DeleteGroup))
	})
}

// authMiddleware validates SCIM authentication tokens.
func (h *Handler) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract token from Authorization header
		auth := r.Header.Get("Authorization")
		if auth == "" {
			h.writeError(w, http.StatusUnauthorized, "invalidCredentials", "Authentication required")
			return
		}

		if !strings.HasPrefix(auth, "Bearer ") {
			h.writeError(w, http.StatusUnauthorized, "invalidCredentials", "Invalid authentication method")
			return
		}

		token := strings.TrimPrefix(auth, "Bearer ")
		
		// Validate token
		scimToken, err := h.service.ValidateToken(r.Context(), token)
		if err != nil {
			h.writeError(w, http.StatusUnauthorized, "invalidCredentials", "Invalid or expired token")
			return
		}

		// Store token in context
		ctx := contextWithSCIMToken(r.Context(), scimToken)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requirePermission returns middleware that checks for a specific permission.
func (h *Handler) requirePermission(permission string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			token := scimTokenFromContext(r.Context())
			if token == nil {
				h.writeError(w, http.StatusUnauthorized, "invalidCredentials", "Authentication required")
				return
			}

			if !h.service.HasPermission(token, permission) {
				h.writeError(w, http.StatusForbidden, "insufficientRights", "Insufficient permissions")
				return
			}

			next(w, r)
		}
	}
}

// Service Provider Config

// GetServiceProviderConfig returns the service provider configuration.
func (h *Handler) GetServiceProviderConfig(w http.ResponseWriter, r *http.Request) {
	config := h.service.GetServiceProviderConfig()
	h.writeJSON(w, http.StatusOK, config)
}

// Schemas

// GetSchemas returns all supported schemas.
func (h *Handler) GetSchemas(w http.ResponseWriter, r *http.Request) {
	schemas := h.service.GetSchemas()
	
	response := &ListResponse{
		Schemas:      []string{SchemaListResponse},
		TotalResults: len(schemas),
		ItemsPerPage: len(schemas),
		StartIndex:   1,
		Resources:    schemas,
	}
	
	h.writeJSON(w, http.StatusOK, response)
}

// GetSchema returns a specific schema.
func (h *Handler) GetSchema(w http.ResponseWriter, r *http.Request) {
	schemaID := chi.URLParam(r, "id")
	schemas := h.service.GetSchemas()
	
	for _, schema := range schemas {
		if schema.ID == schemaID {
			h.writeJSON(w, http.StatusOK, schema)
			return
		}
	}
	
	h.writeError(w, http.StatusNotFound, "notFound", "Schema not found")
}

// User operations

// ListUsers handles GET /Users.
func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	filter := r.URL.Query().Get("filter")
	sortBy := r.URL.Query().Get("sortBy")
	sortOrder := SortAscending
	if r.URL.Query().Get("sortOrder") == "descending" {
		sortOrder = SortDescending
	}
	
	startIndex := 1
	if si := r.URL.Query().Get("startIndex"); si != "" {
		if parsed, err := strconv.Atoi(si); err == nil && parsed > 0 {
			startIndex = parsed
		}
	}
	
	count := 20
	if c := r.URL.Query().Get("count"); c != "" {
		if parsed, err := strconv.Atoi(c); err == nil && parsed > 0 {
			count = parsed
		}
	}

	response, err := h.service.ListUsers(r.Context(), filter, sortBy, sortOrder, startIndex, count)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "internalError", err.Error())
		return
	}

	h.writeJSON(w, http.StatusOK, response)
}

// GetUser handles GET /Users/{id}.
func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	user, err := h.service.GetUser(r.Context(), id)
	if err != nil {
		h.writeError(w, http.StatusNotFound, "notFound", "User not found")
		return
	}

	h.writeJSON(w, http.StatusOK, user)
}

// CreateUser handles POST /Users.
func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var user SCIMUser
	if err := json.NewDecoder(r.Body).Decode(&user); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalidSyntax", "Invalid JSON")
		return
	}

	createdUser, err := h.service.CreateUser(r.Context(), &user)
	if err != nil {
		if strings.Contains(err.Error(), "userName is required") {
			h.writeError(w, http.StatusBadRequest, "invalidValue", err.Error())
			return
		}
		if strings.Contains(err.Error(), "already exists") {
			h.writeError(w, http.StatusConflict, "uniqueness", err.Error())
			return
		}
		h.writeError(w, http.StatusInternalServerError, "internalError", err.Error())
		return
	}

	h.writeJSON(w, http.StatusCreated, createdUser)
}

// UpdateUser handles PUT /Users/{id}.
func (h *Handler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var user SCIMUser
	if err := json.NewDecoder(r.Body).Decode(&user); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalidSyntax", "Invalid JSON")
		return
	}

	updatedUser, err := h.service.UpdateUser(r.Context(), id, &user)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			h.writeError(w, http.StatusNotFound, "notFound", "User not found")
			return
		}
		h.writeError(w, http.StatusInternalServerError, "internalError", err.Error())
		return
	}

	h.writeJSON(w, http.StatusOK, updatedUser)
}

// PatchUser handles PATCH /Users/{id}.
func (h *Handler) PatchUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var patchReq PatchRequest
	if err := json.NewDecoder(r.Body).Decode(&patchReq); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalidSyntax", "Invalid JSON")
		return
	}

	// Validate patch operations
	if len(patchReq.Operations) == 0 {
		h.writeError(w, http.StatusBadRequest, "invalidValue", "No operations provided")
		return
	}

	user, err := h.service.PatchUser(r.Context(), id, patchReq.Operations)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			h.writeError(w, http.StatusNotFound, "notFound", "User not found")
			return
		}
		if strings.Contains(err.Error(), "invalid operation") {
			h.writeError(w, http.StatusBadRequest, "invalidValue", err.Error())
			return
		}
		h.writeError(w, http.StatusInternalServerError, "internalError", err.Error())
		return
	}

	h.writeJSON(w, http.StatusOK, user)
}

// DeleteUser handles DELETE /Users/{id}.
func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	err := h.service.DeleteUser(r.Context(), id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			h.writeError(w, http.StatusNotFound, "notFound", "User not found")
			return
		}
		h.writeError(w, http.StatusInternalServerError, "internalError", err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Group operations

// ListGroups handles GET /Groups.
func (h *Handler) ListGroups(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	filter := r.URL.Query().Get("filter")
	sortBy := r.URL.Query().Get("sortBy")
	sortOrder := SortAscending
	if r.URL.Query().Get("sortOrder") == "descending" {
		sortOrder = SortDescending
	}
	
	startIndex := 1
	if si := r.URL.Query().Get("startIndex"); si != "" {
		if parsed, err := strconv.Atoi(si); err == nil && parsed > 0 {
			startIndex = parsed
		}
	}
	
	count := 20
	if c := r.URL.Query().Get("count"); c != "" {
		if parsed, err := strconv.Atoi(c); err == nil && parsed > 0 {
			count = parsed
		}
	}

	response, err := h.service.ListGroups(r.Context(), filter, sortBy, sortOrder, startIndex, count)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "internalError", err.Error())
		return
	}

	h.writeJSON(w, http.StatusOK, response)
}

// GetGroup handles GET /Groups/{id}.
func (h *Handler) GetGroup(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	group, err := h.service.GetGroup(r.Context(), id)
	if err != nil {
		h.writeError(w, http.StatusNotFound, "notFound", "Group not found")
		return
	}

	h.writeJSON(w, http.StatusOK, group)
}

// CreateGroup handles POST /Groups.
func (h *Handler) CreateGroup(w http.ResponseWriter, r *http.Request) {
	var group SCIMGroup
	if err := json.NewDecoder(r.Body).Decode(&group); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalidSyntax", "Invalid JSON")
		return
	}

	createdGroup, err := h.service.CreateGroup(r.Context(), &group)
	if err != nil {
		if strings.Contains(err.Error(), "displayName is required") {
			h.writeError(w, http.StatusBadRequest, "invalidValue", err.Error())
			return
		}
		if strings.Contains(err.Error(), "already exists") {
			h.writeError(w, http.StatusConflict, "uniqueness", err.Error())
			return
		}
		h.writeError(w, http.StatusInternalServerError, "internalError", err.Error())
		return
	}

	h.writeJSON(w, http.StatusCreated, createdGroup)
}

// UpdateGroup handles PUT /Groups/{id}.
func (h *Handler) UpdateGroup(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var group SCIMGroup
	if err := json.NewDecoder(r.Body).Decode(&group); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalidSyntax", "Invalid JSON")
		return
	}

	updatedGroup, err := h.service.UpdateGroup(r.Context(), id, &group)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			h.writeError(w, http.StatusNotFound, "notFound", "Group not found")
			return
		}
		h.writeError(w, http.StatusInternalServerError, "internalError", err.Error())
		return
	}

	h.writeJSON(w, http.StatusOK, updatedGroup)
}

// PatchGroup handles PATCH /Groups/{id}.
func (h *Handler) PatchGroup(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var patchReq PatchRequest
	if err := json.NewDecoder(r.Body).Decode(&patchReq); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalidSyntax", "Invalid JSON")
		return
	}

	// Validate patch operations
	if len(patchReq.Operations) == 0 {
		h.writeError(w, http.StatusBadRequest, "invalidValue", "No operations provided")
		return
	}

	group, err := h.service.PatchGroup(r.Context(), id, patchReq.Operations)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			h.writeError(w, http.StatusNotFound, "notFound", "Group not found")
			return
		}
		if strings.Contains(err.Error(), "invalid operation") {
			h.writeError(w, http.StatusBadRequest, "invalidValue", err.Error())
			return
		}
		h.writeError(w, http.StatusInternalServerError, "internalError", err.Error())
		return
	}

	h.writeJSON(w, http.StatusOK, group)
}

// DeleteGroup handles DELETE /Groups/{id}.
func (h *Handler) DeleteGroup(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	err := h.service.DeleteGroup(r.Context(), id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			h.writeError(w, http.StatusNotFound, "notFound", "Group not found")
			return
		}
		h.writeError(w, http.StatusInternalServerError, "internalError", err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Utility methods

// writeJSON writes a JSON response.
func (h *Handler) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/scim+json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// writeError writes a SCIM error response.
func (h *Handler) writeError(w http.ResponseWriter, status int, scimType, detail string) {
	errorResp := ErrorResponse{
		Schemas:  []string{SchemaError},
		ScimType: scimType,
		Detail:   detail,
		Status:   strconv.Itoa(status),
	}

	w.Header().Set("Content-Type", "application/scim+json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(errorResp)
}