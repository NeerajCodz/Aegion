// Package handler provides HTTP middleware for the admin module.
package handler

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	aegionloza "github.com/aegion/aegion/internal/platform/loza"
	"github.com/aegion/aegion/internal/platform/observability"
	"github.com/aegion/aegion/modules/admin/store"
	lozasdk "github.com/astraive/loza/sdks/go"
)

// Context keys for admin data.
type contextKey string

const (
	contextKeyOperator   contextKey = "aegion.admin.operator"
	contextKeyIPAddress  contextKey = "aegion.admin.ip_address"
	contextKeyAuthMethod contextKey = "aegion.admin.auth_method"
	contextKeyAuthKeyID  contextKey = "aegion.admin.auth_key_id"
)

// OperatorFromContext retrieves the operator from request context.
func OperatorFromContext(ctx context.Context) *store.Operator {
	if op, ok := ctx.Value(contextKeyOperator).(*store.Operator); ok {
		return op
	}
	return nil
}

// IPAddressFromContext retrieves the client IP from request context.
func IPAddressFromContext(ctx context.Context) string {
	if ip, ok := ctx.Value(contextKeyIPAddress).(string); ok {
		return ip
	}

	return ""
}
func emitAdminSecurityEvent(ctx context.Context, name, outcome string, err error, attrs ...lozasdk.Attr) {
	logger := lozasdk.Default()
	eventCtx := aegionloza.Start(ctx, logger, lozasdk.Params{
		Event:   name,
		Kind:    "security",
		Service: "aegion.module.admin",
		Custom:  attrs,
	})
	if err != nil {
		_ = logger.FinishError(eventCtx, err)
	} else {
		_ = logger.Finish(eventCtx, aegionloza.NormalizeOutcome(outcome))
	}
	_ = logger.Emit(eventCtx)
}

// RequireAdmin middleware validates that the request is from an authenticated operator.
func (h *Handler) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := strings.TrimSpace(r.Header.Get("Authorization"))
		if strings.HasPrefix(auth, "Bearer "+h.config.APIKeyPrefix) {
			h.handleAPIKeyAuth(w, r, next, auth)
			return
		}
		emitAdminSecurityEvent(r.Context(), "admin.login", "rejected", nil,
			lozasdk.String("auth.operation", "api_key"),
			lozasdk.String("policy.decision", "deny"),
			lozasdk.String("http.method", r.Method),
			lozasdk.String("http.path", r.URL.Path))
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
	})
}

// handleAPIKeyAuth handles authentication via admin API keys.
func (h *Handler) handleAPIKeyAuth(w http.ResponseWriter, r *http.Request, next http.Handler, auth string) {
	// Extract API key from Authorization header
	apiKey := strings.TrimPrefix(auth, "Bearer ")
	if apiKey == "" || !strings.HasPrefix(apiKey, h.config.APIKeyPrefix) {
		writeError(w, http.StatusUnauthorized, "invalid_api_key", "Invalid API key format")
		return
	}

	// Extract prefix for lookup (configured chars after token prefix)
	minLen := len(h.config.APIKeyPrefix) + h.config.APIKeyPrefixLen
	if len(apiKey) < minLen {
		writeError(w, http.StatusUnauthorized, "invalid_api_key", "Invalid API key")
		return
	}
	keyPrefix := apiKey[len(h.config.APIKeyPrefix):minLen]

	// Look up API key
	key, err := h.service.Store().GetAPIKeyByPrefix(r.Context(), keyPrefix)
	if err != nil {
		emitAdminSecurityEvent(r.Context(), "admin.login", "rejected", err,
			lozasdk.String("auth.operation", "api_key"),
			lozasdk.String("policy.decision", "deny"),
			lozasdk.String("http.path", r.URL.Path))
		writeError(w, http.StatusUnauthorized, "invalid_api_key", "Invalid or expired API key")
		return
	}

	if !store.ValidateAPIKeyToken(apiKey, key.KeyHash) {
		emitAdminSecurityEvent(r.Context(), "admin.login", "rejected", nil,
			lozasdk.String("auth.operation", "api_key"),
			lozasdk.String("policy.decision", "deny"))
		writeError(w, http.StatusUnauthorized, "invalid_api_key", "Invalid or expired API key")
		return
	}

	// Check expiration
	if key.ExpiresAt != nil && time.Now().UTC().After(*key.ExpiresAt) {
		emitAdminSecurityEvent(r.Context(), "admin.login", "rejected", nil,
			lozasdk.String("auth.operation", "api_key"),
			lozasdk.String("policy.decision", "deny"),
			lozasdk.String("error.code", "api_key_expired"))
		writeError(w, http.StatusUnauthorized, "api_key_expired", "API key has expired")
		return
	}

	// Get operator for the API key
	operator, err := h.service.Store().GetOperator(r.Context(), key.OperatorID)
	if err != nil {
		emitAdminSecurityEvent(r.Context(), "admin.login", "rejected", err,
			lozasdk.String("auth.operation", "api_key"),
			lozasdk.String("policy.decision", "deny"))
		writeError(w, http.StatusUnauthorized, "invalid_api_key", "API key operator not found")
		return
	}

	// Update last used timestamp (best effort)
	_ = h.service.Store().UpdateAPIKeyLastUsed(r.Context(), key.ID)

	// Store operator and IP in context
	ctx := context.WithValue(r.Context(), contextKeyOperator, operator)
	ctx = context.WithValue(ctx, contextKeyIPAddress, getClientIP(r))
	ctx = context.WithValue(ctx, contextKeyAuthMethod, "api_key")
	ctx = context.WithValue(ctx, contextKeyAuthKeyID, key.ID.String())
	ctx = observability.WithUserID(ctx, operator.ID.String())

	emitAdminSecurityEvent(ctx, "admin.login", "success", nil,
		lozasdk.String("auth.operation", "api_key"),
		lozasdk.String("auth.subject_id", operator.ID.String()),
		lozasdk.String("auth.key_id", key.ID.String()),
		lozasdk.String("http.path", r.URL.Path),
		lozasdk.String("http.method", r.Method))

	next.ServeHTTP(w, r.WithContext(ctx))
}

// RequirePermission returns middleware that checks for a specific permission.
func RequirePermission(h *Handler, permission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			operator := OperatorFromContext(r.Context())
			if operator == nil {
				emitAdminSecurityEvent(r.Context(), "admin.permission_decision", "rejected", nil,
					lozasdk.String("policy.decision", "deny"),
					lozasdk.String("policy.reason", "missing_operator"),
					lozasdk.String("auth.operation", permission))
				writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
				return
			}

			if err := h.service.EvaluateCapability(r.Context(), operator.ID, permission); err != nil {
				emitAdminSecurityEvent(r.Context(), "admin.permission_decision", "rejected", err,
					lozasdk.String("policy.decision", "deny"),
					lozasdk.String("policy.reason", "capability_denied"),
					lozasdk.String("auth.operation", permission))
				writeError(w, http.StatusForbidden, "insufficient_permissions",
					"You do not have permission to perform this action")
				return
			}

			emitAdminSecurityEvent(r.Context(), "admin.permission_decision", "success", nil,
				lozasdk.String("policy.decision", "allow"),
				lozasdk.String("auth.operation", permission))
			next.ServeHTTP(w, r)
		})
	}
}

// AuditLog middleware logs admin actions.
func (h *Handler) AuditLog(action, resourceType string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Capture the response for logging
			wrapped := &responseWriter{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
			}

			next.ServeHTTP(wrapped, r)

			h.logAdminAction(r, action, resourceType, wrapped.statusCode)
		})
	}
}

// responseWriter wraps http.ResponseWriter to capture status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// logAdminAction logs an admin action to the audit log.
func (h *Handler) logAdminAction(r *http.Request, action, resourceType string, statusCode int) {
	ctx := context.Background()
	operator := OperatorFromContext(r.Context())
	ipAddress := IPAddressFromContext(r.Context())

	// Determine resource ID from URL
	resourceID := ""
	if r.URL != nil {
		resourceID = r.URL.Path
	}

	// Build details
	details := map[string]interface{}{
		"method":      r.Method,
		"path":        r.URL.Path,
		"status_code": statusCode,
		"user_agent":  r.UserAgent(),
	}
	if requestID := middleware.GetReqID(r.Context()); requestID != "" {
		details["request_id"] = requestID
	}

	// Create audit entry
	entry := &store.AuditLogEntry{
		ID:           uuid.New(),
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Details:      details,
		IPAddress:    ipAddress,
		CreatedAt:    time.Now().UTC(),
	}

	if operator != nil {
		entry.OperatorID = &operator.ID
	}

	// Log action (best effort)
	_ = h.service.Store().LogAction(ctx, entry)

	eventCtx := aegionloza.Start(r.Context(), lozasdk.Default(), lozasdk.Params{
		Event:      "admin.audit",
		Kind:       "audit",
		Service:    "aegion.module.admin",
		RequestID:  middleware.GetReqID(r.Context()),
		StatusCode: statusCode,
		Custom: []lozasdk.Attr{
			lozasdk.String("audit.action", action),
			lozasdk.String("audit.resource_type", resourceType),
			lozasdk.String("audit.resource_id", resourceID),
			lozasdk.String("http.method", r.Method),
			lozasdk.String("http.path", r.URL.Path),
		},
	})
	_ = lozasdk.Default().Finish(eventCtx, aegionloza.OutcomeForHTTP(statusCode, nil))
	_ = lozasdk.Default().Emit(eventCtx)
}

func fmtUUID(op *store.Operator) string {
	if op == nil {
		return ""
	}
	return op.ID.String()
}
