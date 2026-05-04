package authtoken

import (
	"context"
	"log/slog"
	"net/http"
	"path"
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
	Logger *slog.Logger
	// SkipPaths are paths that bypass token validation
	SkipPaths []string
}

// Middleware creates HTTP middleware that validates internal auth tokens.
func Middleware(cfg MiddlewareConfig) func(http.Handler) http.Handler {
	skipPaths := make(map[string]bool)
	for _, p := range cfg.SkipPaths {
		normalized := path.Clean(p)
		if normalized == "." {
			normalized = "/"
		}
		skipPaths[normalized] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			currentPath := path.Clean(r.URL.Path)
			if currentPath == "." {
				currentPath = "/"
			}
			// Skip configured paths
			if skipPaths[currentPath] {
				next.ServeHTTP(w, r)
				return
			}

			if cfg.Generator == nil {
				if cfg.Logger != nil {
					cfg.Logger.ErrorContext(ctx, "internal auth generator unavailable",
						"path", r.URL.Path,
						"method", r.Method,
					)
				}
				http.Error(w, "internal auth unavailable", http.StatusServiceUnavailable)
				return
			}

			token := r.Header.Get(HeaderInternalToken)
			if token == "" {
				if cfg.Logger != nil {
					cfg.Logger.WarnContext(ctx, "missing internal auth token",
						"path", r.URL.Path,
						"method", r.Method,
					)
				}
				http.Error(w, "missing internal auth token", http.StatusUnauthorized)
				return
			}

			moduleID, err := cfg.Generator.ValidateString(token)
			if err != nil {
				if cfg.Logger != nil {
					cfg.Logger.WarnContext(ctx, "invalid internal auth token",
						"error", err,
						"path", r.URL.Path,
						"method", r.Method,
					)
				}
				http.Error(w, "invalid internal auth token", http.StatusUnauthorized)
				return
			}

			// Add module ID to context for downstream handlers
			ctx = context.WithValue(ctx, ContextKeyModuleID, moduleID)

			if cfg.Logger != nil {
				cfg.Logger.DebugContext(ctx, "internal auth validated",
					"module_id", moduleID,
					"path", r.URL.Path,
					"method", r.Method,
				)
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
