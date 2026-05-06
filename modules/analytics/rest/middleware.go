package rest

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/aegion/aegion/internal/platform/jwt"
	"github.com/aegion/aegion/internal/platform/logger"
)

type contextKey string

const userIDContextKey contextKey = "user_id"

func withUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDContextKey, userID)
}

func userIDFromContext(ctx context.Context) (string, bool) {
	userID, ok := ctx.Value(userIDContextKey).(string)
	return userID, ok && userID != ""
}

// AuthMiddleware validates JWT bearer tokens
func AuthMiddleware(log *logger.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract token from Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			parts := strings.Split(authHeader, " ")
			if len(parts) != 2 || parts[0] != "Bearer" {
				http.Error(w, "Invalid authorization header", http.StatusUnauthorized)
				return
			}

			token := parts[1]

			// Validate token (in production, verify JWT signature)
			userID, err := validateToken(token)
			if err != nil {
				log.Warn("invalid token", "error", err)
				http.Error(w, "Invalid token", http.StatusUnauthorized)
				return
			}

			// Add user ID to context
			ctx := withUserID(r.Context(), userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RateLimitMiddleware enforces rate limiting per user
func RateLimitMiddleware(log *logger.Logger, rateLimit int) func(http.Handler) http.Handler {
	limiter := NewRateLimiter(rateLimit)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := userIDFromContext(r.Context())
			if !ok {
				userID = "anonymous"
			}

			if !limiter.Allow(userID) {
				w.Header().Set("Retry-After", "60")
				http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// QueryTimeoutMiddleware enforces query timeout
func QueryTimeoutMiddleware(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequestLoggingMiddleware logs incoming requests using wide events pattern
func RequestLoggingMiddleware(log *logger.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ctx := r.Context()

			next.ServeHTTP(w, r)

			// Wide event: single log with all context
			log.InfoContext(ctx, "request completed",
				"http.method", r.Method,
				"http.path", r.RequestURI,
				"http.remote_addr", r.RemoteAddr,
				"latency_ms", time.Since(start).Milliseconds(),
			)
		})
	}
}

// CORSMiddleware adds CORS headers (deprecated - use RestrictedCORSMiddleware instead)
func CORSMiddleware(log *logger.Logger) func(http.Handler) http.Handler {
	return RestrictedCORSMiddleware(log, []string{})
}

// RateLimiter implements per-user rate limiting
type RateLimiter struct {
	mu        sync.RWMutex
	limits    map[string]*RateLimitEntry
	rateLimit int
}

// RateLimitEntry represents a rate limit entry
type RateLimitEntry struct {
	count     int
	resetTime time.Time
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(rateLimit int) *RateLimiter {
	limiter := &RateLimiter{
		limits:    make(map[string]*RateLimitEntry),
		rateLimit: rateLimit,
	}

	// Start cleanup goroutine
	go limiter.cleanup()

	return limiter
}

// Allow checks if a request is allowed
func (rl *RateLimiter) Allow(userID string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	entry, exists := rl.limits[userID]
	if !exists || now.After(entry.resetTime) {
		// Reset limit
		rl.limits[userID] = &RateLimitEntry{
			count:     1,
			resetTime: now.Add(time.Minute),
		}
		return true
	}

	if entry.count < rl.rateLimit {
		entry.count++
		return true
	}

	return false
}

// cleanup periodically cleans up expired limits
func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()

		for userID, limit := range rl.limits {
			if now.After(limit.resetTime) {
				delete(rl.limits, userID)
			}
		}

		rl.mu.Unlock()
	}
}

// Token validation with expiration check
func validateToken(token string) (string, error) {
	if token == "" {
		return "", fmt.Errorf("empty token")
	}

	publicKeyB64 := strings.TrimSpace(os.Getenv("AEGION_ANALYTICS_REST_JWT_PUBLIC_KEY_BASE64"))
	if publicKeyB64 == "" {
		return "", fmt.Errorf("jwt verification key is not configured")
	}

	publicKey, err := base64.StdEncoding.DecodeString(publicKeyB64)
	if err != nil {
		return "", fmt.Errorf("invalid jwt verification key: %w", err)
	}

	verified, err := jwt.Verify(token, publicKey, "ES256", jwt.VerifyOptions{})
	if err != nil {
		return "", fmt.Errorf("token verification failed: %w", err)
	}

	userID := strings.TrimSpace(verified.Claims.Subject)
	if userID == "" {
		return "", fmt.Errorf("invalid token: missing sub claim")
	}

	return userID, nil
}
