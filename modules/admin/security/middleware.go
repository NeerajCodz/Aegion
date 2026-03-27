// Package security provides security middleware for the Admin SPA.
package security

import (
	"context"
	"net/http"
)

// Headers applies comprehensive security headers for the Admin SPA.
func Headers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Content Security Policy - strict policy for admin interface
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self'; "+
				"style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data: https:; "+
				"font-src 'self'; "+
				"connect-src 'self'; "+
				"frame-ancestors 'none'; "+
				"base-uri 'self'; "+
				"form-action 'self'; "+
				"object-src 'none'; "+
				"media-src 'none'; "+
				"worker-src 'none'; "+
				"child-src 'none'; "+
				"frame-src 'none'")

		// Prevent clickjacking
		w.Header().Set("X-Frame-Options", "DENY")

		// Prevent MIME type sniffing
		w.Header().Set("X-Content-Type-Options", "nosniff")

		// Enable XSS filtering
		w.Header().Set("X-XSS-Protection", "1; mode=block")

		// Control referrer information
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// Control browser permissions
		w.Header().Set("Permissions-Policy",
			"geolocation=(), "+
				"microphone=(), "+
				"camera=(), "+
				"magnetometer=(), "+
				"gyroscope=(), "+
				"speaker=(), "+
				"vibrate=(), "+
				"fullscreen=(self), "+
				"payment=()")

		// Force HTTPS in production (add Strict-Transport-Security header)
		// This should be enabled only in production environments
		if isProduction() {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains; preload")
		}

		// Cache control for sensitive admin content
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate, private")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")

		next.ServeHTTP(w, r)
	})
}

// DevHeaders applies relaxed security headers for development mode.
// This allows hot module reload and other development tools to work.
func DevHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Relaxed CSP for development with Vite HMR support
		w.Header().Set("Content-Security-Policy",
			"default-src 'self' 'unsafe-inline'; "+
				"script-src 'self' 'unsafe-eval' 'unsafe-inline'; "+
				"style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data: https: blob:; "+
				"font-src 'self' data:; "+
				"connect-src 'self' ws: wss:; "+
				"frame-ancestors 'none'; "+
				"base-uri 'self'; "+
				"form-action 'self'")

		// Keep other security headers even in development
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// Relaxed permissions policy for development
		w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")

		// Allow caching in development for better performance
		w.Header().Set("Cache-Control", "public, max-age=3600")

		next.ServeHTTP(w, r)
	})
}

// CSRFProtection adds CSRF protection middleware.
func CSRFProtection(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// For non-GET requests, validate CSRF token
		if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" {
			token := r.Header.Get("X-CSRF-Token")
			if token == "" {
				http.Error(w, "CSRF token required", http.StatusForbidden)
				return
			}

			// Validate token against session
			if !validateCSRFToken(r, token) {
				http.Error(w, "Invalid CSRF token", http.StatusForbidden)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

// RequestID adds a unique request ID to each request for audit correlation.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := generateRequestID()
		w.Header().Set("X-Request-ID", requestID)
		
		// Add to context for logging
		ctx := setRequestIDInContext(r.Context(), requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RateLimitAdmin applies rate limiting specifically for admin endpoints.
func RateLimitAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Get identity from context
		identityID := getIdentityFromContext(r.Context())
		if identityID == "" {
			http.Error(w, "Authentication required", http.StatusUnauthorized)
			return
		}

		// Check rate limit for this admin identity
		if !checkAdminRateLimit(identityID, r.RemoteAddr) {
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// SecurityAudit logs security-relevant events for admin actions.
func SecurityAudit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture response for audit logging
		wrapped := &responseWriter{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}

		next.ServeHTTP(wrapped, r)

		// Log security events asynchronously
		go logSecurityEvent(r, wrapped.statusCode)
	})
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

// Helper functions (these would need actual implementations)

// isProduction checks if the application is running in production mode.
func isProduction() bool {
	// This would check environment variables or build tags
	return false // Default to false for safety in development
}

// validateCSRFToken validates a CSRF token against the session.
func validateCSRFToken(r *http.Request, token string) bool {
	// This would implement actual CSRF token validation
	// Compare against token stored in session or signed token
	return true // Placeholder
}

// generateRequestID generates a unique request ID.
func generateRequestID() string {
	// This would generate a unique ID (UUID, random string, etc.)
	return "req-" + generateRandomString(16)
}

// generateRandomString generates a random string of given length.
func generateRandomString(length int) string {
	// This would implement actual random string generation
	return "placeholder"
}

// setRequestIDInContext stores request ID in context.
func setRequestIDInContext(ctx context.Context, requestID string) context.Context {
	// This would use context.WithValue to store the request ID
	return ctx
}

// getIdentityFromContext retrieves identity from context.
func getIdentityFromContext(ctx context.Context) string {
	// This would extract identity from context set by auth middleware
	return ""
}

// checkAdminRateLimit checks if an admin has exceeded rate limits.
func checkAdminRateLimit(identityID, remoteAddr string) bool {
	// This would implement rate limiting logic
	// Could use Redis, in-memory cache, or database
	return true // Placeholder - allow all requests
}

// logSecurityEvent logs security-relevant events.
func logSecurityEvent(r *http.Request, statusCode int) {
	// This would log to audit system
	// Include: timestamp, IP, user agent, endpoint, response code, etc.
}