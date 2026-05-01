package store

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

var ErrRunNotFound = errors.New("cli command run not found")

type CommandRun struct {
	ID         string         `json:"id"`
	Command    string         `json:"command"`
	Arguments  map[string]any `json:"arguments,omitempty"`
	Result     map[string]any `json:"result,omitempty"`
	Success    bool           `json:"success"`
	Error      string         `json:"error,omitempty"`
	ExecutedAt time.Time      `json:"executed_at"`
}

type Repository interface {
	SaveRun(ctx context.Context, run CommandRun) error
	ListRuns(ctx context.Context, limit int) ([]CommandRun, error)
	GetRun(ctx context.Context, id string) (*CommandRun, error)
	StatusSummary(ctx context.Context) (map[string]any, error)
	RuntimeConfig(ctx context.Context) (map[string]any, error)
	CourierSummary(ctx context.Context) (map[string]any, error)
}

type MemoryStore struct {
	mu   sync.RWMutex
	runs map[string]CommandRun
}

func New() *MemoryStore {
	return &MemoryStore{runs: make(map[string]CommandRun)}
}

func (s *MemoryStore) SaveRun(_ context.Context, run CommandRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs[run.ID] = cloneRun(run)
	return nil
}

func (s *MemoryStore) ListRuns(_ context.Context, limit int) ([]CommandRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	runs := make([]CommandRun, 0, len(s.runs))
	for _, run := range s.runs {
		runs = append(runs, cloneRun(run))
	}
	sort.Slice(runs, func(i, j int) bool {
		return runs[i].ExecutedAt.After(runs[j].ExecutedAt)
	})
	if limit > 0 && len(runs) > limit {
		runs = runs[:limit]
	}
	return runs, nil
}

func (s *MemoryStore) GetRun(_ context.Context, id string) (*CommandRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	run, ok := s.runs[strings.TrimSpace(id)]
	if !ok {
		return nil, ErrRunNotFound
	}
	cloned := cloneRun(run)
	return &cloned, nil
}

func (s *MemoryStore) StatusSummary(context.Context) (map[string]any, error) {
	return map[string]any{
		"database":            "unavailable",
		"identities_total":    0,
		"sessions_active":     0,
		"oauth2_clients":      0,
		"social_providers":    0,
		"proxy_upstreams":     0,
		"proxy_routes":        0,
		"recent_command_runs": len(s.runs),
	}, nil
}

func (s *MemoryStore) RuntimeConfig(context.Context) (map[string]any, error) {
	return map[string]any{
		"database": "unavailable",
		"policy":   map[string]any{},
		"proxy":    map[string]any{},
	}, nil
}

func (s *MemoryStore) CourierSummary(context.Context) (map[string]any, error) {
	return map[string]any{
		"database":      "unavailable",
		"queued":        0,
		"processing":    0,
		"delivered":     0,
		"failed":        0,
		"cancelled":     0,
		"retriable":     0,
		"last_activity": "",
	}, nil
}

func cloneRun(in CommandRun) CommandRun {
	out := in
	out.Arguments = cloneMap(in.Arguments)
	out.Result = cloneMap(in.Result)
	return out
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
