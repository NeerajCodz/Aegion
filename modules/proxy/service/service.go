package service

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	coreproxy "github.com/aegion/aegion/core/proxy"
	"github.com/aegion/aegion/core/session"
	"github.com/aegion/aegion/modules/proxy/store"
	"github.com/google/uuid"
)

var (
	ErrInvalidProxyConfig = errors.New("invalid proxy configuration")
)

type EffectiveConfig struct {
	Upstreams []store.Upstream `json:"upstreams"`
	Routes    []store.Route    `json:"routes"`
}

type SimulateRequest struct {
	Path          string   `json:"path"`
	Method        string   `json:"method"`
	Authenticated bool     `json:"authenticated"`
	AAL           string   `json:"aal,omitempty"`
	Capabilities  []string `json:"capabilities,omitempty"`
}

type SimulateResponse struct {
	Matched        bool            `json:"matched"`
	Rule           *store.Route    `json:"rule,omitempty"`
	Upstream       *store.Upstream `json:"upstream,omitempty"`
	RewrittenPath  string          `json:"rewritten_path,omitempty"`
	Allowed        bool            `json:"allowed"`
	DenialReason   string          `json:"denial_reason,omitempty"`
	IdentityNeeded bool            `json:"identity_needed"`
}

type Service struct {
	repo store.Repository
	now  func() time.Time
}

func New(repo store.Repository) *Service {
	if repo == nil {
		repo = store.New()
	}
	return &Service{
		repo: repo,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func (s *Service) ListUpstreams(ctx context.Context) ([]store.Upstream, error) {
	return s.repo.ListUpstreams(ctx)
}

func (s *Service) GetUpstream(ctx context.Context, name string) (*store.Upstream, error) {
	return s.repo.GetUpstreamByName(ctx, name)
}

func (s *Service) UpsertUpstream(ctx context.Context, upstream store.Upstream) (*store.Upstream, error) {
	normalized, err := normalizeUpstream(upstream)
	if err != nil {
		return nil, err
	}
	return s.repo.UpsertUpstream(ctx, normalized)
}

func (s *Service) DeleteUpstream(ctx context.Context, name string) error {
	return s.repo.DeleteUpstream(ctx, name)
}

func (s *Service) ListRoutes(ctx context.Context) ([]store.Route, error) {
	return s.repo.ListRoutes(ctx)
}

func (s *Service) GetRoute(ctx context.Context, id string) (*store.Route, error) {
	return s.repo.GetRouteByID(ctx, id)
}

func (s *Service) UpsertRoute(ctx context.Context, route store.Route) (*store.Route, error) {
	normalized, err := s.normalizeRoute(ctx, route)
	if err != nil {
		return nil, err
	}
	return s.repo.UpsertRoute(ctx, normalized)
}

func (s *Service) DeleteRoute(ctx context.Context, id string) error {
	return s.repo.DeleteRoute(ctx, id)
}

func (s *Service) EffectiveConfig(ctx context.Context) (*EffectiveConfig, error) {
	upstreams, err := s.repo.ListUpstreams(ctx)
	if err != nil {
		return nil, err
	}
	routes, err := s.repo.ListRoutes(ctx)
	if err != nil {
		return nil, err
	}
	return &EffectiveConfig{
		Upstreams: upstreams,
		Routes:    routes,
	}, nil
}

func (s *Service) Simulate(ctx context.Context, req SimulateRequest) (*SimulateResponse, error) {
	upstreams, err := s.repo.ListUpstreams(ctx)
	if err != nil {
		return nil, err
	}
	routes, err := s.repo.ListRoutes(ctx)
	if err != nil {
		return nil, err
	}
	engine, upstreamByName, err := buildRuleEngine(upstreams, routes)
	if err != nil {
		return nil, err
	}

	requestPath := strings.TrimSpace(req.Path)
	if requestPath == "" {
		requestPath = "/"
	}
	if !strings.HasPrefix(requestPath, "/") {
		requestPath = "/" + requestPath
	}

	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}

	httpReq := &http.Request{
		Method: method,
		URL:    &url.URL{Path: requestPath},
		Header: make(http.Header),
	}

	matched, ok := engine.Match(httpReq)
	if !ok {
		return &SimulateResponse{
			Matched: false,
			Allowed: false,
		}, nil
	}

	resp := &SimulateResponse{
		Matched:        true,
		Allowed:        true,
		IdentityNeeded: matched.RequireAuth,
		RewrittenPath:  matched.ApplyRewrite(requestPath),
	}

	if route, err := s.repo.GetRouteByID(ctx, matched.ID); err == nil {
		resp.Rule = route
	}
	if upstream, ok := upstreamByName[strings.ToLower(strings.TrimSpace(matched.Target))]; ok {
		copyUpstream := upstream
		resp.Upstream = &copyUpstream
	}

	var simulatedSession *session.Session
	if req.Authenticated {
		aal := session.AAL(strings.ToLower(strings.TrimSpace(req.AAL)))
		if aal == "" {
			aal = session.AAL1
		}
		simulatedSession = &session.Session{
			ID:         uuid.New(),
			IdentityID: uuid.New(),
			AAL:        aal,
		}
	}

	if accessErr := engine.CheckAccess(httpReq, matched, simulatedSession); accessErr != nil {
		resp.Allowed = false
		resp.DenialReason = accessErr.Error()
	}
	return resp, nil
}

func (s *Service) normalizeRoute(ctx context.Context, route store.Route) (store.Route, error) {
	route.ID = strings.TrimSpace(route.ID)
	if route.ID == "" {
		route.ID = uuid.NewString()
	}
	route.Path = strings.TrimSpace(route.Path)
	if route.Path == "" {
		return store.Route{}, ErrInvalidProxyConfig
	}
	if !strings.HasPrefix(route.Path, "/") {
		route.Path = "/" + route.Path
	}
	route.Target = strings.ToLower(strings.TrimSpace(route.Target))
	if route.Target == "" {
		return store.Route{}, ErrInvalidProxyConfig
	}
	if _, err := s.repo.GetUpstreamByName(ctx, route.Target); err != nil {
		return store.Route{}, err
	}

	route.Methods = normalizeStringList(route.Methods, strings.ToUpper)
	route.Capabilities = normalizeStringList(route.Capabilities, strings.ToLower)
	route.Headers = normalizeHeaderMap(route.Headers)
	route.Description = strings.TrimSpace(route.Description)
	route.RequiredAAL = strings.ToLower(strings.TrimSpace(route.RequiredAAL))
	switch route.RequiredAAL {
	case "", string(session.AAL0), string(session.AAL1), string(session.AAL2):
	default:
		return store.Route{}, ErrInvalidProxyConfig
	}
	if route.Rewrite != nil {
		trimmed := &store.Rewrite{
			StripPrefix: strings.TrimSpace(route.Rewrite.StripPrefix),
			AddPrefix:   strings.TrimSpace(route.Rewrite.AddPrefix),
		}
		if trimmed.StripPrefix == "" && trimmed.AddPrefix == "" {
			route.Rewrite = nil
		} else {
			route.Rewrite = trimmed
		}
	}
	if route.RateLimit != nil {
		if route.RateLimit.RequestsPerSecond <= 0 && route.RateLimit.BurstSize <= 0 {
			route.RateLimit = nil
		} else {
			if route.RateLimit.RequestsPerSecond <= 0 || route.RateLimit.BurstSize <= 0 {
				return store.Route{}, ErrInvalidProxyConfig
			}
		}
	}
	if route.CreatedAt.IsZero() {
		route.CreatedAt = s.now()
	}
	route.UpdatedAt = s.now()

	if _, err := toCoreRule(route); err != nil {
		return store.Route{}, err
	}
	return route, nil
}

func normalizeUpstream(upstream store.Upstream) (store.Upstream, error) {
	now := time.Now().UTC()
	upstream.Name = strings.ToLower(strings.TrimSpace(upstream.Name))
	if upstream.Name == "" {
		return store.Upstream{}, ErrInvalidProxyConfig
	}
	upstream.URL = strings.TrimSpace(upstream.URL)
	if upstream.URL == "" {
		return store.Upstream{}, ErrInvalidProxyConfig
	}
	parsed, err := url.Parse(upstream.URL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return store.Upstream{}, ErrInvalidProxyConfig
	}
	upstream.URL = strings.TrimRight(upstream.URL, "/")
	upstream.HealthCheck = strings.TrimSpace(upstream.HealthCheck)
	upstream.HealthCheckExpectedBody = strings.TrimSpace(upstream.HealthCheckExpectedBody)
	if upstream.HealthCheck == "" {
		upstream.HealthCheck = "/health"
	}
	if !strings.HasPrefix(upstream.HealthCheck, "/") {
		upstream.HealthCheck = "/" + upstream.HealthCheck
	}
	upstream.Timeout = strings.TrimSpace(upstream.Timeout)
	if upstream.Timeout != "" {
		if _, err := time.ParseDuration(upstream.Timeout); err != nil {
			return store.Upstream{}, ErrInvalidProxyConfig
		}
	}
	upstream.Headers = normalizeHeaderMap(upstream.Headers)
	if upstream.CircuitBreaker != nil {
		trimmed := &store.CircuitBreaker{
			FailureThreshold: upstream.CircuitBreaker.FailureThreshold,
			Timeout:          strings.TrimSpace(upstream.CircuitBreaker.Timeout),
			SuccessThreshold: upstream.CircuitBreaker.SuccessThreshold,
		}
		if trimmed.Timeout != "" {
			if _, err := time.ParseDuration(trimmed.Timeout); err != nil {
				return store.Upstream{}, ErrInvalidProxyConfig
			}
		}
		if trimmed.FailureThreshold == 0 && trimmed.Timeout == "" && trimmed.SuccessThreshold == 0 {
			upstream.CircuitBreaker = nil
		} else {
			upstream.CircuitBreaker = trimmed
		}
	}
	if upstream.CreatedAt.IsZero() {
		upstream.CreatedAt = now
	}
	upstream.UpdatedAt = now
	return upstream, nil
}

func buildRuleEngine(upstreams []store.Upstream, routes []store.Route) (*coreproxy.RuleEngine, map[string]store.Upstream, error) {
	upstreamByName := make(map[string]store.Upstream, len(upstreams))
	for _, upstream := range upstreams {
		upstreamByName[strings.ToLower(strings.TrimSpace(upstream.Name))] = upstream
	}

	coreRules := make([]coreproxy.Rule, 0, len(routes))
	for _, route := range routes {
		if _, ok := upstreamByName[strings.ToLower(strings.TrimSpace(route.Target))]; !ok {
			return nil, nil, ErrInvalidProxyConfig
		}
		coreRule, err := toCoreRule(route)
		if err != nil {
			return nil, nil, err
		}
		coreRules = append(coreRules, coreRule)
	}
	return coreproxy.NewRuleEngine(coreRules), upstreamByName, nil
}

func toCoreRule(route store.Route) (coreproxy.Rule, error) {
	coreRule := coreproxy.Rule{
		ID:           route.ID,
		Path:         route.Path,
		Methods:      append([]string(nil), route.Methods...),
		RequireAuth:  route.RequireAuth,
		RequiredAAL:  session.AAL(route.RequiredAAL),
		Capabilities: append([]string(nil), route.Capabilities...),
		Target:       route.Target,
		Priority:     route.Priority,
		Headers:      normalizeHeaderMap(route.Headers),
		Enabled:      route.Enabled,
		Description:  route.Description,
	}
	if route.RateLimit != nil {
		coreRule.RateLimit = &coreproxy.RateLimitConfig{
			RequestsPerSecond: route.RateLimit.RequestsPerSecond,
			BurstSize:         route.RateLimit.BurstSize,
			ByIP:              route.RateLimit.ByIP,
			ByUser:            route.RateLimit.ByUser,
			ByPath:            route.RateLimit.ByPath,
		}
	}
	if route.Rewrite != nil {
		coreRule.Rewrite = &coreproxy.RewriteConfig{
			StripPrefix: route.Rewrite.StripPrefix,
			AddPrefix:   route.Rewrite.AddPrefix,
		}
	}
	if err := coreRule.Validate(); err != nil {
		return coreproxy.Rule{}, ErrInvalidProxyConfig
	}
	return coreRule, nil
}

func normalizeHeaderMap(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return map[string]string{}
	}
	keys := make([]string, 0, len(headers))
	normalized := make(map[string]string, len(headers))
	for key, value := range headers {
		trimmedKey := http.CanonicalHeaderKey(strings.TrimSpace(key))
		trimmedValue := strings.TrimSpace(value)
		if trimmedKey == "" || trimmedValue == "" {
			continue
		}
		if _, ok := normalized[trimmedKey]; !ok {
			keys = append(keys, trimmedKey)
		}
		normalized[trimmedKey] = trimmedValue
	}
	sort.Strings(keys)
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		out[key] = normalized[key]
	}
	return out
}

func normalizeStringList(values []string, transformer func(string) string) []string {
	if len(values) == 0 {
		return []string{}
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if transformer != nil {
			trimmed = transformer(trimmed)
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}
