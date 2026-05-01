package service

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/aegion/aegion/modules/cli/store"
	"github.com/google/uuid"
)

var ErrUnsupportedCommand = errors.New("unsupported cli command")

type CommandDescriptor struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	ReadOnly    bool     `json:"read_only"`
	Arguments   []string `json:"arguments,omitempty"`
}

type ExecuteRequest struct {
	Command   string         `json:"command"`
	Arguments map[string]any `json:"arguments,omitempty"`
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

func (s *Service) Commands() []CommandDescriptor {
	commands := []CommandDescriptor{
		{Name: "status.summary", Description: "Summarize platform entity counts and operator-facing module inventory", Category: "status", ReadOnly: true},
		{Name: "runtime.config", Description: "Inspect persisted runtime policy and proxy configuration", Category: "config", ReadOnly: true},
		{Name: "courier.summary", Description: "Inspect courier queue and retry health", Category: "courier", ReadOnly: true},
	}
	sort.Slice(commands, func(i, j int) bool { return commands[i].Name < commands[j].Name })
	return commands
}

func (s *Service) Execute(ctx context.Context, req ExecuteRequest) (*store.CommandRun, error) {
	command := strings.TrimSpace(req.Command)
	if command == "" {
		return nil, ErrUnsupportedCommand
	}
	run := store.CommandRun{
		ID:         uuid.NewString(),
		Command:    command,
		Arguments:  cloneMap(req.Arguments),
		Success:    true,
		ExecutedAt: s.now(),
		Result:     map[string]any{},
	}

	var err error
	switch command {
	case "status.summary":
		run.Result, err = s.repo.StatusSummary(ctx)
	case "runtime.config":
		run.Result, err = s.repo.RuntimeConfig(ctx)
	case "courier.summary":
		run.Result, err = s.repo.CourierSummary(ctx)
	default:
		err = ErrUnsupportedCommand
	}
	if err != nil {
		run.Success = false
		run.Error = err.Error()
		_ = s.repo.SaveRun(ctx, run)
		return &run, err
	}
	if saveErr := s.repo.SaveRun(ctx, run); saveErr != nil {
		return nil, saveErr
	}
	return &run, nil
}

func (s *Service) ListRuns(ctx context.Context, limit int) ([]store.CommandRun, error) {
	return s.repo.ListRuns(ctx, limit)
}

func (s *Service) GetRun(ctx context.Context, id string) (*store.CommandRun, error) {
	return s.repo.GetRun(ctx, id)
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
