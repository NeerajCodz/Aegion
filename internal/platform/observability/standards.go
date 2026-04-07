package observability

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
)

var (
	numericSegmentPattern = regexp.MustCompile(`^\d+$`)
	uuidSegmentPattern    = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	hexSegmentPattern     = regexp.MustCompile(`^[0-9a-f]{16,}$`)
	tokenSegmentPattern   = regexp.MustCompile(`^[a-zA-Z0-9_-]{20,}$`)
)

// RoutePattern returns the chi route pattern from request context when available.
func RoutePattern(r *http.Request) string {
	if r == nil {
		return ""
	}
	if routeCtx := chi.RouteContext(r.Context()); routeCtx != nil {
		return routeCtx.RoutePattern()
	}
	return ""
}

// HTTPRouteLabel returns a low-cardinality HTTP route label.
func HTTPRouteLabel(routePattern, requestPath string) string {
	candidate := strings.TrimSpace(routePattern)
	if candidate == "" {
		candidate = strings.TrimSpace(requestPath)
	}
	if candidate == "" {
		return "/"
	}

	if idx := strings.IndexAny(candidate, "?#"); idx >= 0 {
		candidate = candidate[:idx]
	}
	if candidate == "" {
		return "/"
	}
	if !strings.HasPrefix(candidate, "/") {
		candidate = "/" + candidate
	}

	parts := strings.Split(candidate, "/")
	for i := range parts {
		parts[i] = sanitizePathSegment(parts[i])
	}

	label := strings.Join(parts, "/")
	if label == "" {
		label = "/"
	}
	if len(label) > 1 && strings.HasSuffix(label, "/") {
		label = strings.TrimSuffix(label, "/")
	}
	return label
}

// NormalizeHTTPMethod returns a canonical HTTP method label.
func NormalizeHTTPMethod(method string) string {
	trimmed := strings.ToUpper(strings.TrimSpace(method))
	if trimmed == "" {
		return "UNKNOWN"
	}
	return trimmed
}

// NormalizeDBOperation returns a normalized database operation label.
func NormalizeDBOperation(operation string) string {
	normalized := strings.ToUpper(strings.TrimSpace(operation))
	if normalized == "" {
		return "UNKNOWN"
	}
	return normalized
}

// NormalizeDBResource returns a normalized database resource label.
func NormalizeDBResource(resource string) string {
	normalized := strings.ToLower(strings.TrimSpace(resource))
	if normalized == "" {
		return "unknown"
	}

	normalized = strings.ReplaceAll(normalized, " ", "_")
	normalized = strings.ReplaceAll(normalized, `"`, "")
	if len(normalized) > 64 {
		normalized = normalized[:64]
	}
	return normalized
}

func sanitizePathSegment(segment string) string {
	if segment == "" || segment == "*" {
		return segment
	}

	if strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}") {
		return segment
	}
	if strings.HasPrefix(segment, ":") {
		return segment
	}

	lower := strings.ToLower(segment)
	switch {
	case len(segment) > 64:
		return "{id}"
	case numericSegmentPattern.MatchString(segment):
		return "{id}"
	case uuidSegmentPattern.MatchString(lower):
		return "{id}"
	case hexSegmentPattern.MatchString(lower):
		return "{id}"
	case tokenSegmentPattern.MatchString(segment):
		return "{id}"
	default:
		return segment
	}
}
