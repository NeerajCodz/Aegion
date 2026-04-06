// Package handler provides HTTP handlers for the admin module.
package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/aegion/aegion/modules/admin/service"
	"github.com/aegion/aegion/modules/admin/store"
)

type dbQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Service defines the admin service behavior needed by handlers.
type Service interface {
	Store() service.Store
	EvaluateCapability(ctx context.Context, operatorID uuid.UUID, permission string) error
	GetOperatorByIdentityID(ctx context.Context, identityID uuid.UUID) (*store.Operator, error)
	ListOperators(ctx context.Context, actorID uuid.UUID, limit, offset int) ([]*store.Operator, int64, error)
	GetOperator(ctx context.Context, actorID uuid.UUID, operatorID uuid.UUID) (*store.Operator, error)
	CreateOperator(ctx context.Context, actorID uuid.UUID, identityID uuid.UUID, role string, permissions map[string]interface{}, ipAddress string) (*store.Operator, error)
	UpdateOperator(ctx context.Context, actorID uuid.UUID, operatorID uuid.UUID, role string, permissions map[string]interface{}, ipAddress string) (*store.Operator, error)
	DeleteOperator(ctx context.Context, actorID uuid.UUID, operatorID uuid.UUID, ipAddress string) error
	ListRoles(ctx context.Context, actorID uuid.UUID, limit, offset int) ([]*store.Role, int64, error)
	GetRole(ctx context.Context, actorID uuid.UUID, name string) (*store.Role, error)
	ListAuditLogs(ctx context.Context, actorID uuid.UUID, filter store.AuditFilter, limit, offset int) ([]*store.AuditLogEntry, int64, error)
}

// HandlerConfig holds handler configuration.
type HandlerConfig struct {
	SessionTokenExpiry time.Duration // Session token expiry (default: 8 hours)
	DefaultPageSize    int           // Default pagination page size (default: 20)
	MaxPageSize        int           // Maximum pagination page size (default: 100)
}

// DefaultHandlerConfig returns default handler configuration.
func DefaultHandlerConfig() HandlerConfig {
	return HandlerConfig{
		SessionTokenExpiry: 8 * time.Hour,
		DefaultPageSize:    20,
		MaxPageSize:        100,
	}
}

const sessionTokenExpiry = 8 * time.Hour

// OperatorView is the API representation used by auth and operator endpoints.
type OperatorView struct {
	ID          string                 `json:"id"`
	Email       string                 `json:"email"`
	Name        string                 `json:"name"`
	Role        string                 `json:"role"`
	Status      string                 `json:"status"`
	Permissions map[string]interface{} `json:"permissions,omitempty"`
	CreatedAt   string                 `json:"created_at"`
	UpdatedAt   string                 `json:"updated_at"`
	LastLoginAt *string                `json:"last_login_at,omitempty"`
}

// DashboardStatsResponse powers the admin dashboard widgets.
type DashboardStatsResponse struct {
	TotalIdentities   int64   `json:"total_identities"`
	ActiveSessions    int64   `json:"active_sessions"`
	IdentitiesLast24h int64   `json:"identities_last_24h"`
	MFAAdoptionRate   float64 `json:"mfa_adoption_rate"`
}

// SystemSettingsResponse is the persisted settings contract for admin UI.
type SystemSettingsResponse struct {
	SessionLifetimeHours   int      `json:"session_lifetime_hours"`
	MFARequired            bool     `json:"mfa_required"`
	PasswordMinLength      int      `json:"password_min_length"`
	MaxLoginAttempts       int      `json:"max_login_attempts"`
	LockoutDurationMinutes int      `json:"lockout_duration_minutes"`
	AllowedDomains         []string `json:"allowed_domains,omitempty"`
}

// Handler handles admin HTTP requests.
type Handler struct {
	service Service
	db      dbQuerier
	config  HandlerConfig
}

// New creates a new admin handler.
func New(svc Service, cfg HandlerConfig) *Handler {
	if cfg.DefaultPageSize == 0 {
		cfg.DefaultPageSize = 20
	}
	if cfg.MaxPageSize == 0 {
		cfg.MaxPageSize = 100
	}
	if cfg.SessionTokenExpiry == 0 {
		cfg.SessionTokenExpiry = 8 * time.Hour
	}
	return &Handler{service: svc, config: cfg}
}

func (h *Handler) dbConn() dbQuerier {
	if h.db != nil {
		return h.db
	}
	return h.service.Store().DB()
}

// RegisterRoutes registers all admin API routes.
func (h *Handler) RegisterRoutes(r chi.Router) {
	// Public auth endpoints
	r.Post("/auth/login", h.Login)

	// Protected admin routes
	r.Group(func(r chi.Router) {
		r.Use(h.RequireAdmin)

		// Auth/session metadata
		r.Post("/auth/logout", h.Logout)
		r.Get("/auth/me", h.Me)

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
			r.With(RequirePermission(h, service.PermSessionsRead)).Get("/", h.ListSessions)
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

		// Dashboard and system settings
		r.With(RequirePermission(h, service.PermAuditRead)).Get("/dashboard/stats", h.DashboardStats)
		r.With(RequirePermission(h, service.PermConfigRead)).Get("/settings", h.GetSettings)
		r.With(RequirePermission(h, service.PermConfigUpdate)).Patch("/settings", h.UpdateSettings)
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
	_ = json.NewEncoder(w).Encode(data)
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
func (h *Handler) parsePagination(r *http.Request) (page, perPage, offset int) {
	page = 1
	perPage = h.config.DefaultPageSize

	if p := r.URL.Query().Get("page"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
			page = parsed
		}
	}

	if pp := r.URL.Query().Get("per_page"); pp != "" {
		if parsed, err := strconv.Atoi(pp); err == nil && parsed > 0 && parsed <= h.config.MaxPageSize {
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
