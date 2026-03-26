package authtoken

import (
	"context"
	"net/http"

	"github.com/rs/zerolog"
)

const (
	// HeaderInternalToken is the header name for internal auth tokens.
	HeaderInternalToken = "X-Aegion-Internal-Token"
)

// contextKey is a type for context keys.
type contextKey string

const (
	// ContextKeyModuleID is the context key for the module ID.
	ContextKeyModuleID contextKey = "aegion_module_id"
)

// MiddlewareConfig holds middleware configuration.
type MiddlewareConfig struct {
	// Generator is the token generator/validator
	Generator *Generator
	// Logger is optional; if nil, no logging occurs
	Logger *zerolog.Logger
	// SkipPaths are paths that bypass token validation
	SkipPaths []string
}

// Middleware creates HTTP middleware that validates internal auth tokens.
func Middleware(cfg MiddlewareConfig) func(http.Handler) http.Handler {
	skipPaths := make(map[string]bool)
	for _, p := range cfg.SkipPaths {
		skipPaths[p] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip configured paths
			if skipPaths[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}

			token := r.Header.Get(HeaderInternalToken)
			if token == "" {
				if cfg.Logger != nil {
					cfg.Logger.Warn().
						Str("path", r.URL.Path).
						Str("method", r.Method).
						Msg("missing internal auth token")
				}
				http.Error(w, "missing internal auth token", http.StatusUnauthorized)
				return
			}

			moduleID, err := cfg.Generator.ValidateString(token)
			if err != nil {
				if cfg.Logger != nil {
					cfg.Logger.Warn().
						Err(err).
						Str("path", r.URL.Path).
						Str("method", r.Method).
						Msg("invalid internal auth token")
				}
				http.Error(w, "invalid internal auth token", http.StatusUnauthorized)
				return
			}

			// Add module ID to context for downstream handlers
			ctx := context.WithValue(r.Context(), ContextKeyModuleID, moduleID)

			if cfg.Logger != nil {
				cfg.Logger.Debug().
					Str("module_id", moduleID).
					Str("path", r.URL.Path).
					Str("method", r.Method).
					Msg("internal auth validated")
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ModuleIDFromContext extracts the module ID from the request context.
func ModuleIDFromContext(ctx context.Context) string {
	if v := ctx.Value(ContextKeyModuleID); v != nil {
		if moduleID, ok := v.(string); ok {
			return moduleID
		}
	}
	return ""
}

// RequireModuleID creates middleware that only allows specific module IDs.
func RequireModuleID(allowedModules ...string) func(http.Handler) http.Handler {
	allowed := make(map[string]bool)
	for _, m := range allowedModules {
		allowed[m] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			moduleID := ModuleIDFromContext(r.Context())
			if moduleID == "" || !allowed[moduleID] {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
