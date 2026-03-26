// Package handler provides HTTP handlers for the admin module.
package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/aegion/aegion/modules/admin/service"
)

// Handler handles admin HTTP requests.
type Handler struct {
	service *service.Service
}

// New creates a new admin handler.
func New(svc *service.Service) *Handler {
	return &Handler{service: svc}
}

// RegisterRoutes registers all admin API routes.
func (h *Handler) RegisterRoutes(r chi.Router) {
	// Apply admin authentication middleware to all routes
	r.Use(h.RequireAdmin)

	// Identity management
	r.Route("/identities", func(r chi.Router) {
		r.With(RequirePermission(h, service.PermIdentitiesRead)).Get("/", h.ListIdentities)
		r.With(RequirePermission(h, service.PermIdentitiesRead)).Post("/search", h.SearchIdentities)
		r.With(RequirePermission(h, service.PermIdentitiesRead)).Get("/{id}", h.GetIdentity)
		r.With(RequirePermission(h, service.PermIdentitiesUpdate)).Patch("/{id}", h.UpdateIdentity)
		r.With(RequirePermission(h, service.PermIdentitiesDelete)).Delete("/{id}", h.DeleteIdentity)

		// Session management for identity
		r.Route("/{id}/sessions", func(r chi.Router) {
			r.With(RequirePermission(h, service.PermSessionsRead)).Get("/", h.ListIdentitySessions)
			r.With(RequirePermission(h, service.PermSessionsDelete)).Delete("/", h.RevokeAllIdentitySessions)
		})
	})

	// Session management
	r.Route("/sessions", func(r chi.Router) {
		r.With(RequirePermission(h, service.PermSessionsDelete)).Delete("/{session_id}", h.RevokeSession)
	})

	// Operator management
	r.Route("/operators", func(r chi.Router) {
		r.With(RequirePermission(h, service.PermOperatorsRead)).Get("/", h.ListOperators)
		r.With(RequirePermission(h, service.PermOperatorsCreate)).Post("/", h.CreateOperator)
		r.With(RequirePermission(h, service.PermOperatorsRead)).Get("/{id}", h.GetOperator)
		r.With(RequirePermission(h, service.PermOperatorsUpdate)).Patch("/{id}", h.UpdateOperator)
		r.With(RequirePermission(h, service.PermOperatorsDelete)).Delete("/{id}", h.DeleteOperator)
	})

	// Audit logs
	r.Route("/audit", func(r chi.Router) {
		r.With(RequirePermission(h, service.PermAuditRead)).Get("/", h.ListAuditLogs)
	})

	// Roles
	r.Route("/roles", func(r chi.Router) {
		r.With(RequirePermission(h, service.PermRolesRead)).Get("/", h.ListRoles)
		r.With(RequirePermission(h, service.PermRolesRead)).Get("/{name}", h.GetRole)
	})
}

// ErrorResponse is the standard error response format.
type ErrorResponse struct {
	Error struct {
		Code    int    `json:"code"`
		Status  string `json:"status"`
		Message string `json:"message"`
	} `json:"error"`
}

// PaginationMeta contains pagination metadata.
type PaginationMeta struct {
	Page    int   `json:"page"`
	PerPage int   `json:"per_page"`
	Total   int64 `json:"total"`
	Pages   int   `json:"pages"`
}

// ListResponse is a generic list response with pagination.
type ListResponse struct {
	Items      interface{}    `json:"items"`
	Pagination PaginationMeta `json:"pagination"`
}

// writeJSON writes a JSON response.
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// writeError writes an error response.
func writeError(w http.ResponseWriter, status int, code, message string) {
	resp := ErrorResponse{}
	resp.Error.Code = status
	resp.Error.Status = code
	resp.Error.Message = message
	writeJSON(w, status, resp)
}

// parsePagination extracts pagination parameters from request.
func parsePagination(r *http.Request) (page, perPage, offset int) {
	page = 1
	perPage = 20

	if p := r.URL.Query().Get("page"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
			page = parsed
		}
	}

	if pp := r.URL.Query().Get("per_page"); pp != "" {
		if parsed, err := strconv.Atoi(pp); err == nil && parsed > 0 && parsed <= 100 {
			perPage = parsed
		}
	}

	offset = (page - 1) * perPage
	return page, perPage, offset
}

// buildPaginationMeta creates pagination metadata.
func buildPaginationMeta(page, perPage int, total int64) PaginationMeta {
	pages := int(total) / perPage
	if int(total)%perPage > 0 {
		pages++
	}

	return PaginationMeta{
		Page:    page,
		PerPage: perPage,
		Total:   total,
		Pages:   pages,
	}
}

// getClientIP extracts the client IP address from request.
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP in the chain
		if idx := len(xff); idx > 0 {
			for i := 0; i < len(xff); i++ {
				if xff[i] == ',' {
					return xff[:i]
				}
			}
			return xff
		}
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	return r.RemoteAddr
}
