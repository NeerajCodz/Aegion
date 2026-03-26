// Package handler provides HTTP middleware for the admin module.
package handler

import (
	"context"
	"net/http"
	"strings"
	"time"

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
		// Get identity ID from session context headers
		identityIDStr := r.Header.Get("X-Aegion-Session-Identity-ID")
		if identityIDStr == "" {
			// Try Authorization header for API key auth
			auth := r.Header.Get("Authorization")
			if strings.HasPrefix(auth, "Bearer aegion_") {
				h.handleAPIKeyAuth(w, r, next, auth)
				return
			}

			writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
			return
		}

		identityID, err := uuid.Parse(identityIDStr)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid_session", "Invalid session identity")
			return
		}

		// Check if identity is an operator
		operator, err := h.service.GetOperatorByIdentityID(r.Context(), identityID)
		if err != nil {
			writeError(w, http.StatusForbidden, "not_operator", "Access denied. Operator status required.")
			return
		}

		// Store operator and IP in context
		ctx := context.WithValue(r.Context(), contextKeyOperator, operator)
		ctx = context.WithValue(ctx, contextKeyIPAddress, getClientIP(r))

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// handleAPIKeyAuth handles authentication via admin API keys.
func (h *Handler) handleAPIKeyAuth(w http.ResponseWriter, r *http.Request, next http.Handler, auth string) {
	// Extract API key from Authorization header
	apiKey := strings.TrimPrefix(auth, "Bearer ")
	if apiKey == "" || !strings.HasPrefix(apiKey, "aegion_") {
		writeError(w, http.StatusUnauthorized, "invalid_api_key", "Invalid API key format")
		return
	}

	// Extract prefix for lookup (first 12 chars after "aegion_")
	if len(apiKey) < 20 {
		writeError(w, http.StatusUnauthorized, "invalid_api_key", "Invalid API key")
		return
	}
	keyPrefix := apiKey[7:19] // "aegion_" is 7 chars, prefix is next 12

	// Look up API key
	key, err := h.service.Store().GetAPIKeyByPrefix(r.Context(), keyPrefix)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_api_key", "Invalid or expired API key")
		return
	}

	// Check expiration
	if key.ExpiresAt != nil && time.Now().UTC().After(*key.ExpiresAt) {
		writeError(w, http.StatusUnauthorized, "api_key_expired", "API key has expired")
		return
	}

	// Get operator for the API key
	operator, err := h.service.Store().GetOperator(r.Context(), key.OperatorID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_api_key", "API key operator not found")
		return
	}

	// Update last used timestamp (best effort)
	_ = h.service.Store().UpdateAPIKeyLastUsed(r.Context(), key.ID)

	// Store operator and IP in context
	ctx := context.WithValue(r.Context(), contextKeyOperator, operator)
	ctx = context.WithValue(ctx, contextKeyIPAddress, getClientIP(r))

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
}
