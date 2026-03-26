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

// Default continuity container TTL
const DefaultContinuityTTL = 15 * time.Minute

// Continuity errors
var (
	ErrContainerNotFound = errors.New("continuity container not found")
	ErrContainerExpired  = errors.New("continuity container has expired")
)

// ContinuityContainer holds state that needs to persist across redirects
type ContinuityContainer struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	IdentityID *uuid.UUID `json:"identity_id,omitempty"`
	SessionID  *uuid.UUID `json:"session_id,omitempty"`
	Payload    Payload    `json:"payload"`
	ExpiresAt  time.Time  `json:"expires_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

// Payload holds arbitrary data for the continuity container
type Payload map[string]any

// ContinuityStore defines the interface for continuity container persistence
type ContinuityStore interface {
	Store(ctx context.Context, container *ContinuityContainer) error
	Retrieve(ctx context.Context, id uuid.UUID) (*ContinuityContainer, error)
	RetrieveByName(ctx context.Context, name string, identityID uuid.UUID) (*ContinuityContainer, error)
	Delete(ctx context.Context, id uuid.UUID) error
	DeleteExpired(ctx context.Context) (int64, error)
}

// PostgresContinuityStore implements ContinuityStore using PostgreSQL
type PostgresContinuityStore struct {
	db *pgxpool.Pool
}

// NewPostgresContinuityStore creates a new PostgreSQL-backed continuity store
func NewPostgresContinuityStore(db *pgxpool.Pool) *PostgresContinuityStore {
	return &PostgresContinuityStore{db: db}
}

// NewContinuityContainer creates a new continuity container
func NewContinuityContainer(name string, ttl time.Duration) *ContinuityContainer {
	if ttl <= 0 {
		ttl = DefaultContinuityTTL
	}

	now := time.Now().UTC()
	return &ContinuityContainer{
		ID:        uuid.New(),
		Name:      name,
		Payload:   make(Payload),
		ExpiresAt: now.Add(ttl),
		CreatedAt: now,
	}
}

// IsExpired returns true if the container has expired
func (c *ContinuityContainer) IsExpired() bool {
	return time.Now().UTC().After(c.ExpiresAt)
}

// SetIdentity sets the identity ID for the container
func (c *ContinuityContainer) SetIdentity(identityID uuid.UUID) {
	c.IdentityID = &identityID
}

// SetSession sets the session ID for the container
func (c *ContinuityContainer) SetSession(sessionID uuid.UUID) {
	c.SessionID = &sessionID
}

// Set adds a key-value pair to the payload
func (c *ContinuityContainer) Set(key string, value any) {
	c.Payload[key] = value
}

// Get retrieves a value from the payload
func (c *ContinuityContainer) Get(key string) (any, bool) {
	v, ok := c.Payload[key]
	return v, ok
}

// GetString retrieves a string value from the payload
func (c *ContinuityContainer) GetString(key string) string {
	if v, ok := c.Payload[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// GetUUID retrieves a UUID value from the payload
func (c *ContinuityContainer) GetUUID(key string) (uuid.UUID, bool) {
	if v, ok := c.Payload[key]; ok {
		switch val := v.(type) {
		case uuid.UUID:
			return val, true
		case string:
			if id, err := uuid.Parse(val); err == nil {
				return id, true
			}
		}
	}
	return uuid.Nil, false
}

// Store persists a continuity container to the database
func (s *PostgresContinuityStore) Store(ctx context.Context, container *ContinuityContainer) error {
	payload, err := json.Marshal(container.Payload)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(ctx, `
		INSERT INTO core_continuity_containers (
			id, name, identity_id, session_id, payload, expires_at, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`,
		container.ID, container.Name, container.IdentityID, container.SessionID,
		payload, container.ExpiresAt, container.CreatedAt,
	)

	return err
}

// Retrieve fetches a continuity container by ID
func (s *PostgresContinuityStore) Retrieve(ctx context.Context, id uuid.UUID) (*ContinuityContainer, error) {
	container := &ContinuityContainer{}
	var payload []byte

	err := s.db.QueryRow(ctx, `
		SELECT id, name, identity_id, session_id, payload, expires_at, created_at
		FROM core_continuity_containers
		WHERE id = $1
	`, id).Scan(
		&container.ID, &container.Name, &container.IdentityID, &container.SessionID,
		&payload, &container.ExpiresAt, &container.CreatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrContainerNotFound
		}
		return nil, err
	}

	if err := json.Unmarshal(payload, &container.Payload); err != nil {
		return nil, err
	}

	if container.IsExpired() {
		return nil, ErrContainerExpired
	}

	return container, nil
}

// RetrieveByName fetches a continuity container by name and identity ID
func (s *PostgresContinuityStore) RetrieveByName(ctx context.Context, name string, identityID uuid.UUID) (*ContinuityContainer, error) {
	container := &ContinuityContainer{}
	var payload []byte

	err := s.db.QueryRow(ctx, `
		SELECT id, name, identity_id, session_id, payload, expires_at, created_at
		FROM core_continuity_containers
		WHERE name = $1 AND identity_id = $2 AND expires_at > NOW()
		ORDER BY created_at DESC
		LIMIT 1
	`, name, identityID).Scan(
		&container.ID, &container.Name, &container.IdentityID, &container.SessionID,
		&payload, &container.ExpiresAt, &container.CreatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrContainerNotFound
		}
		return nil, err
	}

	if err := json.Unmarshal(payload, &container.Payload); err != nil {
		return nil, err
	}

	return container, nil
}

// Delete removes a continuity container
func (s *PostgresContinuityStore) Delete(ctx context.Context, id uuid.UUID) error {
	result, err := s.db.Exec(ctx, `DELETE FROM core_continuity_containers WHERE id = $1`, id)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return ErrContainerNotFound
	}

	return nil
}

// DeleteExpired removes all expired continuity containers
func (s *PostgresContinuityStore) DeleteExpired(ctx context.Context) (int64, error) {
	result, err := s.db.Exec(ctx, `
		DELETE FROM core_continuity_containers
		WHERE expires_at < NOW()
	`)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected(), nil
}

// ContinuityManager provides high-level continuity operations
type ContinuityManager struct {
	store ContinuityStore
	ttl   time.Duration
}

// NewContinuityManager creates a new continuity manager
func NewContinuityManager(store ContinuityStore, ttl time.Duration) *ContinuityManager {
	if ttl <= 0 {
		ttl = DefaultContinuityTTL
	}
	return &ContinuityManager{
		store: store,
		ttl:   ttl,
	}
}

// Create creates and stores a new continuity container
func (m *ContinuityManager) Create(ctx context.Context, name string, identityID *uuid.UUID, payload Payload) (*ContinuityContainer, error) {
	container := NewContinuityContainer(name, m.ttl)

	if identityID != nil {
		container.SetIdentity(*identityID)
	}

	for k, v := range payload {
		container.Set(k, v)
	}

	if err := m.store.Store(ctx, container); err != nil {
		return nil, err
	}

	return container, nil
}

// Get retrieves a continuity container by ID
func (m *ContinuityManager) Get(ctx context.Context, id uuid.UUID) (*ContinuityContainer, error) {
	return m.store.Retrieve(ctx, id)
}

// GetByName retrieves a continuity container by name and identity
func (m *ContinuityManager) GetByName(ctx context.Context, name string, identityID uuid.UUID) (*ContinuityContainer, error) {
	return m.store.RetrieveByName(ctx, name, identityID)
}

// Consume retrieves and deletes a continuity container (one-time use)
func (m *ContinuityManager) Consume(ctx context.Context, id uuid.UUID) (*ContinuityContainer, error) {
	container, err := m.store.Retrieve(ctx, id)
	if err != nil {
		return nil, err
	}

	if err := m.store.Delete(ctx, id); err != nil {
		return nil, err
	}

	return container, nil
}

// Cleanup removes expired continuity containers
func (m *ContinuityManager) Cleanup(ctx context.Context) (int64, error) {
	return m.store.DeleteExpired(ctx)
}
