// Package scim provides SCIM 2.0 HTTP handlers.
package scim

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/aegion/aegion/internal/platform/observability"
)

const maxSCIMJSONBodyBytes int64 = 1 << 20

// Handler handles SCIM 2.0 HTTP requests.
type Handler struct {
	service *Service
	config  HandlerConfig
	log     *slog.Logger
}

// HandlerConfig configures SCIM HTTP handler behavior.
type HandlerConfig struct {
	DefaultPageSize int
	MaxPageSize     int
	Logger          *slog.Logger
}

// DefaultHandlerConfig returns default SCIM handler settings.
func DefaultHandlerConfig() HandlerConfig {
	return HandlerConfig{
		DefaultPageSize: 20,
		MaxPageSize:     1000,
	}
}

// NewHandler creates a new SCIM handler.
func NewHandler(service *Service, cfgOverride ...HandlerConfig) *Handler {
	cfg := DefaultHandlerConfig()
	if len(cfgOverride) > 0 {
		cfg = cfgOverride[0]
	}
	if service != nil {
		if cfg.DefaultPageSize == 0 {
			cfg.DefaultPageSize = service.DefaultPageSize()
		}
		if cfg.MaxPageSize == 0 {
			cfg.MaxPageSize = service.MaxPageSize()
		}
	}
	if cfg.DefaultPageSize == 0 {
		cfg.DefaultPageSize = 20
	}
	if cfg.MaxPageSize == 0 {
		cfg.MaxPageSize = 1000
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return &Handler{service: service, config: cfg, log: logger.With("component", "admin.scim.handler")}
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

		token := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))

		// Validate token
		scimToken, err := h.service.ValidateToken(r.Context(), token)
		if err != nil {
			h.log.WarnContext(r.Context(), "SCIM authentication failed", "error", err)
			h.writeError(w, http.StatusUnauthorized, "invalidCredentials", "Invalid or expired token")
			return
		}

		// Store token in context
		ctx := contextWithSCIMToken(r.Context(), scimToken)
		ctx = observability.WithUserID(ctx, "scim:"+scimToken.ID.String())
		ctx = observability.WithSessionID(ctx, scimToken.ID.String())
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requirePermission returns middleware that checks for a specific permission.
func (h *Handler) requirePermission(permission string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			token := scimTokenFromContext(r.Context())
			if token == nil {
				h.log.WarnContext(r.Context(), "SCIM permission denied: no token in context", "permission", permission)
				h.writeError(w, http.StatusUnauthorized, "invalidCredentials", "Authentication required")
				return
			}

			if !h.service.HasPermission(token, permission) {
				h.log.WarnContext(r.Context(), "SCIM permission denied", "permission", permission, "token_id", token.ID.String())
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

	startIndex, count := h.parsePagination(r)

	response, err := h.service.ListUsers(r.Context(), filter, sortBy, sortOrder, startIndex, count)
	if err != nil {
		h.log.ErrorContext(r.Context(), "SCIM list users failed", "error", err)
		h.writeError(w, http.StatusInternalServerError, "internalError", "Failed to list users")
		return
	}

	h.writeJSON(w, http.StatusOK, response)
}

// GetUser handles GET /Users/{id}.
func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	user, err := h.service.GetUser(r.Context(), id)
	if err != nil {
		h.log.WarnContext(r.Context(), "SCIM get user failed", "user_id", id, "error", err)
		h.writeError(w, http.StatusNotFound, "notFound", "User not found")
		return
	}

	h.writeResourceETag(w, user.Meta)
	h.writeJSON(w, http.StatusOK, user)
}

// CreateUser handles POST /Users.
func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var user SCIMUser
	if err := h.decodeJSONBody(w, r, &user); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalidSyntax", "Invalid JSON")
		return
	}

	createdUser, err := h.service.CreateUser(r.Context(), &user)
	if err != nil {
		if errors.Is(err, ErrRequiredUserName) {
			h.writeError(w, http.StatusBadRequest, "invalidValue", err.Error())
			return
		}
		if strings.Contains(err.Error(), "already exists") {
			h.writeError(w, http.StatusConflict, "uniqueness", err.Error())
			return
		}
		h.log.ErrorContext(r.Context(), "SCIM create user failed", "error", err)
		h.writeError(w, http.StatusInternalServerError, "internalError", "Failed to create user")
		return
	}

	h.writeResourceETag(w, createdUser.Meta)
	h.writeJSON(w, http.StatusCreated, createdUser)
}

// UpdateUser handles PUT /Users/{id}.
func (h *Handler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !h.ensureUserIfMatch(w, r, id) {
		return
	}

	var user SCIMUser
	if err := h.decodeJSONBody(w, r, &user); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalidSyntax", "Invalid JSON")
		return
	}

	updatedUser, err := h.service.UpdateUser(r.Context(), id, &user)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			h.writeError(w, http.StatusNotFound, "notFound", "User not found")
			return
		}
		h.log.ErrorContext(r.Context(), "SCIM update user failed", "user_id", id, "error", err)
		h.writeError(w, http.StatusInternalServerError, "internalError", "Failed to update user")
		return
	}

	h.writeResourceETag(w, updatedUser.Meta)
	h.writeJSON(w, http.StatusOK, updatedUser)
}

// PatchUser handles PATCH /Users/{id}.
func (h *Handler) PatchUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !h.ensureUserIfMatch(w, r, id) {
		return
	}

	var patchReq PatchRequest
	if err := h.decodeJSONBody(w, r, &patchReq); err != nil {
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
		h.log.ErrorContext(r.Context(), "SCIM patch user failed", "user_id", id, "error", err)
		h.writeError(w, http.StatusInternalServerError, "internalError", "Failed to patch user")
		return
	}

	h.writeResourceETag(w, user.Meta)
	h.writeJSON(w, http.StatusOK, user)
}

// DeleteUser handles DELETE /Users/{id}.
func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !h.ensureUserIfMatch(w, r, id) {
		return
	}

	err := h.service.DeleteUser(r.Context(), id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			h.writeError(w, http.StatusNotFound, "notFound", "User not found")
			return
		}
		h.log.ErrorContext(r.Context(), "SCIM delete user failed", "user_id", id, "error", err)
		h.writeError(w, http.StatusInternalServerError, "internalError", "Failed to delete user")
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

	startIndex, count := h.parsePagination(r)

	response, err := h.service.ListGroups(r.Context(), filter, sortBy, sortOrder, startIndex, count)
	if err != nil {
		h.log.ErrorContext(r.Context(), "SCIM list groups failed", "error", err)
		h.writeError(w, http.StatusInternalServerError, "internalError", "Failed to list groups")
		return
	}

	h.writeJSON(w, http.StatusOK, response)
}

// GetGroup handles GET /Groups/{id}.
func (h *Handler) GetGroup(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	group, err := h.service.GetGroup(r.Context(), id)
	if err != nil {
		h.log.WarnContext(r.Context(), "SCIM get group failed", "group_id", id, "error", err)
		h.writeError(w, http.StatusNotFound, "notFound", "Group not found")
		return
	}

	h.writeResourceETag(w, group.Meta)
	h.writeJSON(w, http.StatusOK, group)
}

// CreateGroup handles POST /Groups.
func (h *Handler) CreateGroup(w http.ResponseWriter, r *http.Request) {
	var group SCIMGroup
	if err := h.decodeJSONBody(w, r, &group); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalidSyntax", "Invalid JSON")
		return
	}

	createdGroup, err := h.service.CreateGroup(r.Context(), &group)
	if err != nil {
		if errors.Is(err, ErrRequiredDisplayName) {
			h.writeError(w, http.StatusBadRequest, "invalidValue", err.Error())
			return
		}
		if strings.Contains(err.Error(), "already exists") {
			h.writeError(w, http.StatusConflict, "uniqueness", err.Error())
			return
		}
		h.log.ErrorContext(r.Context(), "SCIM create group failed", "error", err)
		h.writeError(w, http.StatusInternalServerError, "internalError", "Failed to create group")
		return
	}

	h.writeResourceETag(w, createdGroup.Meta)
	h.writeJSON(w, http.StatusCreated, createdGroup)
}

// UpdateGroup handles PUT /Groups/{id}.
func (h *Handler) UpdateGroup(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !h.ensureGroupIfMatch(w, r, id) {
		return
	}

	var group SCIMGroup
	if err := h.decodeJSONBody(w, r, &group); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalidSyntax", "Invalid JSON")
		return
	}

	updatedGroup, err := h.service.UpdateGroup(r.Context(), id, &group)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			h.writeError(w, http.StatusNotFound, "notFound", "Group not found")
			return
		}
		h.log.ErrorContext(r.Context(), "SCIM update group failed", "group_id", id, "error", err)
		h.writeError(w, http.StatusInternalServerError, "internalError", "Failed to update group")
		return
	}

	h.writeResourceETag(w, updatedGroup.Meta)
	h.writeJSON(w, http.StatusOK, updatedGroup)
}

// PatchGroup handles PATCH /Groups/{id}.
func (h *Handler) PatchGroup(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !h.ensureGroupIfMatch(w, r, id) {
		return
	}

	var patchReq PatchRequest
	if err := h.decodeJSONBody(w, r, &patchReq); err != nil {
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
		h.log.ErrorContext(r.Context(), "SCIM patch group failed", "group_id", id, "error", err)
		h.writeError(w, http.StatusInternalServerError, "internalError", "Failed to patch group")
		return
	}

	h.writeResourceETag(w, group.Meta)
	h.writeJSON(w, http.StatusOK, group)
}

// DeleteGroup handles DELETE /Groups/{id}.
func (h *Handler) DeleteGroup(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !h.ensureGroupIfMatch(w, r, id) {
		return
	}

	err := h.service.DeleteGroup(r.Context(), id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			h.writeError(w, http.StatusNotFound, "notFound", "Group not found")
			return
		}
		h.log.ErrorContext(r.Context(), "SCIM delete group failed", "group_id", id, "error", err)
		h.writeError(w, http.StatusInternalServerError, "internalError", "Failed to delete group")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Utility methods

// writeJSON writes a JSON response.
func (h *Handler) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/scim+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func (h *Handler) decodeJSONBody(w http.ResponseWriter, r *http.Request, dst interface{}) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxSCIMJSONBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return errors.New("request body must contain a single JSON object")
	}
	return nil
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
	_ = json.NewEncoder(w).Encode(errorResp)
}

func (h *Handler) parsePagination(r *http.Request) (startIndex, count int) {
	startIndex = 1
	if si := r.URL.Query().Get("startIndex"); si != "" {
		if parsed, err := strconv.Atoi(si); err == nil && parsed > 0 {
			startIndex = parsed
		}
	}

	count = h.config.DefaultPageSize
	if c := r.URL.Query().Get("count"); c != "" {
		if parsed, err := strconv.Atoi(c); err == nil && parsed > 0 {
			count = parsed
		}
	}
	if count > h.config.MaxPageSize {
		count = h.config.MaxPageSize
	}

	return startIndex, count
}

func (h *Handler) writeResourceETag(w http.ResponseWriter, meta Meta) {
	if w == nil {
		return
	}
	etag := h.resourceETagValue(meta)
	if etag == "" {
		return
	}
	w.Header().Set("ETag", etag)
}

func (h *Handler) ensureUserIfMatch(w http.ResponseWriter, r *http.Request, id string) bool {
	ifMatch := strings.TrimSpace(r.Header.Get("If-Match"))
	if ifMatch == "" {
		return true
	}
	user, err := h.service.GetUser(r.Context(), id)
	if err != nil {
		h.writeError(w, http.StatusNotFound, "notFound", "User not found")
		return false
	}
	if h.ifMatchSatisfied(user.Meta, ifMatch) {
		return true
	}
	h.writeError(w, http.StatusPreconditionFailed, "preconditionFailed", "If-Match precondition failed")
	return false
}

func (h *Handler) ensureGroupIfMatch(w http.ResponseWriter, r *http.Request, id string) bool {
	ifMatch := strings.TrimSpace(r.Header.Get("If-Match"))
	if ifMatch == "" {
		return true
	}
	group, err := h.service.GetGroup(r.Context(), id)
	if err != nil {
		h.writeError(w, http.StatusNotFound, "notFound", "Group not found")
		return false
	}
	if h.ifMatchSatisfied(group.Meta, ifMatch) {
		return true
	}
	h.writeError(w, http.StatusPreconditionFailed, "preconditionFailed", "If-Match precondition failed")
	return false
}

func (h *Handler) ifMatchSatisfied(meta Meta, ifMatch string) bool {
	ifMatch = strings.TrimSpace(ifMatch)
	if ifMatch == "" || ifMatch == "*" {
		return true
	}
	current := h.resourceETagToken(meta)
	if current == "" {
		return false
	}
	for _, candidate := range strings.Split(ifMatch, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" {
			return true
		}
		if h.normalizeETagToken(candidate) == current {
			return true
		}
	}
	return false
}

func (h *Handler) resourceETagValue(meta Meta) string {
	token := h.resourceETagToken(meta)
	if token == "" {
		return ""
	}
	return `W/"` + token + `"`
}

func (h *Handler) resourceETagToken(meta Meta) string {
	etag := strings.TrimSpace(meta.Version)
	if etag != "" {
		return etag
	}
	if meta.LastModified != nil && !meta.LastModified.IsZero() {
		return strconv.FormatInt(meta.LastModified.UTC().UnixNano(), 10)
	}
	return ""
}

func (h *Handler) normalizeETagToken(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "W/")
	value = strings.TrimPrefix(value, "w/")
	value = strings.TrimSpace(value)
	return strings.Trim(value, `"`)
}
