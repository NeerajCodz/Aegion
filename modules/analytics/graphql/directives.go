package graphql

import (
	"context"
	"fmt"
	"regexp"
	"strings"
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

	if directives, err := extractDirectiveDefinitions(schemaSDL); err == nil {
		return directives
	}

	return directives
}

func extractDirectiveDefinitions(schema string) (map[string]interface{}, error) {
	directives := make(map[string]interface{})
	matches := directiveDefinitionRegex.FindAllStringSubmatch(schema, -1)
	for _, match := range matches {
		name := match[1]
		args := parseDirectiveArguments(match[3])
		locations := parseDirectiveLocations(match[4])
		directives[name] = map[string]interface{}{
			"description": fmt.Sprintf("Directive %s", name),
			"locations":   locations,
			"arguments":   args,
		}
	}

	if len(directives) == 0 {
		return nil, fmt.Errorf("no directives found")
	}

	return directives, nil
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
	matches := directiveUsageRegex.FindAllStringSubmatch(query, -1)
	for _, match := range matches {
		name := match[1]
		args := match[3]

		if _, ok := dv.registry.GetDirective(name); !ok {
			return fmt.Errorf("directive %q is not registered", name)
		}

		switch name {
		case "auth":
			if strings.TrimSpace(args) == "" {
				continue
			}
			if !authDirectiveArgsRegex.MatchString(args) {
				return fmt.Errorf("directive %q expects optional Boolean required argument", name)
			}
		case "cache":
			if strings.TrimSpace(args) == "" {
				continue
			}
			if !cacheDirectiveArgsRegex.MatchString(args) {
				return fmt.Errorf("directive %q expects ttl: Int argument", name)
			}
		case "deprecated":
			if strings.TrimSpace(args) == "" {
				continue
			}
			if !deprecatedDirectiveArgsRegex.MatchString(args) {
				return fmt.Errorf("directive %q expects reason: String argument", name)
			}
		}
	}

	return nil
}

var (
	directiveDefinitionRegex   = regexp.MustCompile(`directive\s+@([A-Za-z_][A-Za-z0-9_]*)\s*(\(([^)]*)\))?\s+on\s+([A-Z_\s|]+)`)
	directiveArgumentRegex     = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)\s*:\s*([A-Za-z!\[\]_][A-Za-z0-9!\[\]_]*)(\s*=\s*([^,)]+))?`)
	directiveUsageRegex        = regexp.MustCompile(`@([A-Za-z_][A-Za-z0-9_]*)(\(([^)]*)\))?`)
	authDirectiveArgsRegex     = regexp.MustCompile(`^\s*required\s*:\s*(true|false)\s*$`)
	cacheDirectiveArgsRegex    = regexp.MustCompile(`^\s*ttl\s*:\s*\d+\s*$`)
	deprecatedDirectiveArgsRegex = regexp.MustCompile(`^\s*reason\s*:\s*"[^"]*"\s*$`)
)

func parseDirectiveArguments(definition string) map[string]interface{} {
	result := make(map[string]interface{})
	for _, match := range directiveArgumentRegex.FindAllStringSubmatch(definition, -1) {
		argName := match[1]
		argType := match[2]
		entry := map[string]interface{}{"type": argType}
		if defaultValue := strings.TrimSpace(match[4]); defaultValue != "" {
			entry["default"] = strings.Trim(defaultValue, `"`)
		}
		result[argName] = entry
	}
	return result
}

func parseDirectiveLocations(definition string) []string {
	parts := strings.Split(definition, "|")
	locations := make([]string, 0, len(parts))
	for _, part := range parts {
		location := strings.TrimSpace(part)
		if location != "" {
			locations = append(locations, location)
		}
	}
	return locations
}
