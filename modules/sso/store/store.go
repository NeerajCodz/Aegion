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
	ErrConnectionNotFound  = errors.New("sso connection not found")
	ErrAuthRequestConflict = errors.New("sso auth request already exists")
)

type AttributeMapping struct {
	Subject     string `json:"subject"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	FirstName   string `json:"first_name"`
	LastName    string `json:"last_name"`
}

type Connection struct {
	ID                uuid.UUID         `json:"id"`
	Slug              string            `json:"slug"`
	DisplayName       string            `json:"display_name"`
	EntityID          string            `json:"entity_id"`
	SSOURL            string            `json:"sso_url"`
	CertificatePEM    string            `json:"certificate_pem,omitempty"`
	MetadataURL       string            `json:"metadata_url,omitempty"`
	Domains           []string          `json:"domains,omitempty"`
	AttributeMapping  AttributeMapping  `json:"attribute_mapping"`
	JITProvisioning   bool              `json:"jit_provisioning"`
	DefaultRedirectTo string            `json:"default_redirect_to,omitempty"`
	ExtraAuthnContext map[string]string `json:"extra_authn_context,omitempty"`
	Enabled           bool              `json:"enabled"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

func (c Connection) Sanitized() Connection {
	return c
}

type Repository interface {
	ListConnections(ctx context.Context, includeDisabled bool) ([]Connection, error)
	GetConnectionBySlug(ctx context.Context, slug string) (*Connection, error)
	GetConnectionByDomain(ctx context.Context, domain string) (*Connection, error)
	UpsertConnection(ctx context.Context, connection Connection) (*Connection, error)
	DeleteConnection(ctx context.Context, slug string) error
	CreateAuthRequest(ctx context.Context, requestID, connectionSlug string, expiresAt time.Time) error
	ConsumeAuthRequest(ctx context.Context, requestID, connectionSlug string, now time.Time) (bool, error)
}

type MemoryStore struct {
	mu           sync.RWMutex
	connections  map[string]Connection
	authRequests map[string]authRequest
}

type authRequest struct {
	connectionSlug string
	expiresAt      time.Time
	consumedAt     *time.Time
}

func New() *MemoryStore {
	return &MemoryStore{
		connections:  make(map[string]Connection),
		authRequests: make(map[string]authRequest),
	}
}

func (s *MemoryStore) ListConnections(_ context.Context, includeDisabled bool) ([]Connection, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	connections := make([]Connection, 0, len(s.connections))
	for _, connection := range s.connections {
		if !includeDisabled && !connection.Enabled {
			continue
		}
		connections = append(connections, cloneConnection(connection))
	}
	sort.Slice(connections, func(i, j int) bool {
		return connections[i].DisplayName < connections[j].DisplayName
	})
	return connections, nil
}

func (s *MemoryStore) GetConnectionBySlug(_ context.Context, slug string) (*Connection, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	connection, ok := s.connections[normalizeSlug(slug)]
	if !ok {
		return nil, ErrConnectionNotFound
	}
	cloned := cloneConnection(connection)
	return &cloned, nil
}

func (s *MemoryStore) GetConnectionByDomain(_ context.Context, domain string) (*Connection, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	domain = normalizeDomain(domain)
	for _, connection := range s.connections {
		for _, candidate := range connection.Domains {
			if normalizeDomain(candidate) == domain {
				cloned := cloneConnection(connection)
				return &cloned, nil
			}
		}
	}
	return nil, ErrConnectionNotFound
}

func (s *MemoryStore) UpsertConnection(_ context.Context, connection Connection) (*Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := normalizeSlug(connection.Slug)
	if key == "" {
		return nil, ErrConnectionNotFound
	}
	if existing, ok := s.connections[key]; ok {
		connection.CreatedAt = existing.CreatedAt
		if connection.ID == uuid.Nil {
			connection.ID = existing.ID
		}
	}
	if connection.ID == uuid.Nil {
		connection.ID = uuid.New()
	}
	s.connections[key] = cloneConnection(connection)
	cloned := cloneConnection(s.connections[key])
	return &cloned, nil
}

func (s *MemoryStore) DeleteConnection(_ context.Context, slug string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := normalizeSlug(slug)
	if _, ok := s.connections[key]; !ok {
		return ErrConnectionNotFound
	}
	delete(s.connections, key)
	return nil
}

func (s *MemoryStore) CreateAuthRequest(_ context.Context, requestID, connectionSlug string, expiresAt time.Time) error {
	requestID = strings.TrimSpace(requestID)
	connectionSlug = normalizeSlug(connectionSlug)
	if requestID == "" || connectionSlug == "" || !expiresAt.After(time.Now().UTC()) {
		return ErrAuthRequestConflict
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	for id, request := range s.authRequests {
		if !request.expiresAt.After(now) {
			delete(s.authRequests, id)
		}
	}
	if _, exists := s.authRequests[requestID]; exists {
		return ErrAuthRequestConflict
	}
	s.authRequests[requestID] = authRequest{
		connectionSlug: connectionSlug,
		expiresAt:      expiresAt.UTC(),
	}
	return nil
}

func (s *MemoryStore) ConsumeAuthRequest(_ context.Context, requestID, connectionSlug string, now time.Time) (bool, error) {
	requestID = strings.TrimSpace(requestID)
	connectionSlug = normalizeSlug(connectionSlug)
	if requestID == "" || connectionSlug == "" {
		return false, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	request, exists := s.authRequests[requestID]
	if !exists || request.connectionSlug != connectionSlug || !request.expiresAt.After(now) || request.consumedAt != nil {
		return false, nil
	}
	consumedAt := now.UTC()
	request.consumedAt = &consumedAt
	s.authRequests[requestID] = request
	return true, nil
}

func cloneConnection(in Connection) Connection {
	out := in
	out.Domains = append([]string(nil), in.Domains...)
	out.ExtraAuthnContext = cloneStringMap(in.ExtraAuthnContext)
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

func normalizeSlug(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeDomain(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "@")
	return value
}
