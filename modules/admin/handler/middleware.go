// Package handler provides HTTP middleware for the admin module.
package handler

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/aegion/aegion/modules/admin/store"
)

// Context keys for admin data.
type contextKey string

const (
	contextKeyOperator  contextKey = "aegion.admin.operator"
	contextKeyIPAddress contextKey = "aegion.admin.ip_address"
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

// RequireAdmin middleware validates that the request is from an authenticated operator.
func (h *Handler) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		// Get identity ID from session context headers
		identityIDStr := r.Header.Get("X-Aegion-Session-Identity-ID")
		if identityIDStr == "" {
			// Try Authorization header for API key auth
			auth := r.Header.Get("Authorization")
			if strings.HasPrefix(auth, "Bearer "+h.config.APIKeyPrefix) {
				h.handleAPIKeyAuth(w, r, next, auth)
				return
			}

			h.log.WarnContext(r.Context(), "admin auth missing credentials",
				"path", r.URL.Path,
				"method", r.Method,
				"duration_ms", time.Since(start).Milliseconds(),
			)

			writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
			return
		}

		identityID, err := uuid.Parse(identityIDStr)
		if err != nil {
			h.log.WarnContext(r.Context(), "admin auth invalid identity header",
				"path", r.URL.Path,
				"method", r.Method,
				"error", err.Error(),
			)
			writeError(w, http.StatusUnauthorized, "invalid_session", "Invalid session identity")
			return
		}

		// Check if identity is an operator
		operator, err := h.service.GetOperatorByIdentityID(r.Context(), identityID)
		if err != nil {
			h.log.WarnContext(r.Context(), "admin auth identity is not operator",
				"identity_id", identityID.String(),
				"path", r.URL.Path,
				"method", r.Method,
			)
			writeError(w, http.StatusForbidden, "not_operator", "Access denied. Operator status required.")
			return
		}

		// Store operator and IP in context
		ctx := context.WithValue(r.Context(), contextKeyOperator, operator)
		ctx = context.WithValue(ctx, contextKeyIPAddress, getClientIP(r))

		h.log.InfoContext(ctx, "admin auth success",
			"operator_id", operator.ID.String(),
			"identity_id", operator.IdentityID.String(),
			"path", r.URL.Path,
			"method", r.Method,
			"duration_ms", time.Since(start).Milliseconds(),
		)

		next.ServeHTTP(w, r.WithContext(ctx))
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
		h.log.WarnContext(r.Context(), "admin api key not found",
			"prefix", keyPrefix,
			"path", r.URL.Path,
		)
		writeError(w, http.StatusUnauthorized, "invalid_api_key", "Invalid or expired API key")
		return
	}

	if !store.ValidateAPIKeyToken(apiKey, key.KeyHash) {
		h.log.WarnContext(r.Context(), "admin api key hash mismatch",
			"key_id", key.ID.String(),
			"operator_id", key.OperatorID.String(),
		)
		writeError(w, http.StatusUnauthorized, "invalid_api_key", "Invalid or expired API key")
		return
	}

	// Check expiration
	if key.ExpiresAt != nil && time.Now().UTC().After(*key.ExpiresAt) {
		h.log.WarnContext(r.Context(), "admin api key expired",
			"key_id", key.ID.String(),
			"expires_at", key.ExpiresAt.Format(time.RFC3339),
		)
		writeError(w, http.StatusUnauthorized, "api_key_expired", "API key has expired")
		return
	}

	// Get operator for the API key
	operator, err := h.service.Store().GetOperator(r.Context(), key.OperatorID)
	if err != nil {
		h.log.WarnContext(r.Context(), "admin api key operator missing",
			"key_id", key.ID.String(),
			"operator_id", key.OperatorID.String(),
		)
		writeError(w, http.StatusUnauthorized, "invalid_api_key", "API key operator not found")
		return
	}

	// Update last used timestamp (best effort)
	_ = h.service.Store().UpdateAPIKeyLastUsed(r.Context(), key.ID)

	// Store operator and IP in context
	ctx := context.WithValue(r.Context(), contextKeyOperator, operator)
	ctx = context.WithValue(ctx, contextKeyIPAddress, getClientIP(r))
	ctx = context.WithValue(ctx, contextKey("aegion.admin.auth_method"), "api_key")
	ctx = context.WithValue(ctx, contextKey("aegion.admin.auth_key_id"), key.ID.String())

	h.log.InfoContext(ctx, "admin api key auth success",
		"operator_id", operator.ID.String(),
		"key_id", key.ID.String(),
		"path", r.URL.Path,
		"method", r.Method,
	)

	next.ServeHTTP(w, r.WithContext(ctx))
}

// RequirePermission returns middleware that checks for a specific permission.
func RequirePermission(h *Handler, permission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			operator := OperatorFromContext(r.Context())
			if operator == nil {
				writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
				return
			}

			// Check if operator has the required permission
			if err := h.service.EvaluateCapability(r.Context(), operator.ID, permission); err != nil {
				writeError(w, http.StatusForbidden, "insufficient_permissions",
					"You do not have permission to perform this action")
				return
			}

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

			// Log the action after response
			go h.logAdminAction(r, action, resourceType, wrapped.statusCode)
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

	if h.log != nil {
		h.log.InfoContext(r.Context(), "admin action audited",
			"action", action,
			"resource_type", resourceType,
			"resource_id", resourceID,
			"status_code", statusCode,
			"operator_id", fmtUUID(operator),
		)
	}
}

func fmtUUID(op *store.Operator) string {
	if op == nil {
		return ""
	}
	return op.ID.String()
}
