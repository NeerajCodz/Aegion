// Package service provides password authentication business logic.
package service

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/aegion/aegion/modules/password/store"
)

var (
	ErrPasswordTooShort   = errors.New("password is too short")
	ErrPasswordTooWeak    = errors.New("password does not meet complexity requirements")
	ErrPasswordBreached   = errors.New("password has been found in a data breach")
	ErrPasswordReused     = errors.New("password was used recently")
	ErrPasswordSimilar    = errors.New("password is too similar to identifier")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrIdentityNotFound   = errors.New("identity not found")
)

// Config holds password service configuration.
type Config struct {
	MinLength        int
	RequireUppercase bool
	RequireLowercase bool
	RequireNumber    bool
	RequireSpecial   bool
	HIBPEnabled      bool
	HistoryCount     int
}

// Hasher interface for password hashing.
type Hasher interface {
	Hash(password string) (string, error)
	Verify(password, hash string) (bool, error)
}

// Service handles password authentication.
type Service struct {
	store  *store.Store
	hasher Hasher
	config Config
}

// New creates a new password service.
func New(store *store.Store, hasher Hasher, config Config) *Service {
	if config.MinLength == 0 {
		config.MinLength = 8
	}
	if config.HistoryCount == 0 {
		config.HistoryCount = 5
	}

	return &Service{
		store:  store,
		hasher: hasher,
		config: config,
	}
}

// Register creates a new password credential for an identity.
func (s *Service) Register(ctx context.Context, identityID uuid.UUID, identifier, password string) error {
	// Validate password
	if err := s.ValidatePassword(ctx, password, identifier); err != nil {
		return err
	}

	// Hash password
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	// Create credential
	cred := &store.Credential{
		ID:         uuid.New(),
		IdentityID: identityID,
		Identifier: strings.ToLower(identifier),
		Hash:       hash,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}

	return s.store.Create(ctx, cred)
}

// Verify verifies a password against stored credentials.
// Returns the identity ID if successful.
func (s *Service) Verify(ctx context.Context, identifier, password string) (uuid.UUID, error) {
	cred, err := s.store.GetByIdentifier(ctx, strings.ToLower(identifier))
	if err != nil {
		if errors.Is(err, store.ErrCredentialNotFound) {
			// Constant-time delay to prevent timing attacks
			s.hasher.Hash(password) // Dummy hash
			return uuid.Nil, ErrInvalidCredentials
		}
		return uuid.Nil, err
	}

	valid, err := s.hasher.Verify(password, cred.Hash)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to verify password: %w", err)
	}

	if !valid {
		return uuid.Nil, ErrInvalidCredentials
	}

	return cred.IdentityID, nil
}

// ChangePassword changes the password for an identity.
func (s *Service) ChangePassword(ctx context.Context, identityID uuid.UUID, oldPassword, newPassword string) error {
	cred, err := s.store.GetByIdentityID(ctx, identityID)
	if err != nil {
		if errors.Is(err, store.ErrCredentialNotFound) {
			return ErrIdentityNotFound
		}
		return err
	}

	// Verify old password
	valid, err := s.hasher.Verify(oldPassword, cred.Hash)
	if err != nil || !valid {
		return ErrInvalidCredentials
	}

	// Validate new password
	if err := s.ValidatePassword(ctx, newPassword, cred.Identifier); err != nil {
		return err
	}

	// Check against history
	if err := s.checkHistory(ctx, cred.ID, newPassword); err != nil {
		return err
	}

	// Hash new password
	newHash, err := s.hasher.Hash(newPassword)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	// Add old hash to history
	if err := s.store.AddToHistory(ctx, cred.ID, cred.Hash); err != nil {
		return err
	}

	// Update credential
	if err := s.store.Update(ctx, cred.ID, newHash); err != nil {
		return err
	}

	// Cleanup old history
	return s.store.CleanupHistory(ctx, cred.ID, s.config.HistoryCount)
}

// ResetPassword sets a new password without requiring the old one.
// Used for password recovery flows.
func (s *Service) ResetPassword(ctx context.Context, identityID uuid.UUID, newPassword string) error {
	cred, err := s.store.GetByIdentityID(ctx, identityID)
	if err != nil {
		if errors.Is(err, store.ErrCredentialNotFound) {
			return ErrIdentityNotFound
		}
		return err
	}

	// Validate new password
	if err := s.ValidatePassword(ctx, newPassword, cred.Identifier); err != nil {
		return err
	}

	// Check against history
	if err := s.checkHistory(ctx, cred.ID, newPassword); err != nil {
		return err
	}

	// Hash new password
	newHash, err := s.hasher.Hash(newPassword)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	// Add old hash to history
	if err := s.store.AddToHistory(ctx, cred.ID, cred.Hash); err != nil {
		return err
	}

	// Update credential
	return s.store.Update(ctx, cred.ID, newHash)
}

// ValidatePassword validates a password against all configured rules.
func (s *Service) ValidatePassword(ctx context.Context, password, identifier string) error {
	// Length check
	if len(password) < s.config.MinLength {
		return ErrPasswordTooShort
	}

	// Complexity checks
	if err := s.checkComplexity(password); err != nil {
		return err
	}

	// Similarity check
	if err := s.checkSimilarity(password, identifier); err != nil {
		return err
	}

	// HIBP check
	if s.config.HIBPEnabled {
		if err := s.checkHIBP(ctx, password); err != nil {
			return err
		}
	}

	return nil
}

// checkComplexity validates password complexity requirements.
func (s *Service) checkComplexity(password string) error {
	var hasUpper, hasLower, hasNumber, hasSpecial bool

	for _, c := range password {
		switch {
		case unicode.IsUpper(c):
			hasUpper = true
		case unicode.IsLower(c):
			hasLower = true
		case unicode.IsNumber(c):
			hasNumber = true
		case unicode.IsPunct(c) || unicode.IsSymbol(c):
			hasSpecial = true
		}
	}

	if s.config.RequireUppercase && !hasUpper {
		return ErrPasswordTooWeak
	}
	if s.config.RequireLowercase && !hasLower {
		return ErrPasswordTooWeak
	}
	if s.config.RequireNumber && !hasNumber {
		return ErrPasswordTooWeak
	}
	if s.config.RequireSpecial && !hasSpecial {
		return ErrPasswordTooWeak
	}

	return nil
}

// checkSimilarity checks if password is too similar to identifier.
func (s *Service) checkSimilarity(password, identifier string) error {
	// Skip check if identifier is empty
	if identifier == "" {
		return nil
	}

	passwordLower := strings.ToLower(password)
	identifierLower := strings.ToLower(identifier)

	// Extract username part from email
	if idx := strings.Index(identifierLower, "@"); idx > 0 {
		identifierLower = identifierLower[:idx]
	}

	// Skip check if username part is too short (< 3 chars)
	if len(identifierLower) < 3 {
		return nil
	}

	// Check if password contains identifier or vice versa
	if strings.Contains(passwordLower, identifierLower) ||
		strings.Contains(identifierLower, passwordLower) {
		return ErrPasswordSimilar
	}

	return nil
}

// checkHIBP checks password against Have I Been Pwned API using k-anonymity.
func (s *Service) checkHIBP(ctx context.Context, password string) error {
	// SHA-1 hash of password
	hash := sha1.Sum([]byte(password))
	hashStr := strings.ToUpper(hex.EncodeToString(hash[:]))

	prefix := hashStr[:5]
	suffix := hashStr[5:]

	// Query HIBP API
	url := fmt.Sprintf("https://api.pwnedpasswords.com/range/%s", prefix)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		// Don't fail registration if HIBP is unavailable
		return nil
	}
	req.Header.Set("User-Agent", "Aegion-Identity-Server")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		// Don't fail registration if HIBP is unavailable
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	// Check if suffix is in response
	lines := strings.Split(string(body), "\n")
	for _, line := range lines {
		parts := strings.Split(strings.TrimSpace(line), ":")
		if len(parts) >= 1 && strings.EqualFold(parts[0], suffix) {
			return ErrPasswordBreached
		}
	}

	return nil
}

// checkHistory checks if password matches any historical passwords.
func (s *Service) checkHistory(ctx context.Context, credID uuid.UUID, password string) error {
	history, err := s.store.GetHistory(ctx, credID, s.config.HistoryCount)
	if err != nil {
		return err
	}

	for _, hash := range history {
		valid, err := s.hasher.Verify(password, hash)
		if err != nil {
			continue
		}
		if valid {
			return ErrPasswordReused
		}
	}

	return nil
}

// Delete removes password credentials for an identity.
func (s *Service) Delete(ctx context.Context, identityID uuid.UUID) error {
	return s.store.DeleteByIdentityID(ctx, identityID)
}
