package rest

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// AuthMiddleware validates JWT bearer tokens
func AuthMiddleware(logger zerolog.Logger) func(http.Handler) http.Handler {
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
				logger.Warn().Err(err).Msg("invalid token")
				http.Error(w, "Invalid token", http.StatusUnauthorized)
				return
			}

			// Add user ID to context
			ctx := context.WithValue(r.Context(), "user_id", userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RateLimitMiddleware enforces rate limiting per user
func RateLimitMiddleware(logger zerolog.Logger, rateLimit int) func(http.Handler) http.Handler {
	limiter := NewRateLimiter(rateLimit)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := r.Context().Value("user_id").(string)
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

// RequestLoggingMiddleware logs incoming requests
func RequestLoggingMiddleware(logger zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			logger.Info().
				Str("method", r.Method).
				Str("path", r.RequestURI).
				Str("remote", r.RemoteAddr).
				Msg("incoming request")

			next.ServeHTTP(w, r)

			logger.Info().
				Str("method", r.Method).
				Str("path", r.RequestURI).
				Int64("duration_ms", time.Since(start).Milliseconds()).
				Msg("request completed")
		})
	}
}

// CORSMiddleware adds CORS headers
func CORSMiddleware(logger zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusOK)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
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

// Token validation (simplified - in production use JWT library)
func validateToken(token string) (string, error) {
	// This is a simplified implementation
	// In production, verify JWT signature and extract claims
	if token == "" {
		return "", fmt.Errorf("empty token")
	}

	// For now, just extract user ID from token (assuming format user_id:rest)
	// In production, parse and verify JWT
	parts := strings.Split(token, ":")
	if len(parts) >= 1 {
		return parts[0], nil
	}

	return "", fmt.Errorf("invalid token format")
}
