package store

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	ErrUpstreamNotFound = errors.New("proxy upstream not found")
	ErrRouteNotFound    = errors.New("proxy route not found")
	ErrUpstreamInUse    = errors.New("proxy upstream is referenced by existing routes")
)

type CircuitBreaker struct {
	FailureThreshold int    `json:"failure_threshold,omitempty"`
	Timeout          string `json:"timeout,omitempty"`
	SuccessThreshold int    `json:"success_threshold,omitempty"`
}

type Upstream struct {
	ID                      uuid.UUID         `json:"id"`
	Name                    string            `json:"name"`
	URL                     string            `json:"url"`
	HealthCheck             string            `json:"health_check,omitempty"`
	HealthCheckExpectedBody string            `json:"health_check_expected_body,omitempty"`
	Timeout                 string            `json:"timeout,omitempty"`
	MaxConnections          int               `json:"max_connections,omitempty"`
	Headers                 map[string]string `json:"headers,omitempty"`
	CircuitBreaker          *CircuitBreaker   `json:"circuit_breaker,omitempty"`
	Enabled                 bool              `json:"enabled"`
	CreatedAt               time.Time         `json:"created_at"`
	UpdatedAt               time.Time         `json:"updated_at"`
}

type Rewrite struct {
	StripPrefix string `json:"strip_prefix,omitempty"`
	AddPrefix   string `json:"add_prefix,omitempty"`
}

type RateLimit struct {
	RequestsPerSecond int  `json:"requests_per_second,omitempty"`
	BurstSize         int  `json:"burst_size,omitempty"`
	ByIP              bool `json:"by_ip,omitempty"`
	ByUser            bool `json:"by_user,omitempty"`
	ByPath            bool `json:"by_path,omitempty"`
}

type Route struct {
	ID           string            `json:"id"`
	Path         string            `json:"path"`
	Methods      []string          `json:"methods,omitempty"`
	RequireAuth  bool              `json:"require_auth"`
	RequiredAAL  string            `json:"required_aal,omitempty"`
	Capabilities []string          `json:"capabilities,omitempty"`
	RateLimit    *RateLimit        `json:"rate_limit,omitempty"`
	Target       string            `json:"target"`
	Priority     int               `json:"priority"`
	Headers      map[string]string `json:"headers,omitempty"`
	Rewrite      *Rewrite          `json:"rewrite,omitempty"`
	Enabled      bool              `json:"enabled"`
	Description  string            `json:"description,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

type Repository interface {
	ListUpstreams(ctx context.Context) ([]Upstream, error)
	GetUpstreamByName(ctx context.Context, name string) (*Upstream, error)
	UpsertUpstream(ctx context.Context, upstream Upstream) (*Upstream, error)
	DeleteUpstream(ctx context.Context, name string) error
	ListRoutes(ctx context.Context) ([]Route, error)
	GetRouteByID(ctx context.Context, id string) (*Route, error)
	UpsertRoute(ctx context.Context, route Route) (*Route, error)
	DeleteRoute(ctx context.Context, id string) error
}

type MemoryStore struct {
	mu        sync.RWMutex
	upstreams map[string]Upstream
	routes    map[string]Route
}

func New() *MemoryStore {
	return &MemoryStore{
		upstreams: make(map[string]Upstream),
		routes:    make(map[string]Route),
	}
}

func (s *MemoryStore) ListUpstreams(_ context.Context) ([]Upstream, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Upstream, 0, len(s.upstreams))
	for _, upstream := range s.upstreams {
		out = append(out, cloneUpstream(upstream))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (s *MemoryStore) GetUpstreamByName(_ context.Context, name string) (*Upstream, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	upstream, ok := s.upstreams[normalizeName(name)]
	if !ok {
		return nil, ErrUpstreamNotFound
	}
	cloned := cloneUpstream(upstream)
	return &cloned, nil
}

func (s *MemoryStore) UpsertUpstream(_ context.Context, upstream Upstream) (*Upstream, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := normalizeName(upstream.Name)
	if key == "" {
		return nil, ErrUpstreamNotFound
	}
	if existing, ok := s.upstreams[key]; ok {
		upstream.CreatedAt = existing.CreatedAt
		if upstream.ID == uuid.Nil {
			upstream.ID = existing.ID
		}
	}
	if upstream.ID == uuid.Nil {
		upstream.ID = uuid.New()
	}
	s.upstreams[key] = cloneUpstream(upstream)
	cloned := cloneUpstream(s.upstreams[key])
	return &cloned, nil
}

func (s *MemoryStore) DeleteUpstream(_ context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := normalizeName(name)
	if _, ok := s.upstreams[key]; !ok {
		return ErrUpstreamNotFound
	}
	for _, route := range s.routes {
		if normalizeName(route.Target) == key {
			return ErrUpstreamInUse
		}
	}
	delete(s.upstreams, key)
	return nil
}

func (s *MemoryStore) ListRoutes(_ context.Context) ([]Route, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Route, 0, len(s.routes))
	for _, route := range s.routes {
		out = append(out, cloneRoute(route))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Priority == out[j].Priority {
			return out[i].ID < out[j].ID
		}
		return out[i].Priority > out[j].Priority
	})
	return out, nil
}

func (s *MemoryStore) GetRouteByID(_ context.Context, id string) (*Route, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	route, ok := s.routes[strings.TrimSpace(id)]
	if !ok {
		return nil, ErrRouteNotFound
	}
	cloned := cloneRoute(route)
	return &cloned, nil
}

func (s *MemoryStore) UpsertRoute(_ context.Context, route Route) (*Route, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := strings.TrimSpace(route.ID)
	if id == "" {
		id = uuid.NewString()
		route.ID = id
	}
	if existing, ok := s.routes[id]; ok {
		route.CreatedAt = existing.CreatedAt
	}
	s.routes[id] = cloneRoute(route)
	cloned := cloneRoute(s.routes[id])
	return &cloned, nil
}

func (s *MemoryStore) DeleteRoute(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	id = strings.TrimSpace(id)
	if _, ok := s.routes[id]; !ok {
		return ErrRouteNotFound
	}
	delete(s.routes, id)
	return nil
}

func cloneUpstream(in Upstream) Upstream {
	out := in
	out.Headers = cloneStringMap(in.Headers)
	if in.CircuitBreaker != nil {
		copyCB := *in.CircuitBreaker
		out.CircuitBreaker = &copyCB
	}
	return out
}

func cloneRoute(in Route) Route {
	out := in
	out.Methods = append([]string(nil), in.Methods...)
	out.Capabilities = append([]string(nil), in.Capabilities...)
	out.Headers = cloneStringMap(in.Headers)
	if in.RateLimit != nil {
		copyRate := *in.RateLimit
		out.RateLimit = &copyRate
	}
	if in.Rewrite != nil {
		copyRewrite := *in.Rewrite
		out.Rewrite = &copyRewrite
	}
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func normalizeName(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
