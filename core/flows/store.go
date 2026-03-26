package flows

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// FlowStore defines the interface for flow persistence
type FlowStore interface {
	Create(ctx context.Context, flow *Flow) error
	Get(ctx context.Context, id uuid.UUID) (*Flow, error)
	GetByCSRF(ctx context.Context, csrfToken string) (*Flow, error)
	Update(ctx context.Context, flow *Flow) error
	Delete(ctx context.Context, id uuid.UUID) error
	DeleteExpired(ctx context.Context) (int64, error)
	ListByIdentity(ctx context.Context, identityID uuid.UUID, flowType FlowType) ([]*Flow, error)
}

// PostgresFlowStore implements FlowStore using PostgreSQL
type PostgresFlowStore struct {
	db *pgxpool.Pool
}

// NewPostgresFlowStore creates a new PostgreSQL-backed flow store
func NewPostgresFlowStore(db *pgxpool.Pool) *PostgresFlowStore {
	return &PostgresFlowStore{db: db}
}

// Create inserts a new flow into the database
func (s *PostgresFlowStore) Create(ctx context.Context, flow *Flow) error {
	ui, err := json.Marshal(flow.UI)
	if err != nil {
		return err
	}

	flowCtx, err := json.Marshal(flow.Context)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(ctx, `
		INSERT INTO core_flows (
			id, type, state, identity_id, session_id, request_url, return_to,
			ui, context, issued_at, expires_at, csrf_token, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`,
		flow.ID, flow.Type, flow.State, flow.IdentityID, flow.SessionID,
		flow.RequestURL, flow.ReturnTo, ui, flowCtx, flow.IssuedAt,
		flow.ExpiresAt, flow.CSRFToken, flow.CreatedAt, flow.UpdatedAt,
	)

	return err
}

// Get retrieves a flow by ID
func (s *PostgresFlowStore) Get(ctx context.Context, id uuid.UUID) (*Flow, error) {
	flow := &Flow{}
	var ui, flowCtx []byte

	err := s.db.QueryRow(ctx, `
		SELECT id, type, state, identity_id, session_id, request_url, return_to,
			ui, context, issued_at, expires_at, csrf_token, created_at, updated_at
		FROM core_flows
		WHERE id = $1
	`, id).Scan(
		&flow.ID, &flow.Type, &flow.State, &flow.IdentityID, &flow.SessionID,
		&flow.RequestURL, &flow.ReturnTo, &ui, &flowCtx, &flow.IssuedAt,
		&flow.ExpiresAt, &flow.CSRFToken, &flow.CreatedAt, &flow.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrFlowNotFound
		}
		return nil, err
	}

	if err := json.Unmarshal(ui, &flow.UI); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(flowCtx, &flow.Context); err != nil {
		return nil, err
	}

	return flow, nil
}

// GetByCSRF retrieves a flow by its CSRF token
func (s *PostgresFlowStore) GetByCSRF(ctx context.Context, csrfToken string) (*Flow, error) {
	flow := &Flow{}
	var ui, flowCtx []byte

	err := s.db.QueryRow(ctx, `
		SELECT id, type, state, identity_id, session_id, request_url, return_to,
			ui, context, issued_at, expires_at, csrf_token, created_at, updated_at
		FROM core_flows
		WHERE csrf_token = $1 AND state = 'active'
	`, csrfToken).Scan(
		&flow.ID, &flow.Type, &flow.State, &flow.IdentityID, &flow.SessionID,
		&flow.RequestURL, &flow.ReturnTo, &ui, &flowCtx, &flow.IssuedAt,
		&flow.ExpiresAt, &flow.CSRFToken, &flow.CreatedAt, &flow.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrFlowNotFound
		}
		return nil, err
	}

	if err := json.Unmarshal(ui, &flow.UI); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(flowCtx, &flow.Context); err != nil {
		return nil, err
	}

	return flow, nil
}

// Update updates an existing flow in the database
func (s *PostgresFlowStore) Update(ctx context.Context, flow *Flow) error {
	ui, err := json.Marshal(flow.UI)
	if err != nil {
		return err
	}

	flowCtx, err := json.Marshal(flow.Context)
	if err != nil {
		return err
	}

	flow.UpdatedAt = time.Now().UTC()

	result, err := s.db.Exec(ctx, `
		UPDATE core_flows
		SET type = $2, state = $3, identity_id = $4, session_id = $5,
			request_url = $6, return_to = $7, ui = $8, context = $9,
			expires_at = $10, updated_at = $11
		WHERE id = $1
	`,
		flow.ID, flow.Type, flow.State, flow.IdentityID, flow.SessionID,
		flow.RequestURL, flow.ReturnTo, ui, flowCtx, flow.ExpiresAt, flow.UpdatedAt,
	)

	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return ErrFlowNotFound
	}

	return nil
}

// Delete removes a flow from the database
func (s *PostgresFlowStore) Delete(ctx context.Context, id uuid.UUID) error {
	result, err := s.db.Exec(ctx, `DELETE FROM core_flows WHERE id = $1`, id)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return ErrFlowNotFound
	}

	return nil
}

// DeleteExpired removes all expired flows from the database
func (s *PostgresFlowStore) DeleteExpired(ctx context.Context) (int64, error) {
	result, err := s.db.Exec(ctx, `
		DELETE FROM core_flows
		WHERE expires_at < NOW() OR state IN ('completed', 'failed', 'expired')
	`)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected(), nil
}

// ListByIdentity retrieves all flows for a given identity and type
func (s *PostgresFlowStore) ListByIdentity(ctx context.Context, identityID uuid.UUID, flowType FlowType) ([]*Flow, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, type, state, identity_id, session_id, request_url, return_to,
			ui, context, issued_at, expires_at, csrf_token, created_at, updated_at
		FROM core_flows
		WHERE identity_id = $1 AND type = $2 AND state = 'active'
		ORDER BY created_at DESC
	`, identityID, flowType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var flows []*Flow
	for rows.Next() {
		flow := &Flow{}
		var ui, flowCtx []byte

		if err := rows.Scan(
			&flow.ID, &flow.Type, &flow.State, &flow.IdentityID, &flow.SessionID,
			&flow.RequestURL, &flow.ReturnTo, &ui, &flowCtx, &flow.IssuedAt,
			&flow.ExpiresAt, &flow.CSRFToken, &flow.CreatedAt, &flow.UpdatedAt,
		); err != nil {
			return nil, err
		}

		if err := json.Unmarshal(ui, &flow.UI); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(flowCtx, &flow.Context); err != nil {
			return nil, err
		}

		flows = append(flows, flow)
	}

	return flows, rows.Err()
}
