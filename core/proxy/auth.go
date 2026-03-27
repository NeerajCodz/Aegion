package proxy

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/rs/zerolog"

	"github.com/aegion/aegion/core/session"
)

// AuthMiddleware provides authentication middleware for the proxy.
type AuthMiddleware struct {
	sessionManager *session.Manager
	logger         zerolog.Logger
	optional       bool // If true, missing sessions are not treated as errors
}

// NewAuthMiddleware creates a new authentication middleware.
func NewAuthMiddleware(sessionManager *session.Manager, logger zerolog.Logger, optional bool) *AuthMiddleware {
	return &AuthMiddleware{
		sessionManager: sessionManager,
		logger:         logger.With().Str("component", "auth-middleware").Logger(),
		optional:       optional,
	}
}

// Middleware returns an HTTP middleware function that handles authentication.
func (am *AuthMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		requestID := getRequestIDFromContext(ctx)
		
		// Attempt to get session from request
		sess, err := am.sessionManager.GetFromRequest(ctx, r)
		if err != nil {
			// Log the authentication failure
			am.logger.Debug().
				Str("request_id", requestID).
				Err(err).
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Msg("authentication failed")

			// If authentication is optional, continue without session
			if am.optional {
				next.ServeHTTP(w, r)
				return
			}

			// Otherwise, return authentication error
			am.handleAuthError(w, r, err)
			return
		}

		// Validate session is still valid and active
		if !sess.Active {
			am.logger.Debug().
				Str("request_id", requestID).
				Str("session_id", sess.ID.String()).
				Msg("session is inactive")
			
			if !am.optional {
				am.handleAuthError(w, r, session.ErrSessionInvalid)
				return
			}
		}

		// Check if session has expired
		if time.Now().UTC().After(sess.ExpiresAt) {
			am.logger.Debug().
				Str("request_id", requestID).
				Str("session_id", sess.ID.String()).
				Time("expires_at", sess.ExpiresAt).
				Msg("session has expired")
			
			if !am.optional {
				am.handleAuthError(w, r, session.ErrSessionExpired)
				return
			}
		}

		// Add session to request context
		ctx = session.WithSession(ctx, sess)
		
		// Log successful authentication
		am.logger.Debug().
			Str("request_id", requestID).
			Str("session_id", sess.ID.String()).
			Str("identity_id", sess.IdentityID.String()).
			Str("aal", string(sess.AAL)).
			Msg("authentication successful")

		// Continue to next handler
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// InjectHeaders adds Aegion-specific headers to the request for downstream services.
func (am *AuthMiddleware) InjectHeaders(r *http.Request, sess *session.Session) {
	if sess == nil {
		return
	}

	// Add session information headers
	r.Header.Set("X-Aegion-Session-ID", sess.ID.String())
	r.Header.Set("X-Aegion-Identity-ID", sess.IdentityID.String())
	r.Header.Set("X-Aegion-AAL", string(sess.AAL))
	r.Header.Set("X-Aegion-Authenticated-At", sess.AuthenticatedAt.Format(time.RFC3339))
	r.Header.Set("X-Aegion-Expires-At", sess.ExpiresAt.Format(time.RFC3339))

	// Add impersonation headers if applicable
	if sess.IsImpersonation && sess.ImpersonatorID != nil {
		r.Header.Set("X-Aegion-Impersonation", "true")
		r.Header.Set("X-Aegion-Impersonator-ID", sess.ImpersonatorID.String())
	}

	// Add authentication methods
	if len(sess.AuthMethods) > 0 {
		methods := make([]string, len(sess.AuthMethods))
		for i, method := range sess.AuthMethods {
			methods[i] = string(method.Method)
		}
		r.Header.Set("X-Aegion-Auth-Methods", joinStrings(methods, ","))
	}

	// Add device information
	if len(sess.Devices) > 0 {
		device := sess.Devices[len(sess.Devices)-1] // Use latest device
		if device.IPAddress != "" {
			r.Header.Set("X-Aegion-Device-IP", device.IPAddress)
		}
		if device.UserAgent != "" {
			r.Header.Set("X-Aegion-Device-UA", device.UserAgent)
		}
	}
}

// handleAuthError handles authentication errors by returning appropriate HTTP responses.
func (am *AuthMiddleware) handleAuthError(w http.ResponseWriter, r *http.Request, err error) {
	requestID := getRequestIDFromContext(r.Context())
	
	var statusCode int
	var message string

	switch err {
	case session.ErrSessionNotFound:
		statusCode = http.StatusUnauthorized
		message = "Authentication required"
	case session.ErrSessionExpired:
		statusCode = http.StatusUnauthorized
		message = "Session expired"
	case session.ErrSessionInvalid:
		statusCode = http.StatusUnauthorized
		message = "Invalid session"
	default:
		statusCode = http.StatusUnauthorized
		message = "Authentication failed"
	}

	// Set appropriate headers
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	
	// For session expired, suggest reauthentication
	if err == session.ErrSessionExpired {
		w.Header().Set("X-Aegion-Action", "reauthenticate")
	}

	w.WriteHeader(statusCode)

	// Write JSON error response
	response := map[string]interface{}{
		"error": map[string]interface{}{
			"code":       statusCode,
			"message":    message,
			"request_id": requestID,
		},
	}

	// Don't fail if we can't encode the JSON response
	if err := writeJSON(w, response); err != nil {
		am.logger.Error().
			Str("request_id", requestID).
			Err(err).
			Msg("failed to write authentication error response")
	}
}

// RequireAAL creates middleware that requires a minimum Authentication Assurance Level.
func RequireAAL(requiredAAL session.AAL) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sess := session.FromContext(r.Context())
			if sess == nil {
				writeErrorResponse(w, http.StatusUnauthorized, "Authentication required")
				return
			}

			if sess.AAL < requiredAAL {
				writeErrorResponse(w, http.StatusForbidden, "Insufficient authentication level")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireCapabilities creates middleware that requires specific capabilities.
func RequireCapabilities(capabilities ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sess := session.FromContext(r.Context())
			if sess == nil {
				writeErrorResponse(w, http.StatusUnauthorized, "Authentication required")
				return
			}

			// Check capabilities (placeholder implementation)
			if err := checkCapabilities(sess, capabilities); err != nil {
				writeErrorResponse(w, http.StatusForbidden, "Insufficient privileges")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// Helper functions

// getRequestIDFromContext extracts the request ID from the context.
func getRequestIDFromContext(ctx context.Context) string {
	if id := ctx.Value("request_id"); id != nil {
		if requestID, ok := id.(string); ok {
			return requestID
		}
	}
	return ""
}

// joinStrings joins a slice of strings with the given separator.
func joinStrings(slice []string, sep string) string {
	if len(slice) == 0 {
		return ""
	}
	if len(slice) == 1 {
		return slice[0]
	}

	result := slice[0]
	for i := 1; i < len(slice); i++ {
		result += sep + slice[i]
	}
	return result
}

// writeJSON writes a JSON response.
func writeJSON(w http.ResponseWriter, v interface{}) error {
	// Simple JSON encoding without external dependencies
	// In a real implementation, you'd use encoding/json
	
	// For now, we'll write a simple error response
	w.Write([]byte(`{"error":{"code":401,"message":"Authentication failed"}}`))
	return nil
}

// writeErrorResponse writes a standard error response.
func writeErrorResponse(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	
	// Simple JSON response
	response := `{"error":{"code":` + strconv.Itoa(statusCode) + `,"message":"` + message + `"}}`
	w.Write([]byte(response))
}