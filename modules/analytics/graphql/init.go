package graphql

import (
	"context"
	"fmt"

	"github.com/aegion/aegion/internal/xlog"
)

// Config holds GraphQL module configuration.
type Config struct {
	// Enabled determines if GraphQL is active
	Enabled bool `yaml:"enabled"`

	// Endpoint is the HTTP path for GraphQL queries
	Endpoint string `yaml:"endpoint"`

	// EnableIntrospection enables schema introspection
	EnableIntrospection bool `yaml:"introspection"`

	// EnablePlayground enables GraphQL Playground
	EnablePlayground bool `yaml:"playground"`

	// MaxQueryDepth limits the depth of queries
	MaxQueryDepth int `yaml:"max_query_depth"`

	// MaxQueryComplexity limits the complexity score of queries
	MaxQueryComplexity int `yaml:"max_query_complexity"`

	// QueryTimeoutSeconds is the timeout for query execution
	QueryTimeoutSeconds int `yaml:"query_timeout_seconds"`

	// RateLimitPerMinute limits requests per minute
	RateLimitPerMinute int `yaml:"rate_limit_per_minute"`
}

// DefaultConfig returns a sensible default configuration.
func DefaultConfig() *Config {
	return &Config{
		Enabled:             true,
		Endpoint:            "/graphql",
		EnableIntrospection: true,
		EnablePlayground:    true,
		MaxQueryDepth:       10,
		MaxQueryComplexity:  1000,
		QueryTimeoutSeconds: 30,
		RateLimitPerMinute:  100,
	}
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if !c.Enabled {
		return nil
	}

	if c.Endpoint == "" {
		return fmt.Errorf("graphql endpoint must be set")
	}

	if c.MaxQueryDepth <= 0 {
		return fmt.Errorf("max_query_depth must be greater than 0")
	}

	if c.MaxQueryComplexity <= 0 {
		return fmt.Errorf("max_query_complexity must be greater than 0")
	}

	if c.QueryTimeoutSeconds <= 0 {
		return fmt.Errorf("query_timeout_seconds must be greater than 0")
	}

	if c.RateLimitPerMinute <= 0 {
		return fmt.Errorf("rate_limit_per_minute must be greater than 0")
	}

	return nil
}

// Module represents the GraphQL module.
type Module struct {
	logger             *xlog.Logger
	config             *Config
	server             *Server
	directiveRegistry  *DirectiveRegistry
	complexityAnalyzer ComplexityAnalyzer
	rateLimiter        RateLimiter
	requestLogger      RequestLogger
	queryExecutor      QueryExecutor
	resolver           *Resolver
}

// InitOptions holds options for initializing the GraphQL module.
type InitOptions struct {
	Logger             *xlog.Logger
	Config             *Config
	Store              Store
	DirectiveRegistry  *DirectiveRegistry
	ComplexityAnalyzer ComplexityAnalyzer
	RateLimiter        RateLimiter
	RequestLogger      RequestLogger
	QueryExecutor      QueryExecutor
}

// Initialize initializes the GraphQL module.
func Initialize(ctx context.Context, opts InitOptions) (*Module, error) {
	// Validate config
	if err := opts.Config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid graphql config: %w", err)
	}

	logger := opts.Logger
	if logger == nil {
		logger = xlog.Default()
	}

	// Create resolver
	resolver := NewResolver(logger, opts.Store)

	// Create schema builder
	schemaBuilder := NewSchemaBuilder(resolver)

	// Set up directive registry if not provided
	directiveRegistry := opts.DirectiveRegistry
	if directiveRegistry == nil {
		directiveRegistry = NewDirectiveRegistry(logger)
		RegisterBuiltInDirectives(directiveRegistry)
	}

	// Set up complexity analyzer if not provided
	complexityAnalyzer := opts.ComplexityAnalyzer
	if complexityAnalyzer == nil {
		complexityAnalyzer = NewSimpleComplexityAnalyzer(opts.Config.MaxQueryDepth)
	}

	// Set up rate limiter if not provided
	rateLimiter := opts.RateLimiter
	if rateLimiter == nil {
		rateLimiter = NewSimpleRateLimiter(opts.Config.RateLimitPerMinute)
	}

	// Set up request logger if not provided
	requestLogger := opts.RequestLogger
	if requestLogger == nil {
		requestLogger = NewSimpleRequestLogger(logger)
	}

	// Set up query executor if not provided
	queryExecutor := opts.QueryExecutor
	if queryExecutor == nil {
		queryExecutor = NewSimpleQueryExecutor(resolver, logger)
	}

	// Create server
	server := NewServer(
		logger,
		resolver,
		schemaBuilder,
		queryExecutor,
		WithMaxQueryDepth(opts.Config.MaxQueryDepth),
		WithMaxQueryComplexity(opts.Config.MaxQueryComplexity),
		WithQueryTimeout(opts.Config.QueryTimeoutSeconds),
		WithRateLimit(opts.Config.RateLimitPerMinute),
		WithIntrospection(opts.Config.EnableIntrospection),
		WithPlayground(opts.Config.EnablePlayground),
		WithComplexityAnalyzer(complexityAnalyzer),
		WithRateLimiter(rateLimiter),
		WithRequestLogger(requestLogger),
	)

	module := &Module{
		logger:             logger,
		config:             opts.Config,
		server:             server,
		directiveRegistry:  directiveRegistry,
		complexityAnalyzer: complexityAnalyzer,
		rateLimiter:        rateLimiter,
		requestLogger:      requestLogger,
		queryExecutor:      queryExecutor,
		resolver:           resolver,
	}

	logger.Info("GraphQL module initialized",
		"endpoint", opts.Config.Endpoint,
		"playground", opts.Config.EnablePlayground,
		"introspection", opts.Config.EnableIntrospection,
	)

	return module, nil
}

// Start starts the GraphQL module.
func (m *Module) Start(ctx context.Context) error {
	m.logger.Info("GraphQL module started")
	return nil
}

// Stop stops the GraphQL module.
func (m *Module) Stop(ctx context.Context) error {
	m.logger.Info("GraphQL module stopped")
	return nil
}

// GetServer returns the GraphQL server.
func (m *Module) GetServer() *Server {
	return m.server
}

// GetResolver returns the GraphQL resolver.
func (m *Module) GetResolver() *Resolver {
	return m.resolver
}

// GetDirectiveRegistry returns the directive registry.
func (m *Module) GetDirectiveRegistry() *DirectiveRegistry {
	return m.directiveRegistry
}

// Health returns the health status of the GraphQL module.
func (m *Module) Health(ctx context.Context) (string, error) {
	if m.resolver == nil {
		return "down", fmt.Errorf("resolver not initialized")
	}

	// Check resolver dependencies
	health, err := m.resolver.Health(ctx)
	if err != nil {
		return "down", err
	}

	if !health.IsHealthy {
		return "degraded", nil
	}

	return "up", nil
}
