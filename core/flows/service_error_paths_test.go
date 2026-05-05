package flows

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

type errFlowStore struct {
	createErr    error
	getErr       error
	getByCSRFErr error
	updateErr    error
	flow         *Flow
}

func (s *errFlowStore) Create(context.Context, *Flow) error { return s.createErr }
func (s *errFlowStore) Get(context.Context, uuid.UUID) (*Flow, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	if s.flow != nil {
		return s.flow, nil
	}
	return nil, ErrFlowNotFound
}
func (s *errFlowStore) GetByCSRF(context.Context, string) (*Flow, error) {
	if s.getByCSRFErr != nil {
		return nil, s.getByCSRFErr
	}
	if s.flow != nil {
		return s.flow, nil
	}
	return nil, ErrFlowNotFound
}
func (s *errFlowStore) Update(context.Context, *Flow) error { return s.updateErr }
func (s *errFlowStore) Delete(context.Context, uuid.UUID) error {
	return nil
}
func (s *errFlowStore) DeleteExpired(context.Context) (int64, error) { return 0, nil }
func (s *errFlowStore) ListByIdentity(context.Context, uuid.UUID, FlowType) ([]*Flow, error) {
	return nil, nil
}

func TestService_CreateFlowErrorPaths(t *testing.T) {
	t.Run("store create errors are returned by all create methods", func(t *testing.T) {
		wantErr := errors.New("store create failed")
		svc := NewService(&errFlowStore{createErr: wantErr}, DefaultConfig())
		ctx := context.Background()
		identityID := uuid.New()

		if _, err := svc.CreateLoginFlow(ctx, "/login"); !errors.Is(err, wantErr) {
			t.Fatalf("expected CreateLoginFlow to return store error, got %v", err)
		}
		if _, err := svc.CreateRegistrationFlow(ctx, "/registration"); !errors.Is(err, wantErr) {
			t.Fatalf("expected CreateRegistrationFlow to return store error, got %v", err)
		}
		if _, err := svc.CreateRecoveryFlow(ctx, "/recovery"); !errors.Is(err, wantErr) {
			t.Fatalf("expected CreateRecoveryFlow to return store error, got %v", err)
		}
		if _, err := svc.CreateSettingsFlow(ctx, "/settings", identityID, uuid.New()); !errors.Is(err, wantErr) {
			t.Fatalf("expected CreateSettingsFlow to return store error, got %v", err)
		}
		if _, err := svc.CreateVerificationFlow(ctx, "/verification", &identityID); !errors.Is(err, wantErr) {
			t.Fatalf("expected CreateVerificationFlow to return store error, got %v", err)
		}
	})
}

func TestService_MutationAndLookupErrorPaths(t *testing.T) {
	ctx := context.Background()
	flow, err := NewFlow(TypeLogin, "/login", DefaultTTL)
	if err != nil {
		t.Fatalf("failed to create flow fixture: %v", err)
	}

	t.Run("validate flow returns store get error", func(t *testing.T) {
		wantErr := errors.New("db read failed")
		svc := NewService(&errFlowStore{getErr: wantErr}, DefaultConfig())
		if _, err := svc.ValidateFlow(ctx, uuid.New(), "csrf"); !errors.Is(err, wantErr) {
			t.Fatalf("expected ValidateFlow to return store error, got %v", err)
		}
	})

	t.Run("complete flow returns terminal state error", func(t *testing.T) {
		completed := *flow
		completed.State = StateCompleted
		svc := NewService(&errFlowStore{flow: &completed}, DefaultConfig())
		err := svc.CompleteFlow(ctx, completed.ID)
		if !errors.Is(err, ErrFlowCompleted) {
			t.Fatalf("expected ErrFlowCompleted, got %v", err)
		}
	})

	t.Run("fail flow returns store get error", func(t *testing.T) {
		wantErr := errors.New("lookup failed")
		svc := NewService(&errFlowStore{getErr: wantErr}, DefaultConfig())
		if err := svc.FailFlow(ctx, uuid.New(), "boom"); !errors.Is(err, wantErr) {
			t.Fatalf("expected FailFlow get error, got %v", err)
		}
	})

	t.Run("update flow ui returns store get error", func(t *testing.T) {
		wantErr := errors.New("lookup failed")
		svc := NewService(&errFlowStore{getErr: wantErr}, DefaultConfig())
		if err := svc.UpdateFlowUI(ctx, uuid.New(), &UIState{}); !errors.Is(err, wantErr) {
			t.Fatalf("expected UpdateFlowUI get error, got %v", err)
		}
	})

	t.Run("add flow message returns store get error", func(t *testing.T) {
		wantErr := errors.New("lookup failed")
		svc := NewService(&errFlowStore{getErr: wantErr}, DefaultConfig())
		if err := svc.AddFlowMessage(ctx, uuid.New(), Msg{}); !errors.Is(err, wantErr) {
			t.Fatalf("expected AddFlowMessage get error, got %v", err)
		}
	})
}
