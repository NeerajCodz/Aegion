package graphql

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"
)

// DirectiveContext holds context information for directive execution.
type DirectiveContext struct {
	FieldName     string
	ParentType    string
	Arguments     map[string]interface{}
	IsSubscription bool
}

// DirectiveHandler executes a directive.
type DirectiveHandler func(ctx context.Context, next func() error, dirCtx *DirectiveContext) error

// DirectiveRegistry manages custom directives.
type DirectiveRegistry struct {
	logger     zerolog.Logger
	directives map[string]DirectiveHandler
	caches     map[string]Cache
}

// Cache represents a simple caching mechanism.
type Cache interface {
	Get(key string) (interface{}, bool)
	Set(key string, value interface{}, ttl time.Duration)
	Clear()
}

// NewDirectiveRegistry creates a new directive registry.
func NewDirectiveRegistry(logger zerolog.Logger) *DirectiveRegistry {
	return &DirectiveRegistry{
		logger:     logger,
		directives: make(map[string]DirectiveHandler),
		caches:     make(map[string]Cache),
	}
}

// RegisterDirective registers a custom directive handler.
func (dr *DirectiveRegistry) RegisterDirective(name string, handler DirectiveHandler) {
	dr.directives[name] = handler
	dr.logger.Debug().Str("directive", name).Msg("directive registered")
}

// GetDirective retrieves a directive handler.
func (dr *DirectiveRegistry) GetDirective(name string) (DirectiveHandler, bool) {
	handler, ok := dr.directives[name]
	return handler, ok
}

// ==================== Built-in Directives ====================

// AuthDirectiveHandler enforces authentication for a field.
func AuthDirectiveHandler(ctx context.Context, next func() error, dirCtx *DirectiveContext) error {
	// Check if user is authenticated
	if _, ok := ctx.Value("userID").(string); !ok {
		return fmt.Errorf("unauthorized: authentication required for field %s", dirCtx.FieldName)
	}

	// Check required parameter
	if required, ok := dirCtx.Arguments["required"].(bool); ok && required {
		if token, ok := ctx.Value("token").(string); !ok || token == "" {
			return fmt.Errorf("unauthorized: valid token required for field %s", dirCtx.FieldName)
		}
	}

	return next()
}

// CacheDirectiveHandler implements field-level caching.
func (dr *DirectiveRegistry) CacheDirectiveHandler(ctx context.Context, next func() error, dirCtx *DirectiveContext) error {
	// Extract TTL from directive arguments
	ttl := 60 * time.Second // default TTL
	if ttlVal, ok := dirCtx.Arguments["ttl"].(int); ok {
		ttl = time.Duration(ttlVal) * time.Second
	}

	// Generate cache key
	cacheKey := fmt.Sprintf("%s:%s", dirCtx.ParentType, dirCtx.FieldName)

	// Check if cache exists
	cache, ok := dr.caches[dirCtx.ParentType]
	if !ok {
		cache = NewSimpleCache()
		dr.caches[dirCtx.ParentType] = cache
	}

	// Try to get from cache
	if _, found := cache.Get(cacheKey); found {
		return nil
	}

	// Execute the resolver
	if err := next(); err != nil {
		return err
	}

	// Cache the result (simplified - in production, cache actual values)
	cache.Set(cacheKey, true, ttl)
	return nil
}

// DeprecatedDirectiveHandler marks a field as deprecated.
func DeprecatedDirectiveHandler(ctx context.Context, next func() error, dirCtx *DirectiveContext) error {
	reason := "no reason provided"
	if r, ok := dirCtx.Arguments["reason"].(string); ok {
		reason = r
	}

	// Log deprecation warning
	fmt.Printf("DEPRECATION WARNING: Field %s.%s is deprecated: %s\n", dirCtx.ParentType, dirCtx.FieldName, reason)

	// Still execute the field
	return next()
}

// ==================== Simple Cache Implementation ====================

// SimpleCache is a basic in-memory cache.
type SimpleCache struct {
	data map[string]*CacheEntry
}

// CacheEntry holds a cached value with expiration.
type CacheEntry struct {
	value     interface{}
	expiresAt time.Time
}

// NewSimpleCache creates a new simple cache.
func NewSimpleCache() *SimpleCache {
	return &SimpleCache{
		data: make(map[string]*CacheEntry),
	}
}

// Get retrieves a value from the cache.
func (sc *SimpleCache) Get(key string) (interface{}, bool) {
	entry, ok := sc.data[key]
	if !ok {
		return nil, false
	}

	// Check if expired
	if time.Now().After(entry.expiresAt) {
		delete(sc.data, key)
		return nil, false
	}

	return entry.value, true
}

// Set stores a value in the cache with a TTL.
func (sc *SimpleCache) Set(key string, value interface{}, ttl time.Duration) {
	sc.data[key] = &CacheEntry{
		value:     value,
		expiresAt: time.Now().Add(ttl),
	}
}

// Clear removes all entries from the cache.
func (sc *SimpleCache) Clear() {
	sc.data = make(map[string]*CacheEntry)
}

// ==================== Directive Chain Executor ====================

// DirectiveChainExecutor executes a chain of directives.
type DirectiveChainExecutor struct {
	registry *DirectiveRegistry
	logger   zerolog.Logger
}

// NewDirectiveChainExecutor creates a new directive chain executor.
func NewDirectiveChainExecutor(registry *DirectiveRegistry, logger zerolog.Logger) *DirectiveChainExecutor {
	return &DirectiveChainExecutor{
		registry: registry,
		logger:   logger,
	}
}

// Execute executes a chain of directives and calls the resolver.
func (dce *DirectiveChainExecutor) Execute(
	ctx context.Context,
	directives []*DirectiveContext,
	resolver func() error,
) error {
	// Build the chain from inside out
	chain := resolver

	// Apply directives in reverse order (last directive is innermost)
	for i := len(directives) - 1; i >= 0; i-- {
		dirCtx := directives[i]
		handler, ok := dce.registry.GetDirective(dirCtx.FieldName)

		if !ok {
			dce.logger.Warn().
				Str("directive", dirCtx.FieldName).
				Msg("directive handler not found")
			continue
		}

		// Capture current chain
		prevChain := chain
		dirHandler := handler

		chain = func() error {
			return dirHandler(ctx, prevChain, dirCtx)
		}
	}

	return chain()
}

// ==================== Built-in Directives Registration ====================

// RegisterBuiltInDirectives registers all built-in directives.
func RegisterBuiltInDirectives(registry *DirectiveRegistry) {
	registry.RegisterDirective("auth", AuthDirectiveHandler)
	registry.RegisterDirective("deprecated", DeprecatedDirectiveHandler)
	
	// Cache directive is special - needs registry reference
	registry.RegisterDirective("cache", func(ctx context.Context, next func() error, dirCtx *DirectiveContext) error {
		return registry.CacheDirectiveHandler(ctx, next, dirCtx)
	})
}

// ==================== Directive Parser ====================

// DirectiveParser parses directive definitions from schema.
type DirectiveParser struct {
	logger zerolog.Logger
}

// NewDirectiveParser creates a new directive parser.
func NewDirectiveParser(logger zerolog.Logger) *DirectiveParser {
	return &DirectiveParser{
		logger: logger,
	}
}

// ParseDirectives parses directives from a query or schema.
func (dp *DirectiveParser) ParseDirectives(schemaSDL string) map[string]interface{} {
	directives := make(map[string]interface{})

	// This is a simplified parser - in production, use a proper GraphQL parser
	// For now, just extract directive definitions
	if directives, err := extractDirectiveDefinitions(schemaSDL); err == nil {
		return directives
	}

	return directives
}

func extractDirectiveDefinitions(schema string) (map[string]interface{}, error) {
	// Placeholder implementation
	return map[string]interface{}{
		"auth": map[string]interface{}{
			"description": "Requires authentication",
			"locations":   []string{"FIELD_DEFINITION"},
			"arguments": map[string]interface{}{
				"required": map[string]interface{}{
					"type": "Boolean",
				},
			},
		},
		"cache": map[string]interface{}{
			"description": "Caches field results",
			"locations":   []string{"FIELD_DEFINITION"},
			"arguments": map[string]interface{}{
				"ttl": map[string]interface{}{
					"type": "Int",
				},
			},
		},
		"deprecated": map[string]interface{}{
			"description": "Marks a field as deprecated",
			"locations":   []string{"FIELD_DEFINITION", "ENUM_VALUE"},
			"arguments": map[string]interface{}{
				"reason": map[string]interface{}{
					"type": "String",
				},
			},
		},
	}, nil
}

// ==================== Directive Validator ====================

// DirectiveValidator validates directive usage in queries.
type DirectiveValidator struct {
	registry *DirectiveRegistry
	logger   zerolog.Logger
}

// NewDirectiveValidator creates a new directive validator.
func NewDirectiveValidator(registry *DirectiveRegistry, logger zerolog.Logger) *DirectiveValidator {
	return &DirectiveValidator{
		registry: registry,
		logger:   logger,
	}
}

// ValidateDirectiveUsage validates that directives are used correctly.
func (dv *DirectiveValidator) ValidateDirectiveUsage(query string) error {
	// Simplified validation - in production, use a proper GraphQL parser
	// This would check:
	// 1. Directives exist
	// 2. Arguments match the directive definition
	// 3. Directives are applied to valid locations

	return nil
}
