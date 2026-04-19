// Package handler provides HTTP handlers for the admin module.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/aegion/aegion/internal/platform/trustedproxy"
	"github.com/aegion/aegion/modules/admin/service"
	"github.com/aegion/aegion/modules/admin/store"
	socialservice "github.com/aegion/aegion/modules/social/service"
	socialstore "github.com/aegion/aegion/modules/social/store"
)

const maxJSONBodyBytes int64 = 1 << 20

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
	GetEffectivePermissions(ctx context.Context, operatorID uuid.UUID) ([]string, error)
	ListOperators(ctx context.Context, actorID uuid.UUID, limit, offset int) ([]*store.Operator, int64, error)
	GetOperator(ctx context.Context, actorID uuid.UUID, operatorID uuid.UUID) (*store.Operator, error)
	CreateOperator(ctx context.Context, actorID uuid.UUID, identityID uuid.UUID, role string, permissions map[string]interface{}, ipAddress string) (*store.Operator, error)
	UpdateOperator(ctx context.Context, actorID uuid.UUID, operatorID uuid.UUID, role string, permissions map[string]interface{}, ipAddress string) (*store.Operator, error)
	DeleteOperator(ctx context.Context, actorID uuid.UUID, operatorID uuid.UUID, ipAddress string) error
	ListRoles(ctx context.Context, actorID uuid.UUID, limit, offset int) ([]*store.Role, int64, error)
	GetRole(ctx context.Context, actorID uuid.UUID, name string) (*store.Role, error)
	CreateRole(ctx context.Context, actorID uuid.UUID, name, description string, permissions []string, ipAddress string) (*store.Role, error)
	UpdateRole(ctx context.Context, actorID uuid.UUID, name string, description *string, permissions []string, ipAddress string) (*store.Role, error)
	DeleteRole(ctx context.Context, actorID uuid.UUID, name string, ipAddress string) error
	AvailablePermissions() []string
	ListAuditLogs(ctx context.Context, actorID uuid.UUID, filter store.AuditFilter, limit, offset int) ([]*store.AuditLogEntry, int64, error)
}

type SocialProviderManager interface {
	ListConfiguredProviders(ctx context.Context, includeDisabled bool) ([]socialstore.Provider, error)
	GetProvider(ctx context.Context, slug string) (*socialstore.Provider, error)
	UpsertProvider(ctx context.Context, req socialservice.ProviderUpsertRequest) (*socialstore.Provider, error)
	DeleteProvider(ctx context.Context, slug string) error
}

// HandlerConfig holds handler configuration.
type HandlerConfig struct {
	SessionTokenExpiry time.Duration // Session token expiry (default: 8 hours)
	DefaultPageSize    int           // Default pagination page size (default: 20)
	MaxPageSize        int           // Maximum pagination page size (default: 100)
	APIKeyPrefix       string        // API key token prefix (default: aegion_)
	APIKeyPrefixLen    int           // Lookup prefix chars after token prefix (default: 12)
	APIKeyEntropyBytes int           // Random token entropy bytes (default: 32)
	Logger             *slog.Logger  // Structured logger (default: slog.Default)
	SocialProviders    SocialProviderManager
}

// DefaultHandlerConfig returns default handler configuration.
func DefaultHandlerConfig() HandlerConfig {
	return HandlerConfig{
		SessionTokenExpiry: 8 * time.Hour,
		DefaultPageSize:    20,
		MaxPageSize:        100,
		APIKeyPrefix:       "aegion_",
		APIKeyPrefixLen:    12,
		APIKeyEntropyBytes: 32,
	}
}

// OperatorView is the API representation used by auth and operator endpoints.
type OperatorView struct {
	ID                   string                 `json:"id"`
	Email                string                 `json:"email"`
	Name                 string                 `json:"name"`
	Role                 string                 `json:"role"`
	Status               string                 `json:"status"`
	Permissions          map[string]interface{} `json:"permissions,omitempty"`
	EffectivePermissions []string               `json:"effective_permissions,omitempty"`
	CreatedAt            string                 `json:"created_at"`
	UpdatedAt            string                 `json:"updated_at"`
	LastLoginAt          *string                `json:"last_login_at,omitempty"`
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
	service         Service
	db              dbQuerier
	config          HandlerConfig
	log             *slog.Logger
	socialProviders SocialProviderManager
}

// New creates a new admin handler.
func New(svc Service, cfgOverride ...HandlerConfig) *Handler {
	cfg := DefaultHandlerConfig()
	if len(cfgOverride) > 0 {
		cfg = cfgOverride[0]
	}

	if cfg.DefaultPageSize == 0 {
		cfg.DefaultPageSize = 20
	}
	if cfg.MaxPageSize == 0 {
		cfg.MaxPageSize = 100
	}
	if cfg.SessionTokenExpiry == 0 {
		cfg.SessionTokenExpiry = 8 * time.Hour
	}
	if cfg.APIKeyPrefix == "" {
		cfg.APIKeyPrefix = "aegion_"
	}
	if cfg.APIKeyPrefixLen == 0 {
		cfg.APIKeyPrefixLen = 12
	}
	if cfg.APIKeyEntropyBytes == 0 {
		cfg.APIKeyEntropyBytes = 32
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{
		service:         svc,
		config:          cfg,
		log:             logger.With("component", "admin.handler"),
		socialProviders: cfg.SocialProviders,
	}
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
			r.With(RequirePermission(h, service.PermIdentitiesUpdate)).Post("/{id}/suspend", h.SuspendIdentity)
			r.With(RequirePermission(h, service.PermIdentitiesUpdate)).Post("/{id}/activate", h.ActivateIdentity)
			r.With(RequirePermission(h, service.PermIdentitiesUpdate)).Post("/{id}/reset-mfa", h.ResetIdentityMFA)

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
			r.With(RequirePermission(h, service.PermRolesRead)).Get("/permissions", h.ListPermissions)
			r.With(RequirePermission(h, service.PermRolesCreate)).Post("/", h.CreateRole)
			r.With(RequirePermission(h, service.PermRolesRead)).Get("/{name}", h.GetRole)
			r.With(RequirePermission(h, service.PermRolesUpdate)).Patch("/{name}", h.UpdateRole)
			r.With(RequirePermission(h, service.PermRolesDelete)).Delete("/{name}", h.DeleteRole)
		})

		// Dashboard and system settings
		r.With(RequirePermission(h, service.PermAuditRead)).Get("/dashboard/stats", h.DashboardStats)
		r.With(RequirePermission(h, service.PermAuditRead)).Get("/logs/activity", h.ActivityFeed)
		r.With(RequirePermission(h, service.PermSecurityRead)).Get("/security/ip-bans", h.ListIPBans)
		r.With(RequirePermission(h, service.PermSecurityCreate)).Post("/security/ip-bans", h.UpsertIPBan)
		r.With(RequirePermission(h, service.PermSecurityDelete)).Delete("/security/ip-bans/{id}", h.DeleteIPBan)
		r.With(RequirePermission(h, service.PermConfigRead)).Get("/setup/status", h.SetupStatus)
		r.With(RequirePermission(h, service.PermRolesRead)).Get("/rbac/summary", h.RBACSummary)
		r.With(RequirePermission(h, service.PermConfigRead)).Get("/integrations/overview", h.IntegrationOverview)
		r.With(RequirePermission(h, service.PermConfigRead)).Get("/integrations/social/presets", h.ListSocialPresets)
		r.With(RequirePermission(h, service.PermConfigRead)).Get("/integrations/social/providers", h.ListSocialProviders)
		r.With(RequirePermission(h, service.PermConfigUpdate)).Post("/integrations/social/providers", h.UpsertSocialProvider)
		r.With(RequirePermission(h, service.PermConfigRead)).Get("/integrations/social/providers/{slug}", h.GetSocialProvider)
		r.With(RequirePermission(h, service.PermConfigUpdate)).Delete("/integrations/social/providers/{slug}", h.DeleteSocialProvider)
		r.With(RequirePermission(h, service.PermConfigRead)).Get("/integrations/sso/connections", h.ListSSOConnections)
		r.With(RequirePermission(h, service.PermConfigUpdate)).Post("/integrations/sso/connections", h.UpsertSSOConnection)
		r.With(RequirePermission(h, service.PermConfigUpdate)).Delete("/integrations/sso/connections/{slug}", h.DeleteSSOConnection)
		r.With(RequirePermission(h, service.PermConfigRead)).Get("/integrations/proxy/upstreams", h.ListProxyUpstreams)
		r.With(RequirePermission(h, service.PermConfigUpdate)).Post("/integrations/proxy/upstreams", h.UpsertProxyUpstream)
		r.With(RequirePermission(h, service.PermConfigUpdate)).Delete("/integrations/proxy/upstreams/{name}", h.DeleteProxyUpstream)
		r.With(RequirePermission(h, service.PermConfigRead)).Get("/integrations/proxy/routes", h.ListProxyRoutes)
		r.With(RequirePermission(h, service.PermConfigUpdate)).Post("/integrations/proxy/routes", h.UpsertProxyRoute)
		r.With(RequirePermission(h, service.PermConfigUpdate)).Delete("/integrations/proxy/routes/{id}", h.DeleteProxyRoute)
		r.With(RequirePermission(h, service.PermConfigRead)).Post("/integrations/proxy/simulate", h.SimulateProxyRoute)
		r.With(RequirePermission(h, service.PermConfigRead)).Get("/policy/abac-rules", h.ListPolicyABACRules)
		r.With(RequirePermission(h, service.PermConfigUpdate)).Post("/policy/abac-rules", h.UpsertPolicyABACRule)
		r.With(RequirePermission(h, service.PermConfigUpdate)).Delete("/policy/abac-rules/{id}", h.DeletePolicyABACRule)
		r.With(RequirePermission(h, service.PermConfigRead)).Get("/policy/rebac-tuples", h.ListPolicyReBACTuples)
		r.With(RequirePermission(h, service.PermConfigUpdate)).Post("/policy/rebac-tuples", h.UpsertPolicyReBACTuple)
		r.With(RequirePermission(h, service.PermConfigUpdate)).Delete("/policy/rebac-tuples/{id}", h.DeletePolicyReBACTuple)
		r.With(RequirePermission(h, service.PermConfigRead)).Get("/policy/rebac-namespaces", h.ListPolicyReBACNamespaces)
		r.With(RequirePermission(h, service.PermConfigUpdate)).Post("/policy/rebac-namespaces", h.UpsertPolicyReBACNamespace)
		r.With(RequirePermission(h, service.PermConfigUpdate)).Delete("/policy/rebac-namespaces/{id}", h.DeletePolicyReBACNamespace)
		r.With(RequirePermission(h, service.PermConfigRead)).Post("/policy/simulate", h.SimulatePolicyDecision)
		r.With(RequirePermission(h, service.PermOAuth2ClientsRead)).Get("/oauth2/clients", h.ListOAuth2Clients)
		r.With(RequirePermission(h, service.PermOAuth2ClientsManage)).Post("/oauth2/clients", h.CreateOAuth2Client)
		r.With(RequirePermission(h, service.PermOAuth2ClientsRead)).Get("/oauth2/clients/{id}", h.GetOAuth2Client)
		r.With(RequirePermission(h, service.PermOAuth2ClientsManage)).Patch("/oauth2/clients/{id}", h.UpdateOAuth2Client)
		r.With(RequirePermission(h, service.PermOAuth2ClientsManage)).Delete("/oauth2/clients/{id}", h.DeleteOAuth2Client)
		r.With(RequirePermission(h, service.PermOAuth2ClientsManage)).Post("/oauth2/clients/{id}/rotate-secret", h.RotateOAuth2ClientSecret)
		r.With(RequirePermission(h, service.PermOAuth2TokensRead)).Get("/oauth2/tokens", h.ListOAuth2Tokens)
		r.With(RequirePermission(h, service.PermOAuth2TokensRevoke)).Post("/oauth2/tokens/revoke", h.RevokeOAuth2Token)
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

func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst interface{}) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var extra struct{}
	if err := dec.Decode(&extra); err != io.EOF {
		return errors.New("invalid request body")
	}
	return nil
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
	return parsePaginationWithConfig(r, h.config)
}

// parsePagination retains backward compatibility for tests and helper callers.
func parsePagination(r *http.Request) (page, perPage, offset int) {
	return parsePaginationWithConfig(r, DefaultHandlerConfig())
}

func parsePaginationWithConfig(r *http.Request, cfg HandlerConfig) (page, perPage, offset int) {
	page = 1
	perPage = cfg.DefaultPageSize

	if p := r.URL.Query().Get("page"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
			page = parsed
		}
	}

	if pp := r.URL.Query().Get("per_page"); pp != "" {
		if parsed, err := strconv.Atoi(pp); err == nil && parsed > 0 && parsed <= cfg.MaxPageSize {
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
	return trustedproxy.ClientIP(r, allowAdminForwardedHeaders(), "AEGION_ADMIN_TRUSTED_PROXY_CIDRS")
}

func allowAdminForwardedHeaders() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("AEGION_ADMIN_TRUST_FORWARDED_HEADERS"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
