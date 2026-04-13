package service

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/aegion/aegion/modules/passkeys/store"
)

var (
	ErrInvalidIdentity  = errors.New("identity_id is required")
	ErrInvalidChallenge = errors.New("challenge is invalid")
	ErrInvalidCredential = errors.New("credential is invalid")
)

type Config struct {
	RPID               string
	RPOrigin           string
	ChallengeTTL       time.Duration
	AllowedCredentials int
}

type ChallengeStore interface {
	SaveChallenge(challenge store.Challenge)
	ConsumeChallenge(challengeID string) (store.Challenge, error)
	UpsertCredential(credential store.Credential)
	GetCredential(credentialID string) (store.Credential, error)
	ListCredentialsByIdentity(identityID string) []store.Credential
}

type Service struct {
	store ChallengeStore
	cfg   Config
}

type RegistrationStartResponse struct {
	Challenge string `json:"challenge"`
	RPID      string `json:"rp_id"`
	RPOrigin  string `json:"rp_origin"`
	ExpiresIn int    `json:"expires_in"`
}

type RegistrationFinishRequest struct {
	IdentityID   string `json:"identity_id"`
	Challenge    string `json:"challenge"`
	CredentialID string `json:"credential_id"`
	PublicKey    string `json:"public_key"`
}

type AuthenticationStartResponse struct {
	Challenge            string   `json:"challenge"`
	AllowedCredentialIDs []string `json:"allowed_credential_ids"`
	ExpiresIn            int      `json:"expires_in"`
}

type AuthenticationFinishRequest struct {
	IdentityID   string `json:"identity_id"`
	Challenge    string `json:"challenge"`
	CredentialID string `json:"credential_id"`
}

func New(challengeStore ChallengeStore, cfg Config) *Service {
	if cfg.ChallengeTTL <= 0 {
		cfg.ChallengeTTL = 5 * time.Minute
	}
	if cfg.RPID == "" {
		cfg.RPID = "localhost"
	}
	if cfg.RPOrigin == "" {
		cfg.RPOrigin = "http://localhost"
	}
	if cfg.AllowedCredentials <= 0 {
		cfg.AllowedCredentials = 20
	}
	return &Service{
		store: challengeStore,
		cfg:   cfg,
	}
}

func (s *Service) BeginRegistration(identityID string) (*RegistrationStartResponse, error) {
	identityID = strings.TrimSpace(identityID)
	if identityID == "" {
		return nil, ErrInvalidIdentity
	}
	challenge, err := randomChallenge()
	if err != nil {
		return nil, err
	}
	s.store.SaveChallenge(store.Challenge{
		ID:         challenge,
		IdentityID: identityID,
		Purpose:    "registration",
		ExpiresAt:  time.Now().UTC().Add(s.cfg.ChallengeTTL),
	})
	return &RegistrationStartResponse{
		Challenge: challenge,
		RPID:      s.cfg.RPID,
		RPOrigin:  s.cfg.RPOrigin,
		ExpiresIn: int(s.cfg.ChallengeTTL.Seconds()),
	}, nil
}

func (s *Service) FinishRegistration(req *RegistrationFinishRequest) error {
	if req == nil || strings.TrimSpace(req.IdentityID) == "" {
		return ErrInvalidIdentity
	}
	if strings.TrimSpace(req.Challenge) == "" || strings.TrimSpace(req.CredentialID) == "" || strings.TrimSpace(req.PublicKey) == "" {
		return ErrInvalidCredential
	}
	challenge, err := s.store.ConsumeChallenge(req.Challenge)
	if err != nil {
		return ErrInvalidChallenge
	}
	if challenge.Purpose != "registration" || challenge.IdentityID != req.IdentityID {
		return ErrInvalidChallenge
	}
	s.store.UpsertCredential(store.Credential{
		ID:         req.CredentialID,
		IdentityID: req.IdentityID,
		PublicKey:  req.PublicKey,
		CreatedAt:  time.Now().UTC(),
	})
	return nil
}

func (s *Service) BeginAuthentication(identityID string) (*AuthenticationStartResponse, error) {
	identityID = strings.TrimSpace(identityID)
	if identityID == "" {
		return nil, ErrInvalidIdentity
	}
	challenge, err := randomChallenge()
	if err != nil {
		return nil, err
	}
	s.store.SaveChallenge(store.Challenge{
		ID:         challenge,
		IdentityID: identityID,
		Purpose:    "authentication",
		ExpiresAt:  time.Now().UTC().Add(s.cfg.ChallengeTTL),
	})
	credentials := s.store.ListCredentialsByIdentity(identityID)
	allowed := make([]string, 0, len(credentials))
	for _, credential := range credentials {
		allowed = append(allowed, credential.ID)
	}
	return &AuthenticationStartResponse{
		Challenge:            challenge,
		AllowedCredentialIDs: allowed,
		ExpiresIn:            int(s.cfg.ChallengeTTL.Seconds()),
	}, nil
}

func (s *Service) FinishAuthentication(req *AuthenticationFinishRequest) error {
	if req == nil || strings.TrimSpace(req.IdentityID) == "" {
		return ErrInvalidIdentity
	}
	if strings.TrimSpace(req.CredentialID) == "" || strings.TrimSpace(req.Challenge) == "" {
		return ErrInvalidCredential
	}
	challenge, err := s.store.ConsumeChallenge(req.Challenge)
	if err != nil {
		return ErrInvalidChallenge
	}
	if challenge.Purpose != "authentication" || challenge.IdentityID != req.IdentityID {
		return ErrInvalidChallenge
	}
	credential, err := s.store.GetCredential(req.CredentialID)
	if err != nil {
		return ErrInvalidCredential
	}
	if credential.IdentityID != req.IdentityID {
		return ErrInvalidCredential
	}
	return nil
}

func randomChallenge() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
