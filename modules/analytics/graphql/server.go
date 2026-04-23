package graphql

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// Server handles GraphQL HTTP and WebSocket requests.
type Server struct {
	logger                 zerolog.Logger
	resolver               *Resolver
	schemaBuilder          *SchemaBuilder
	maxQueryDepth          int
	maxQueryComplexity     int
	queryTimeoutSeconds    int
	rateLimitPerMinute     int
	enableIntrospection    bool
	enablePlayground       bool
	queryExecutor          QueryExecutor
	requestLogger          RequestLogger
	complexityAnalyzer     ComplexityAnalyzer
	rateLimiter            RateLimiter
}

// QueryExecutor executes parsed GraphQL queries.
type QueryExecutor interface {
	Execute(ctx context.Context, query string, operationName string, variables map[string]interface{}) (*ExecutionResult, error)
}

// RequestLogger logs GraphQL requests and responses.
type RequestLogger interface {
	LogRequest(ctx context.Context, query string, operationName string, variables map[string]interface{})
	LogResponse(ctx context.Context, result *ExecutionResult, durationMs int64)
}

// ComplexityAnalyzer analyzes query complexity.
type ComplexityAnalyzer interface {
	AnalyzeComplexity(query string) (int, error)
	ValidateDepth(query string, maxDepth int) (int, error)
}

// RateLimiter tracks and enforces rate limits.
type RateLimiter interface {
	AllowRequest(ctx context.Context, clientID string) bool
	GetRemaining(clientID string) int
}

// ExecutionResult represents the result of executing a GraphQL query.
type ExecutionResult struct {
	Data       interface{}   `json:"data,omitempty"`
	Errors     []*GraphQLError `json:"errors,omitempty"`
	Extensions map[string]interface{} `json:"extensions,omitempty"`
}

// GraphQLError represents an error in GraphQL execution.
type GraphQLError struct {
	Message    string                 `json:"message"`
	Locations  []interface{}          `json:"locations,omitempty"`
	Path       []interface{}          `json:"path,omitempty"`
	Extensions map[string]interface{} `json:"extensions,omitempty"`
}

// GraphQLRequest represents a GraphQL request.
type GraphQLRequest struct {
	Query         string                 `json:"query"`
	OperationName string                 `json:"operationName,omitempty"`
	Variables     map[string]interface{} `json:"variables,omitempty"`
}

// NewServer creates a new GraphQL HTTP server.
func NewServer(
	logger zerolog.Logger,
	resolver *Resolver,
	schemaBuilder *SchemaBuilder,
	executor QueryExecutor,
	opts ...ServerOption,
) *Server {
	s := &Server{
		logger:              logger,
		resolver:            resolver,
		schemaBuilder:       schemaBuilder,
		maxQueryDepth:       10,
		maxQueryComplexity:  1000,
		queryTimeoutSeconds: 30,
		rateLimitPerMinute:  100,
		enableIntrospection: true,
		enablePlayground:    true,
		queryExecutor:       executor,
		requestLogger:       &noOpRequestLogger{},
		complexityAnalyzer:  &noOpComplexityAnalyzer{},
		rateLimiter:         &noOpRateLimiter{},
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// ServerOption is a functional option for configuring the server.
type ServerOption func(*Server)

// WithMaxQueryDepth sets the maximum query depth.
func WithMaxQueryDepth(depth int) ServerOption {
	return func(s *Server) {
		s.maxQueryDepth = depth
	}
}

// WithMaxQueryComplexity sets the maximum query complexity.
func WithMaxQueryComplexity(complexity int) ServerOption {
	return func(s *Server) {
		s.maxQueryComplexity = complexity
	}
}

// WithQueryTimeout sets the query execution timeout.
func WithQueryTimeout(seconds int) ServerOption {
	return func(s *Server) {
		s.queryTimeoutSeconds = seconds
	}
}

// WithRateLimit sets the rate limit per minute.
func WithRateLimit(limit int) ServerOption {
	return func(s *Server) {
		s.rateLimitPerMinute = limit
	}
}

// WithIntrospection enables or disables schema introspection.
func WithIntrospection(enabled bool) ServerOption {
	return func(s *Server) {
		s.enableIntrospection = enabled
	}
}

// WithPlayground enables or disables GraphQL Playground.
func WithPlayground(enabled bool) ServerOption {
	return func(s *Server) {
		s.enablePlayground = enabled
	}
}

// WithRequestLogger sets the request logger.
func WithRequestLogger(logger RequestLogger) ServerOption {
	return func(s *Server) {
		s.requestLogger = logger
	}
}

// WithComplexityAnalyzer sets the complexity analyzer.
func WithComplexityAnalyzer(analyzer ComplexityAnalyzer) ServerOption {
	return func(s *Server) {
		s.complexityAnalyzer = analyzer
	}
}

// WithRateLimiter sets the rate limiter.
func WithRateLimiter(limiter RateLimiter) ServerOption {
	return func(s *Server) {
		s.rateLimiter = limiter
	}
}

// HandleQuery handles POST requests with GraphQL queries.
func (s *Server) HandleQuery(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Check rate limit
	clientID := getClientID(r)
	if !s.rateLimiter.AllowRequest(r.Context(), clientID) {
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"errors": []map[string]string{
				{"message": "rate limit exceeded"},
			},
		})
		return
	}

	// Parse request
	var req GraphQLRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"errors": []map[string]string{
				{"message": fmt.Sprintf("invalid request: %v", err)},
			},
		})
		return
	}

	// Log request
	s.requestLogger.LogRequest(r.Context(), req.Query, req.OperationName, req.Variables)

	// Validate query depth
	if depth, err := s.complexityAnalyzer.ValidateDepth(req.Query, s.maxQueryDepth); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(&ExecutionResult{
			Errors: []*GraphQLError{
				{
					Message: fmt.Sprintf("query depth exceeded: %d > %d", depth, s.maxQueryDepth),
				},
			},
		})
		return
	}

	// Validate query complexity
	if complexity, err := s.complexityAnalyzer.AnalyzeComplexity(req.Query); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(&ExecutionResult{
			Errors: []*GraphQLError{
				{
					Message: fmt.Sprintf("query complexity invalid: %v", err),
				},
			},
		})
		return
	} else if complexity > s.maxQueryComplexity {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(&ExecutionResult{
			Errors: []*GraphQLError{
				{
					Message: fmt.Sprintf("query complexity exceeded: %d > %d", complexity, s.maxQueryComplexity),
				},
			},
		})
		return
	}

	// Add timeout to context
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(s.queryTimeoutSeconds)*time.Second)
	defer cancel()

	// Execute query
	start := time.Now()
	result, err := s.queryExecutor.Execute(ctx, req.Query, req.OperationName, req.Variables)
	executionTimeMs := time.Since(start).Milliseconds()

	if err != nil {
		s.logger.Error().Err(err).Msg("query execution failed")
		result = &ExecutionResult{
			Errors: []*GraphQLError{
				{
					Message: "internal server error",
				},
			},
		}
	}

	// Add execution time to extensions
	if result.Extensions == nil {
		result.Extensions = make(map[string]interface{})
	}
	result.Extensions["executionTimeMs"] = executionTimeMs

	// Log response
	s.requestLogger.LogResponse(ctx, result, executionTimeMs)

	// Write response
	w.Header().Set("X-Execution-Time-Ms", fmt.Sprintf("%d", executionTimeMs))
	json.NewEncoder(w).Encode(result)
}

// HandlePlayground handles GET requests for GraphQL Playground.
func (s *Server) HandlePlayground(w http.ResponseWriter, r *http.Request) {
	if !s.enablePlayground {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Playground not available"))
		return
	}

	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(playgroundHTML))
}

// HandleIntrospection handles introspection queries.
func (s *Server) HandleIntrospection(w http.ResponseWriter, r *http.Request) {
	if !s.enableIntrospection {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"errors": []map[string]string{
				{"message": "introspection disabled"},
			},
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	schema, err := s.schemaBuilder.Build(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"errors": []map[string]string{
				{"message": fmt.Sprintf("failed to build schema: %v", err)},
			},
		})
		return
	}

	// Return schema as introspection result
	json.NewEncoder(w).Encode(map[string]interface{}{
		"data": map[string]interface{}{
			"__schema": parseSchema(schema),
		},
	})
}

// HandleHealth handles health check requests.
func (s *Server) HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// Check resolver health
	if s.resolver == nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "down",
			"ready":  false,
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "up",
		"ready":  true,
		"time":   time.Now(),
	})
}

// RegisterRoutes registers GraphQL routes on the given HTTP router.
// This is a generic helper - actual registration depends on the router being used (chi, mux, etc.)
func (s *Server) RegisterRoutes(mux interface{}, basePath string) error {
	// This is a placeholder. In real implementation, this would register
	// routes with the specific router (chi, mux, etc.)
	s.logger.Info().
		Str("basePath", basePath).
		Bool("playgroundEnabled", s.enablePlayground).
		Bool("introspectionEnabled", s.enableIntrospection).
		Msg("GraphQL server configured")
	return nil
}

// ==================== Helper Functions ====================

func getClientID(r *http.Request) string {
	// Try to get from authorization header
	if auth := r.Header.Get("Authorization"); auth != "" {
		return auth
	}
	// Fall back to IP address
	return r.RemoteAddr
}

func parseSchema(schema string) map[string]interface{} {
	// Simplified schema parsing - in production, use a proper GraphQL parser
	return map[string]interface{}{
		"types": []interface{}{},
		"queryType": map[string]string{
			"name": "Query",
		},
		"mutationType": map[string]string{
			"name": "Mutation",
		},
		"subscriptionType": map[string]string{
			"name": "Subscription",
		},
	}
}

// ==================== No-op Implementations ====================

type noOpRequestLogger struct{}

func (nrl *noOpRequestLogger) LogRequest(ctx context.Context, query string, operationName string, variables map[string]interface{}) {
}

func (nrl *noOpRequestLogger) LogResponse(ctx context.Context, result *ExecutionResult, durationMs int64) {
}

type noOpComplexityAnalyzer struct{}

func (nca *noOpComplexityAnalyzer) AnalyzeComplexity(query string) (int, error) {
	return 1, nil
}

func (nca *noOpComplexityAnalyzer) ValidateDepth(query string, maxDepth int) (int, error) {
	depth := strings.Count(query, "{")
	if depth > maxDepth {
		return depth, fmt.Errorf("depth exceeded")
	}
	return depth, nil
}

type noOpRateLimiter struct{}

func (nrl *noOpRateLimiter) AllowRequest(ctx context.Context, clientID string) bool {
	return true
}

func (nrl *noOpRateLimiter) GetRemaining(clientID string) int {
	return 100
}

// ==================== GraphQL Playground HTML ====================

const playgroundHTML = `
<!DOCTYPE html>
<html>
<head>
	<meta charset=utf-8/>
	<meta name="viewport" content="width=device-width, initial-scale=1"/>
	<title>GraphQL Playground</title>
	<link rel="stylesheet" href="//cdn.jsdelivr.net/npm/graphql-playground-react/build/static/css/index.css"/>
	<link rel="shortcut icon" href="//cdn.jsdelivr.net/npm/graphql-playground-react/build/favicon.png"/>
	<script src="//cdn.jsdelivr.net/npm/graphql-playground-react/build/static/js/middleware.js"></script>
</head>
<body>
	<div id="root"></div>
	<script>
		window.addEventListener('load', function (event) {
			GraphQLPlayground.init(document.getElementById('root'), {
				endpoint: window.location.origin + '/graphql',
				subscriptionEndpoint: window.location.origin + '/graphql',
				settings: {
					'general.betaUpdates': false,
					'editor.cursorShape': 'line',
					'editor.fontSize': 14,
					'editor.fontFamily': '\'Source Code Pro\', \'Courier New\', Courier, monospace',
					'editor.theme': 'dark',
					'editor.reuseHeaders': true,
					'request.credentials': 'same-origin',
					'schema.polling.enable': true,
					'schema.polling.interval': 5000,
				},
			});
		});
	</script>
</body>
</html>
`

// SimpleQueryExecutor is a basic implementation of QueryExecutor.
type SimpleQueryExecutor struct {
	resolver *Resolver
	logger   zerolog.Logger
}

// NewSimpleQueryExecutor creates a new simple query executor.
func NewSimpleQueryExecutor(resolver *Resolver, logger zerolog.Logger) *SimpleQueryExecutor {
	return &SimpleQueryExecutor{
		resolver: resolver,
		logger:   logger,
	}
}

// Execute executes a GraphQL query (simplified implementation).
func (e *SimpleQueryExecutor) Execute(
	ctx context.Context,
	query string,
	operationName string,
	variables map[string]interface{},
) (*ExecutionResult, error) {
	// In production, this would parse the query and execute it with the resolver
	// For now, return a simple result
	return &ExecutionResult{
		Data: map[string]interface{}{
			"message": "GraphQL query received",
		},
	}, nil
}
