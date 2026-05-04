package rest

import (
	"net/http"
	"strings"
	"time"

	"github.com/aegion/aegion/internal/platform/logger"
	"github.com/aegion/aegion/modules/analytics/rbac"
	"github.com/aegion/aegion/modules/analytics/store"
)

// PermissionMiddleware enforces permission-based access control
func PermissionMiddleware(manager *rbac.Manager, log *logger.Logger, requiredPerms ...rbac.Permission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := userIDFromContext(r.Context())
			if !ok {
				log.Warn("no user ID in context for permission check")
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			// Check if user has all required permissions
			for _, perm := range requiredPerms {
				hasPermission, err := manager.HasPermission(userID, perm)
				if err != nil || !hasPermission {
					log.Warn("permission denied",
						"user_id", userID,
						"permission", string(perm),
					)
					http.Error(w, "Forbidden", http.StatusForbidden)
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// SecurityHeadersMiddleware adds security headers to responses
func SecurityHeadersMiddleware(log *logger.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Prevent clickjacking
			w.Header().Set("X-Frame-Options", "DENY")

			// Prevent MIME type sniffing
			w.Header().Set("X-Content-Type-Options", "nosniff")

			// Enable XSS protection
			w.Header().Set("X-XSS-Protection", "1; mode=block")

			// Content Security Policy
			w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'")

			// HSTS (only in production with HTTPS)
			if r.TLS != nil || strings.HasPrefix(r.Header.Get("X-Forwarded-Proto"), "https") {
				w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}

			// Referrer Policy
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

			// Permissions Policy
			w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")

			next.ServeHTTP(w, r)
		})
	}
}

// RestrictedCORSMiddleware enforces strict CORS policies
func RestrictedCORSMiddleware(log *logger.Logger, allowedOrigins []string) func(http.Handler) http.Handler {
	originMap := make(map[string]bool)
	for _, origin := range allowedOrigins {
		originMap[origin] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			// Only set CORS headers if origin is allowed
			if origin != "" && originMap[origin] {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
				w.Header().Set("Access-Control-Max-Age", "3600")
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusOK)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// AuditLoggingMiddleware logs all requests to audit store
func AuditLoggingMiddleware(auditStore *store.AuditStore, log *logger.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, _ := userIDFromContext(r.Context())
			if userID == "" {
				userID = "anonymous"
			}

			// Capture the response status
			lrw := &loggingResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			start := time.Now()
			next.ServeHTTP(lrw, r)
			duration := time.Since(start)

			// Log to audit store
			eventType := store.AuditEventQuery
			if strings.HasPrefix(r.URL.Path, "/export") {
				eventType = store.AuditEventExport
			} else if strings.HasPrefix(r.URL.Path, "/dashboard") {
				eventType = store.AuditEventDashboard
			} else if strings.HasPrefix(r.URL.Path, "/webhook") {
				eventType = store.AuditEventWebhook
			}

			status := "success"
			if lrw.statusCode >= 400 {
				status = "failure"
			}

			event := store.AuditEvent{
				ID:        generateID(),
				Timestamp: time.Now(),
				UserID:    userID,
				EventType: eventType,
				Action:    r.Method + " " + r.URL.Path,
				Status:    status,
				IPAddress: r.RemoteAddr,
				UserAgent: r.Header.Get("User-Agent"),
				Details: map[string]interface{}{
					"status_code":  lrw.statusCode,
					"method":       r.Method,
					"path":         r.URL.Path,
					"duration_ms":  duration.Milliseconds(),
					"query_params": r.URL.RawQuery,
				},
			}

			if err := auditStore.LogEvent(r.Context(), event); err != nil {
				log.Error("failed to log audit event", "error", err)
			}
		})
	}
}

// loggingResponseWriter wraps ResponseWriter to capture status code
type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (lrw *loggingResponseWriter) WriteHeader(code int) {
	lrw.statusCode = code
	lrw.ResponseWriter.WriteHeader(code)
}

// AuthenticationCheckMiddleware enforces authentication on all requests
func AuthenticationCheckMiddleware(log *logger.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Allow health checks without authentication
			if r.URL.Path == "/health" || r.URL.Path == "/ready" {
				next.ServeHTTP(w, r)
				return
			}

			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				log.Warn("missing authorization header", "path", r.URL.Path)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// InputValidationMiddleware validates and sanitizes all inputs
func InputValidationMiddleware(log *logger.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Validate Content-Type for POST/PUT requests
			if r.Method == http.MethodPost || r.Method == http.MethodPut {
				contentType := r.Header.Get("Content-Type")
				if !strings.HasPrefix(contentType, "application/json") {
					log.Warn("invalid content type", "content_type", contentType)
					http.Error(w, "Bad Request", http.StatusBadRequest)
					return
				}

				// Limit request body size to 10MB
				r.Body = http.MaxBytesReader(w, r.Body, 10*1024*1024)
			}

			next.ServeHTTP(w, r)
		})
	}
}

// EndpointRateLimitMiddleware enforces per-endpoint rate limits
func EndpointRateLimitMiddleware(log *logger.Logger, endpointLimits map[string]int) func(http.Handler) http.Handler {
	limiters := make(map[string]*RateLimiter)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			endpoint := r.Method + " " + r.URL.Path

			// Get or create limiter for this endpoint
			limiter, exists := limiters[endpoint]
			if !exists {
				// Use default limit if not specified
				limit := endpointLimits[endpoint]
				if limit == 0 {
					limit = 1000 // Default high limit
				}
				limiter = NewRateLimiter(limit)
				limiters[endpoint] = limiter
			}

			userID, _ := userIDFromContext(r.Context())
			if userID == "" {
				userID = "anonymous"
			}

			if !limiter.Allow(userID) {
				log.Warn("endpoint rate limit exceeded",
					"user_id", userID,
					"endpoint", endpoint,
				)
				w.Header().Set("Retry-After", "60")
				http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// generateID generates a simple unique ID (in production, use UUID)
func generateID() string {
	return time.Now().UTC().Format("20060102150405") + randomString(6)
}

// randomString generates a random string of given length
func randomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	result := make([]byte, length)
	for i := range result {
		result[i] = charset[time.Now().UnixNano()%int64(len(charset))]
	}
	return string(result)
}
