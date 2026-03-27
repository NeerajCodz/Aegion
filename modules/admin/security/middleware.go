// Package security provides security middleware for the Admin SPA.
package security

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

const (
	csrfHeaderName = "X-CSRF-Token"
	csrfCookieName = "aegion_admin_csrf"

	defaultRateLimitRPS   = 5.0
	defaultRateLimitBurst = 20
)

type contextKey string

const contextKeyRequestID contextKey = "aegion.admin.request_id"

// Headers applies comprehensive security headers for the Admin SPA.
func Headers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Content Security Policy - strict policy for admin interface
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self'; "+
				"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; "+
				"img-src 'self' data: https:; "+
				"font-src 'self' https://fonts.gstatic.com; "+
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
				"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; "+
				"img-src 'self' data: https: blob:; "+
				"font-src 'self' data: https://fonts.gstatic.com; "+
				"connect-src 'self' ws: wss: http: https:; "+
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
		if isSafeMethod(r.Method) {
			token, err := ensureCSRFCookie(w, r)
			if err != nil {
				http.Error(w, "Failed to initialize CSRF token", http.StatusInternalServerError)
				return
			}
			w.Header().Set(csrfHeaderName, token)
			next.ServeHTTP(w, r)
			return
		}

		if isAPIKeyAuth(r) {
			next.ServeHTTP(w, r)
			return
		}

		token := r.Header.Get(csrfHeaderName)
		if token == "" {
			http.Error(w, "CSRF token required", http.StatusForbidden)
			return
		}

		if !validateCSRFToken(r, token) {
			http.Error(w, "Invalid CSRF token", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// RequestID adds a unique request ID to each request for audit correlation.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = generateRequestID()
		}
		w.Header().Set("X-Request-ID", requestID)

		// Add to context for logging
		ctx := setRequestIDInContext(r.Context(), requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RateLimitAdmin applies rate limiting specifically for admin endpoints.
func RateLimitAdmin(next http.Handler) http.Handler {
	rps, burst := loadRateLimitConfig()
	limiter := newRateLimiter(rps, burst)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := rateLimitKey(r)
		if !limiter.allow(key) {
			w.Header().Set("Retry-After", "1")
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
	env := strings.ToLower(strings.TrimSpace(os.Getenv("AEGION_ENV")))
	if env == "" {
		env = strings.ToLower(strings.TrimSpace(os.Getenv("AEGION_ENVIRONMENT")))
	}

	return env == "prod" || env == "production"
}

// validateCSRFToken validates a CSRF token against the request cookie.
func validateCSRFToken(r *http.Request, token string) bool {
	cookie, err := r.Cookie(csrfCookieName)
	if err != nil || cookie.Value == "" {
		return false
	}

	if len(cookie.Value) != len(token) {
		return false
	}

	return subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(token)) == 1
}

// generateRequestID generates a unique request ID.
func generateRequestID() string {
	return uuid.NewString()
}

// setRequestIDInContext stores request ID in context.
func setRequestIDInContext(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, contextKeyRequestID, requestID)
}

// logSecurityEvent logs security-relevant events.
func logSecurityEvent(r *http.Request, statusCode int) {
	event := log.Info()
	if statusCode >= http.StatusBadRequest {
		event = log.Warn()
	}

	event.
		Str("request_id", r.Header.Get("X-Request-ID")).
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Str("ip", getClientIP(r)).
		Str("user_agent", r.UserAgent()).
		Int("status", statusCode).
		Msg("admin security event")
}

func ensureCSRFCookie(w http.ResponseWriter, r *http.Request) (string, error) {
	if cookie, err := r.Cookie(csrfCookieName); err == nil && cookie.Value != "" {
		return cookie.Value, nil
	}

	token, err := generateCSRFToken()
	if err != nil {
		return "", err
	}

	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
		Secure:   isProduction(),
	})

	return token, nil
}

func generateCSRFToken() (string, error) {
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(token), nil
}

func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

func isAPIKeyAuth(r *http.Request) bool {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	return strings.HasPrefix(auth, "Bearer aegion_")
}

type tokenBucket struct {
	capacity float64
	tokens   float64
	refill   float64
	lastFill time.Time
	mu       sync.Mutex
}

func newTokenBucket(capacity, refill float64) *tokenBucket {
	return &tokenBucket{
		capacity: capacity,
		tokens:   capacity,
		refill:   refill,
		lastFill: time.Now(),
	}
}

func (b *tokenBucket) take() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(b.lastFill).Seconds()
	if elapsed > 0 {
		b.tokens = minFloat(b.capacity, b.tokens+(elapsed*b.refill))
		b.lastFill = now
	}

	if b.tokens < 1 {
		return false
	}

	b.tokens -= 1
	return true
}

type rateLimiter struct {
	rps     float64
	burst   int
	buckets map[string]*tokenBucket
	mu      sync.RWMutex
}

func newRateLimiter(rps float64, burst int) *rateLimiter {
	limiter := &rateLimiter{
		rps:     rps,
		burst:   burst,
		buckets: make(map[string]*tokenBucket),
	}

	go limiter.cleanupLoop()

	return limiter
}

func (r *rateLimiter) allow(key string) bool {
	r.mu.RLock()
	bucket, ok := r.buckets[key]
	r.mu.RUnlock()

	if !ok {
		r.mu.Lock()
		bucket, ok = r.buckets[key]
		if !ok {
			bucket = newTokenBucket(float64(r.burst), r.rps)
			r.buckets[key] = bucket
		}
		r.mu.Unlock()
	}

	return bucket.take()
}

func (r *rateLimiter) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		r.mu.Lock()
		for key, bucket := range r.buckets {
			bucket.mu.Lock()
			idle := now.Sub(bucket.lastFill)
			bucket.mu.Unlock()
			if idle > 10*time.Minute {
				delete(r.buckets, key)
			}
		}
		r.mu.Unlock()
	}
}

func rateLimitKey(r *http.Request) string {
	if identityID := strings.TrimSpace(r.Header.Get("X-Aegion-Session-Identity-ID")); identityID != "" {
		return "id:" + identityID
	}

	return "ip:" + getClientIP(r)
}

func getClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}

	if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
		return xri
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}

	return r.RemoteAddr
}

func loadRateLimitConfig() (float64, int) {
	rps := defaultRateLimitRPS
	if value := strings.TrimSpace(os.Getenv("AEGION_ADMIN_RATE_LIMIT_RPS")); value != "" {
		if parsed, err := strconv.ParseFloat(value, 64); err == nil && parsed > 0 {
			rps = parsed
		}
	}

	burst := defaultRateLimitBurst
	if value := strings.TrimSpace(os.Getenv("AEGION_ADMIN_RATE_LIMIT_BURST")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			burst = parsed
		}
	}

	return rps, burst
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
