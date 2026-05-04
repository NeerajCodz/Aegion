package store

import (
	"errors"
	"sync"
	"time"
)

var (
	ErrCredentialNotFound = errors.New("credential not found")
	ErrCredentialExists   = errors.New("credential already exists")
	ErrChallengeNotFound  = errors.New("challenge not found")
	ErrChallengeExpired   = errors.New("challenge expired")
)

type Credential struct {
	ID         string
	IdentityID string
	PublicKey  string
	SignCount  uint32
	CreatedAt  time.Time
}

type Challenge struct {
	ID         string
	IdentityID string
	Purpose    string
	ExpiresAt  time.Time
}

// Store handles passkeys persistence concerns.
type Store struct {
	mu          sync.Mutex
	credentials map[string]Credential
	challenges  map[string]Challenge
}

// New creates a new passkeys store.
func New() *Store {
	return &Store{
		credentials: make(map[string]Credential),
		challenges:  make(map[string]Challenge),
	}
}

func (s *Store) SaveChallenge(challenge Challenge) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.challenges[challenge.ID] = challenge
}

func (s *Store) ConsumeChallenge(challengeID string) (Challenge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	challenge, ok := s.challenges[challengeID]
	if !ok {
		return Challenge{}, ErrChallengeNotFound
	}
	delete(s.challenges, challengeID)
	if time.Now().UTC().After(challenge.ExpiresAt) {
		return Challenge{}, ErrChallengeExpired
	}
	return challenge, nil
}

func (s *Store) CreateCredential(credential Credential) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.credentials[credential.ID]; exists {
		return ErrCredentialExists
	}
	s.credentials[credential.ID] = credential
	return nil
}

func (s *Store) GetCredential(credentialID string) (Credential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	credential, ok := s.credentials[credentialID]
	if !ok {
		return Credential{}, ErrCredentialNotFound
	}
	return credential, nil
}

func (s *Store) UpdateCredentialSignCount(credentialID string, signCount uint32) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	credential, ok := s.credentials[credentialID]
	if !ok {
		return ErrCredentialNotFound
	}
	credential.SignCount = signCount
	s.credentials[credentialID] = credential
	return nil
}

func (s *Store) ListCredentialsByIdentity(identityID string) []Credential {
	s.mu.Lock()
	defer s.mu.Unlock()
	credentials := make([]Credential, 0)
	for _, credential := range s.credentials {
		if credential.IdentityID == identityID {
			credentials = append(credentials, credential)
		}
	}
	return credentials
}
