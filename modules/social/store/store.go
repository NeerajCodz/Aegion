package store

import (
	"errors"
	"sync"
	"time"
)

var (
	ErrStateNotFound = errors.New("state not found")
	ErrStateExpired  = errors.New("state expired")
)

// AuthState tracks pending social login state.
type AuthState struct {
	ID         string
	Provider   string
	RedirectTo string
	ExpiresAt  time.Time
}

// SocialProfile stores normalized social profile data.
type SocialProfile struct {
	Provider      string
	ProviderUser  string
	Email         string
	EmailVerified bool
	Name          string
	PictureURL    string
}

// Store handles social module persistence concerns.
type Store struct {
	mu      sync.Mutex
	states  map[string]AuthState
	profile map[string]SocialProfile
}

// New creates a new social store.
func New() *Store {
	return &Store{
		states:  make(map[string]AuthState),
		profile: make(map[string]SocialProfile),
	}
}

// SaveState stores a state nonce with expiry.
func (s *Store) SaveState(state AuthState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states[state.ID] = state
}

// ConsumeState loads and invalidates a state nonce.
func (s *Store) ConsumeState(stateID string) (AuthState, error) {
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

// UpsertProfile upserts social profile by provider user key.
func (s *Store) UpsertProfile(profile SocialProfile) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.profile[profile.Provider+":"+profile.ProviderUser] = profile
}
