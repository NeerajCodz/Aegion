package graphql

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// Middleware represents a GraphQL middleware function.
type Middleware func(http.Handler) http.Handler

// AuthMiddleware enforces authentication for GraphQL queries.
func AuthMiddleware(logger zerolog.Logger, requiredForFields map[string]bool) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			query, updatedRequest, err := extractGraphQLQuery(r)
			if err != nil {
				logger.Warn().Err(err).Msg("failed to parse graphql auth request")
				http.Error(w, "invalid graphql request", http.StatusBadRequest)
				return
			}
			r = updatedRequest

			token, hasToken, err := extractAuthToken(r)
			if err != nil {
				logger.Warn().Err(err).Msg("invalid authorization header format")
				http.Error(w, "invalid authorization header", http.StatusUnauthorized)
				return
			}

			if !hasToken {
				if queryRequiresAuthentication(query, requiredForFields) {
					logger.Debug().Msg("graphql auth required but token missing")
					http.Error(w, "unauthorized", http.StatusUnauthorized)
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			userID, err := validateGraphQLToken(token)
			if err != nil {
				logger.Warn().Err(err).Msg("invalid graphql auth token")
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), "userID", userID)
			ctx = context.WithValue(ctx, "token", token)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// InstrumentationMiddleware adds tracing and logging to GraphQL requests.
func InstrumentationMiddleware(logger zerolog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			traceID := generateTraceID()

			// Add trace ID to context
			ctx := context.WithValue(r.Context(), "traceID", traceID)
			ctx = context.WithValue(ctx, "startTime", start)

			// Log request
			logger.Info().
				Str("traceID", traceID).
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Str("ip", getClientIP(r)).
				Msg("GraphQL request started")

			// Call next handler
			next.ServeHTTP(w, r.WithContext(ctx))

			// Log response
			duration := time.Since(start)
			logger.Info().
				Str("traceID", traceID).
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Dur("duration", duration).
				Msg("GraphQL request completed")
		})
	}
}

// RateLimitMiddleware enforces per-user or per-IP rate limiting.
func RateLimitMiddleware(logger zerolog.Logger, limiter RateLimiter) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			clientID := getClientID(r)

			if !limiter.AllowRequest(r.Context(), clientID) {
				logger.Warn().
					Str("clientID", clientID).
					Msg("rate limit exceeded")

				w.Header().Set("Retry-After", "60")
				w.Header().Set("X-RateLimit-Remaining", "0")
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}

			remaining := limiter.GetRemaining(clientID)
			w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))

			next.ServeHTTP(w, r)
		})
	}
}

// ErrorHandlingMiddleware catches and formats errors consistently.
func ErrorHandlingMiddleware(logger zerolog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// In a real implementation, this would wrap the response writer
			// to capture panics and format errors
			defer func() {
				if err := recover(); err != nil {
					logger.Error().
						Interface("panic", err).
						Str("method", r.Method).
						Str("path", r.URL.Path).
						Msg("panic recovered in GraphQL handler")

					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					fmt.Fprintf(w, `{"errors":[{"message":"internal server error"}]}`)
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}

// CORSMiddleware adds CORS headers to responses.
func CORSMiddleware(allowedOrigins []string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			// Check if origin is allowed
			allowed := false
			for _, allowedOrigin := range allowedOrigins {
				if allowedOrigin == "*" || allowedOrigin == origin {
					allowed = true
					break
				}
			}

			if allowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
				w.Header().Set("Access-Control-Max-Age", "3600")
			}

			// Handle preflight requests
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequestValidationMiddleware validates incoming requests.
func RequestValidationMiddleware(logger zerolog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Validate content type for POST requests
			if r.Method == http.MethodPost {
				contentType := r.Header.Get("Content-Type")
				if !strings.Contains(contentType, "application/json") {
					logger.Warn().
						Str("contentType", contentType).
						Msg("invalid content type for GraphQL request")

					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusBadRequest)
					fmt.Fprintf(w, `{"errors":[{"message":"content type must be application/json"}]}`)
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// Chain combines multiple middleware in sequence.
func Chain(handler http.Handler, middlewares ...Middleware) http.Handler {
	// Apply middleware in reverse order so they execute in the intended order
	for i := len(middlewares) - 1; i >= 0; i-- {
		handler = middlewares[i](handler)
	}
	return handler
}

// ==================== Helper Functions ====================

type authGraphQLRequest struct {
	Query string `json:"query"`
}

func extractGraphQLQuery(r *http.Request) (string, *http.Request, error) {
	if query := strings.TrimSpace(r.URL.Query().Get("query")); query != "" {
		return query, r, nil
	}

	if r.Body == nil || r.Body == http.NoBody {
		return "", r, nil
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return "", r, err
	}

	r.Body = io.NopCloser(bytes.NewReader(body))
	if len(bytes.TrimSpace(body)) == 0 {
		return "", r, nil
	}

	var payload authGraphQLRequest
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", r, err
	}

	r.Body = io.NopCloser(bytes.NewReader(body))
	return payload.Query, r, nil
}

func extractAuthToken(r *http.Request) (string, bool, error) {
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			return "", false, fmt.Errorf("invalid authorization header")
		}
		token := strings.TrimSpace(parts[1])
		if token == "" {
			return "", false, fmt.Errorf("empty bearer token")
		}
		return token, true, nil
	}

	if sessionToken := strings.TrimSpace(r.Header.Get("X-Session-Token")); sessionToken != "" {
		return sessionToken, true, nil
	}

	return "", false, nil
}

func queryRequiresAuthentication(query string, requiredForFields map[string]bool) bool {
	if len(requiredForFields) == 0 {
		return true
	}

	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return false
	}

	for field, required := range requiredForFields {
		if !required {
			continue
		}
		pattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(field) + `\b`)
		if pattern.MatchString(trimmed) {
			return true
		}
	}

	return false
}

func validateGraphQLToken(token string) (string, error) {
	if token == "" {
		return "", fmt.Errorf("empty token")
	}

	parts := strings.Split(token, ":")
	userID := strings.TrimSpace(parts[0])
	if userID == "" {
		return "", fmt.Errorf("invalid token: empty user ID")
	}
	return userID, nil
}

func generateTraceID() string {
	return fmt.Sprintf("trace-%d", time.Now().UnixNano())
}

func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header (for proxied requests)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}

	// Fall back to RemoteAddr
	return r.RemoteAddr
}

// SimpleRateLimiter is a basic in-memory rate limiter.
type SimpleRateLimiter struct {
	requestsPerMinute int
	clients           map[string]*ClientRateLimit
}

// ClientRateLimit tracks rate limit state for a single client.
type ClientRateLimit struct {
	count     int
	resetTime time.Time
}

// NewSimpleRateLimiter creates a new simple rate limiter.
func NewSimpleRateLimiter(requestsPerMinute int) *SimpleRateLimiter {
	return &SimpleRateLimiter{
		requestsPerMinute: requestsPerMinute,
		clients:           make(map[string]*ClientRateLimit),
	}
}

// AllowRequest checks if a request should be allowed.
func (srl *SimpleRateLimiter) AllowRequest(ctx context.Context, clientID string) bool {
	now := time.Now()

	// Get or create client limit
	limit, exists := srl.clients[clientID]
	if !exists || now.After(limit.resetTime) {
		// Reset limit
		srl.clients[clientID] = &ClientRateLimit{
			count:     1,
			resetTime: now.Add(time.Minute),
		}
		return true
	}

	// Check if limit exceeded
	if limit.count >= srl.requestsPerMinute {
		return false
	}

	limit.count++
	return true
}

// GetRemaining returns the number of remaining requests for a client.
func (srl *SimpleRateLimiter) GetRemaining(clientID string) int {
	limit, exists := srl.clients[clientID]
	if !exists {
		return srl.requestsPerMinute
	}

	if time.Now().After(limit.resetTime) {
		return srl.requestsPerMinute
	}

	return srl.requestsPerMinute - limit.count
}

// SimpleComplexityAnalyzer analyzes GraphQL query complexity.
type SimpleComplexityAnalyzer struct {
	maxDepth int
}

// NewSimpleComplexityAnalyzer creates a new complexity analyzer.
func NewSimpleComplexityAnalyzer(maxDepth int) *SimpleComplexityAnalyzer {
	return &SimpleComplexityAnalyzer{
		maxDepth: maxDepth,
	}
}

// AnalyzeComplexity calculates the complexity score of a query.
func (sca *SimpleComplexityAnalyzer) AnalyzeComplexity(query string) (int, error) {
	// Simplified: count braces and arguments as complexity
	complexity := 0
	complexity += strings.Count(query, "{") * 5
	complexity += strings.Count(query, "(") * 3
	return complexity, nil
}

// ValidateDepth validates that query depth doesn't exceed the limit.
func (sca *SimpleComplexityAnalyzer) ValidateDepth(query string, maxDepth int) (int, error) {
	depth := 0
	currentDepth := 0

	for _, ch := range query {
		if ch == '{' {
			currentDepth++
			if currentDepth > depth {
				depth = currentDepth
			}
		} else if ch == '}' {
			currentDepth--
		}
	}

	if depth > maxDepth {
		return depth, fmt.Errorf("query depth %d exceeds maximum %d", depth, maxDepth)
	}

	return depth, nil
}

// SimpleRequestLogger logs GraphQL requests and responses.
type SimpleRequestLogger struct {
	logger zerolog.Logger
}

// NewSimpleRequestLogger creates a new request logger.
func NewSimpleRequestLogger(logger zerolog.Logger) *SimpleRequestLogger {
	return &SimpleRequestLogger{
		logger: logger,
	}
}

// LogRequest logs a GraphQL request.
func (srl *SimpleRequestLogger) LogRequest(
	ctx context.Context,
	query string,
	operationName string,
	variables map[string]interface{},
) {
	srl.logger.Debug().
		Str("operationName", operationName).
		Int("queryLength", len(query)).
		Interface("variables", variables).
		Msg("GraphQL request")
}

// LogResponse logs a GraphQL response.
func (srl *SimpleRequestLogger) LogResponse(
	ctx context.Context,
	result *ExecutionResult,
	durationMs int64,
) {
	srl.logger.Debug().
		Bool("hasErrors", len(result.Errors) > 0).
		Int("errorCount", len(result.Errors)).
		Int64("durationMs", durationMs).
		Msg("GraphQL response")
}
