package store

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type MemoryStore struct {
	mu             sync.Mutex
	providers      map[string]Provider
	states         map[string]AuthState
	linkBySubject  map[string]uuid.UUID
	identityByMail map[string]uuid.UUID
}

func New() *MemoryStore {
	return &MemoryStore{
		providers:      make(map[string]Provider),
		states:         make(map[string]AuthState),
		linkBySubject:  make(map[string]uuid.UUID),
		identityByMail: make(map[string]uuid.UUID),
	}
}

func (s *MemoryStore) ListProviders(_ context.Context, includeDisabled bool) ([]Provider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	providers := make([]Provider, 0, len(s.providers))
	for _, provider := range s.providers {
		if !includeDisabled && !provider.Enabled {
			continue
		}
		providers = append(providers, provider)
	}
	return providers, nil
}

func (s *MemoryStore) GetProviderBySlug(_ context.Context, slug string) (*Provider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	provider, ok := s.providers[normalizeSlug(slug)]
	if !ok {
		return nil, ErrProviderNotFound
	}
	copy := provider
	return &copy, nil
}

func (s *MemoryStore) UpsertProvider(_ context.Context, provider Provider) (*Provider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	slug := normalizeSlug(provider.Slug)
	now := time.Now().UTC()
	existing, ok := s.providers[slug]
	if ok {
		provider.ID = existing.ID
		provider.CreatedAt = existing.CreatedAt
		if strings.TrimSpace(provider.ClientSecret) == "" {
			provider.ClientSecret = existing.ClientSecret
		}
	} else {
		if provider.ID == uuid.Nil {
			provider.ID = uuid.New()
		}
		provider.CreatedAt = now
	}
	provider.Slug = slug
	provider.UpdatedAt = now
	s.providers[slug] = provider

	copy := provider
	return &copy, nil
}

func (s *MemoryStore) DeleteProvider(_ context.Context, slug string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	slug = normalizeSlug(slug)
	if _, ok := s.providers[slug]; !ok {
		return ErrProviderNotFound
	}
	delete(s.providers, slug)
	return nil
}

func (s *MemoryStore) SaveState(_ context.Context, state AuthState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states[state.ID] = state
	return nil
}

func (s *MemoryStore) ConsumeState(_ context.Context, stateID string) (AuthState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, ok := s.states[stateID]
	if !ok {
		return AuthState{}, ErrStateNotFound
	}
	delete(s.states, stateID)
	if time.Now().UTC().After(state.ExpiresAt) {
		return AuthState{}, ErrStateExpired
	}
	return state, nil
}

func (s *MemoryStore) ResolveIdentity(_ context.Context, provider Provider, profile SocialProfile) (*IdentityLinkResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := provider.Slug + ":" + profile.ProviderUser
	if identityID, ok := s.linkBySubject[key]; ok {
		return &IdentityLinkResult{IdentityID: identityID, Linked: true}, nil
	}

	if email := strings.ToLower(strings.TrimSpace(profile.Email)); email != "" {
		if identityID, ok := s.identityByMail[email]; ok {
			s.linkBySubject[key] = identityID
			return &IdentityLinkResult{IdentityID: identityID, Linked: true}, nil
		}
	}

	identityID := uuid.New()
	s.linkBySubject[key] = identityID
	if email := strings.ToLower(strings.TrimSpace(profile.Email)); email != "" {
		s.identityByMail[email] = identityID
	}
	return &IdentityLinkResult{
		IdentityID: identityID,
		Created:    true,
		Linked:     true,
	}, nil
}

func normalizeSlug(slug string) string {
	return strings.ToLower(strings.TrimSpace(slug))
}
