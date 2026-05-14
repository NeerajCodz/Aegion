package router

import (
	"context"
	"fmt"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/aegion/aegion/internal/platform/observability"
	"github.com/aegion/aegion/internal/platform/trustedproxy"
	"github.com/aegion/aegion/internal/xlog"
	"github.com/google/uuid"
)

// Context keys for middleware data.
type contextKey string

const (
	contextKeyRequestID    contextKey = "aegion.request_id"
	contextKeyRequestTime  contextKey = "aegion.request_time"
	coreTrustedProxyEnvVar contextKey = "AEGION_TRUSTED_PROXY_CIDRS"
)

// RequestID middleware generates or extracts a unique request ID.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = uuid.New().String()
		}

		// Set header for downstream
		w.Header().Set("X-Request-ID", requestID)

		// Store in context
		ctx := context.WithValue(r.Context(), contextKeyRequestID, requestID)
		ctx = observability.WithRequestIDForLogger(ctx, requestID)
		ctx = context.WithValue(ctx, contextKeyRequestTime, time.Now())
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetRequestID extracts the request ID from context.
func GetRequestID(ctx context.Context) string {
	if id, ok := ctx.Value(contextKeyRequestID).(string); ok {
		return id
	}
	return ""
}

// GetRequestTime extracts the request start time from context.
func GetRequestTime(ctx context.Context) time.Time {
	if t, ok := ctx.Value(contextKeyRequestTime).(time.Time); ok {
		return t
	}
	return time.Time{}
}

// responseRecorder wraps ResponseWriter to capture status code.
type responseRecorder struct {
	http.ResponseWriter
	statusCode int
	written    int64
}

func newResponseRecorder(w http.ResponseWriter) *responseRecorder {
	return &responseRecorder{ResponseWriter: w, statusCode: http.StatusOK}
}

func (rr *responseRecorder) WriteHeader(code int) {
	rr.statusCode = code
	rr.ResponseWriter.WriteHeader(code)
}

func (rr *responseRecorder) Write(b []byte) (int, error) {
	n, err := rr.ResponseWriter.Write(b)
	rr.written += int64(n)
	return n, err
}

// Logger middleware provides structured request logging.
func Logger(log *xlog.Logger) func(http.Handler) http.Handler {
	return LoggerWithTrustProxy(log, false)
}

// LoggerWithTrustProxy controls whether forwarded client-IP headers are trusted.
func LoggerWithTrustProxy(log *xlog.Logger, trustProxy bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := GetRequestID(r.Context())
			start := time.Now()
			ctx := r.Context()

			rr := newResponseRecorder(w)
			next.ServeHTTP(rr, r)

			duration := time.Since(start)
			route := observability.HTTPRouteLabel(observability.RoutePattern(r), r.URL.Path)

			// Build wide event attributes
			attrs := []any{
				"http.method", r.Method,
				"http.route", route,
				"http.path", r.URL.Path,
				"http.remote_addr", getClientIPWithTrust(r, trustProxy),
				"http.status", rr.statusCode,
				"http.bytes", rr.written,
				"latency_ms", duration.Milliseconds(),
				"http.user_agent", r.UserAgent(),
				"request_id", requestID,
				"outcome", "success",
			}

			// Log level based on status code
			switch {
			case rr.statusCode >= 500:
				log.ErrorContext(ctx, "request completed", append(attrs, "outcome", "error")...)
			case rr.statusCode >= 400:
				log.WarnContext(ctx, "request completed", append(attrs, "outcome", "warning")...)
			default:
				log.InfoContext(ctx, "request completed", attrs...)
			}
		})
	}
}

// Recoverer middleware recovers from panics and logs them.
func Recoverer(log *xlog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					requestID := GetRequestID(r.Context())
					stack := string(debug.Stack())
					route := observability.HTTPRouteLabel(observability.RoutePattern(r), r.URL.Path)
					ctx := r.Context()

					log.ErrorContext(ctx, "panic recovered",
						"method", r.Method,
						"route", route,
						"path", r.URL.Path,
						"panic", rec,
						"stack", stack,
						"request_id", requestID,
					)

					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// CORS middleware handles Cross-Origin Resource Sharing.
func CORS(cfg CORSConfig) func(http.Handler) http.Handler {
	allowedOriginsSet := make(map[string]bool)
	allowAll := false
	for _, origin := range cfg.AllowedOrigins {
		if origin == "*" {
			allowAll = true
		}
		allowedOriginsSet[origin] = true
	}

	allowedMethodsStr := strings.Join(cfg.AllowedMethods, ", ")
	allowedHeadersStr := strings.Join(cfg.AllowedHeaders, ", ")
	exposedHeadersStr := strings.Join(cfg.ExposedHeaders, ", ")

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := strings.TrimSpace(r.Header.Get("Origin"))
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			allowOrigin := ""
			switch {
			case allowedOriginsSet[origin]:
				allowOrigin = origin
			case allowAll && !cfg.AllowCredentials:
				allowOrigin = "*"
			}

			if allowOrigin == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Set CORS headers
			if allowOrigin != "*" {
				appendVaryHeader(w.Header(), "Origin")
			}
			w.Header().Set("Access-Control-Allow-Origin", allowOrigin)
			if cfg.AllowCredentials && allowOrigin != "*" {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
			if exposedHeadersStr != "" {
				w.Header().Set("Access-Control-Expose-Headers", exposedHeadersStr)
			}

			// Handle preflight
			if r.Method == http.MethodOptions {
				w.Header().Set("Access-Control-Allow-Methods", allowedMethodsStr)
				w.Header().Set("Access-Control-Allow-Headers", allowedHeadersStr)
				if cfg.MaxAge > 0 {
					w.Header().Set("Access-Control-Max-Age", strings.TrimSpace(timeToSeconds(cfg.MaxAge)))
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// Token bucket for rate limiting
type tokenBucket struct {
	tokens         float64
	maxTokens      float64
	refillRate     float64
	lastRefillTime time.Time
	mu             sync.Mutex
}

func newTokenBucket(maxTokens float64, refillRate float64) *tokenBucket {
	return &tokenBucket{
		tokens:         maxTokens,
		maxTokens:      maxTokens,
		refillRate:     refillRate,
		lastRefillTime: time.Now(),
	}
}

func (tb *tokenBucket) take() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(tb.lastRefillTime).Seconds()
	tb.tokens += elapsed * tb.refillRate
	if tb.tokens > tb.maxTokens {
		tb.tokens = tb.maxTokens
	}
	tb.lastRefillTime = now

	if tb.tokens >= 1 {
		tb.tokens--
		return true
	}
	return false
}

// RateLimit middleware implements token bucket rate limiting per IP.
func RateLimit(cfg RateLimitConfig) func(http.Handler) http.Handler {
	return RateLimitWithTrustProxy(cfg, false)
}

// RateLimitWithTrustProxy controls whether forwarded client-IP headers are trusted.
func RateLimitWithTrustProxy(cfg RateLimitConfig, trustProxy bool) func(http.Handler) http.Handler {
	buckets := make(map[string]*tokenBucket)
	var mu sync.RWMutex

	// Cleanup old buckets periodically
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			mu.Lock()
			now := time.Now()
			for ip, bucket := range buckets {
				bucket.mu.Lock()
				if now.Sub(bucket.lastRefillTime) > 10*time.Minute {
					delete(buckets, ip)
				}
				bucket.mu.Unlock()
			}
			mu.Unlock()
		}
	}()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := getClientIPWithTrust(r, trustProxy)

			mu.RLock()
			bucket, exists := buckets[ip]
			mu.RUnlock()

			if !exists {
				mu.Lock()
				bucket, exists = buckets[ip]
				if !exists {
					bucket = newTokenBucket(float64(cfg.Burst), cfg.RequestsPerSecond)
					buckets[ip] = bucket
				}
				mu.Unlock()
			}

			if !bucket.take() {
				w.Header().Set("Retry-After", "1")
				http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// SecurityHeaders middleware adds security-related headers.
func SecurityHeaders(devMode bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Prevent MIME type sniffing
			w.Header().Set("X-Content-Type-Options", "nosniff")

			// Prevent clickjacking
			w.Header().Set("X-Frame-Options", "DENY")

			// XSS protection (legacy, but still useful)
			w.Header().Set("X-XSS-Protection", "1; mode=block")

			// Referrer policy
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

			// Permissions policy (formerly Feature-Policy)
			w.Header().Set("Permissions-Policy", "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()")

			if !devMode {
				// HSTS - only in production
				w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains; preload")

				// Content Security Policy
				w.Header().Set("Content-Security-Policy", buildCSP())
			}

			next.ServeHTTP(w, r)
		})
	}
}

// buildCSP builds a Content Security Policy header value.
func buildCSP() string {
	directives := []string{
		"default-src 'self'",
		"script-src 'self'",
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data: https:",
		"font-src 'self'",
		"connect-src 'self'",
		"frame-ancestors 'none'",
		"base-uri 'self'",
		"form-action 'self'",
	}
	return strings.Join(directives, "; ")
}

// Timeout middleware wraps requests with a timeout.
func Timeout(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Helper functions

func getClientIP(r *http.Request) string {
	return getClientIPWithTrust(r, true)
}

func getClientIPWithTrust(r *http.Request, trustProxy bool) string {
	return trustedproxy.ClientIP(r, trustProxy, string(coreTrustedProxyEnvVar))
}

func timeToSeconds(seconds int) string {
	return fmt.Sprintf("%d", seconds)
}

func appendVaryHeader(header http.Header, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	for _, existing := range header.Values("Vary") {
		for _, part := range strings.Split(existing, ",") {
			if strings.EqualFold(strings.TrimSpace(part), value) {
				return
			}
		}
	}
	header.Add("Vary", value)
}
