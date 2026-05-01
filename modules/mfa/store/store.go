package store

import (
	"errors"
	"sync"
	"time"
)

var (
	ErrEnrollmentNotFound    = errors.New("mfa enrollment not found")
	ErrTOTPFactorNotFound    = errors.New("mfa totp factor not found")
	ErrBackupCodeNotFound    = errors.New("mfa backup code not found")
	ErrTrustedDeviceNotFound = errors.New("mfa trusted device not found")
)

type Enrollment struct {
	ID               string
	IdentityID       string
	SecretCiphertext string
	ExpiresAt        time.Time
	CreatedAt        time.Time
}

type TOTPFactor struct {
	IdentityID       string
	SecretCiphertext string
	EnrolledAt       time.Time
	LastUsedAt       time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type BackupCode struct {
	ID         string
	IdentityID string
	CodeHash   string
	BatchID    string
	UsedAt     *time.Time
	CreatedAt  time.Time
}

type TrustedDevice struct {
	ID          string
	IdentityID  string
	TokenHash   string
	TokenPrefix string
	Label       string
	ExpiresAt   time.Time
	LastUsedAt  *time.Time
	CreatedAt   time.Time
	RevokedAt   *time.Time
}

type Factor struct {
	ID         string
	Method     string
	Verified   bool
	EnrolledAt time.Time
	LastUsedAt time.Time
}

// Store handles MFA persistence concerns using an in-memory backend.
type Store struct {
	mu             sync.Mutex
	enrollments    map[string]Enrollment
	totpFactors    map[string]TOTPFactor
	backupCodes    map[string][]BackupCode
	trustedDevices map[string][]TrustedDevice
}

// New creates a new MFA store.
func New() *Store {
	return &Store{
		enrollments:    make(map[string]Enrollment),
		totpFactors:    make(map[string]TOTPFactor),
		backupCodes:    make(map[string][]BackupCode),
		trustedDevices: make(map[string][]TrustedDevice),
	}
}

func (s *Store) SaveEnrollment(enrollment Enrollment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.enrollments[enrollment.ID] = enrollment
	return nil
}

func (s *Store) GetEnrollment(enrollmentID string) (Enrollment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	enrollment, ok := s.enrollments[enrollmentID]
	if !ok {
		return Enrollment{}, ErrEnrollmentNotFound
	}
	return enrollment, nil
}

func (s *Store) DeleteEnrollment(enrollmentID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.enrollments, enrollmentID)
	return nil
}

func (s *Store) UpsertTOTPFactor(factor TOTPFactor) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.totpFactors[factor.IdentityID] = factor
	return nil
}

func (s *Store) GetTOTPFactor(identityID string) (TOTPFactor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	factor, ok := s.totpFactors[identityID]
	if !ok {
		return TOTPFactor{}, ErrTOTPFactorNotFound
	}
	return factor, nil
}

func (s *Store) UpdateTOTPLastUsed(identityID string, usedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	factor, ok := s.totpFactors[identityID]
	if !ok {
		return ErrTOTPFactorNotFound
	}
	factor.LastUsedAt = usedAt
	factor.UpdatedAt = usedAt
	s.totpFactors[identityID] = factor
	return nil
}

func (s *Store) ReplaceBackupCodes(identityID string, codes []BackupCode) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cloned := make([]BackupCode, len(codes))
	copy(cloned, codes)
	s.backupCodes[identityID] = cloned
	return nil
}

func (s *Store) ListBackupCodes(identityID string) ([]BackupCode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	codes := s.backupCodes[identityID]
	cloned := make([]BackupCode, len(codes))
	copy(cloned, codes)
	return cloned, nil
}

func (s *Store) MarkBackupCodeUsed(identityID, codeID string, usedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	codes := s.backupCodes[identityID]
	for idx := range codes {
		if codes[idx].ID != codeID {
			continue
		}
		codes[idx].UsedAt = &usedAt
		s.backupCodes[identityID] = codes
		return nil
	}
	return ErrBackupCodeNotFound
}

func (s *Store) SaveTrustedDevice(device TrustedDevice) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trustedDevices[device.IdentityID] = append(s.trustedDevices[device.IdentityID], device)
	return nil
}

func (s *Store) GetTrustedDevice(identityID, tokenHash, tokenPrefix string) (TrustedDevice, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, device := range s.trustedDevices[identityID] {
		if device.TokenHash == tokenHash && device.TokenPrefix == tokenPrefix {
			return device, nil
		}
	}
	return TrustedDevice{}, ErrTrustedDeviceNotFound
}

func (s *Store) TouchTrustedDevice(identityID, deviceID string, touchedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	devices := s.trustedDevices[identityID]
	for idx := range devices {
		if devices[idx].ID != deviceID {
			continue
		}
		devices[idx].LastUsedAt = &touchedAt
		s.trustedDevices[identityID] = devices
		return nil
	}
	return ErrTrustedDeviceNotFound
}

func (s *Store) DeleteTrustedDevice(identityID, deviceID string, revokedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	devices := s.trustedDevices[identityID]
	for idx := range devices {
		if devices[idx].ID != deviceID {
			continue
		}
		devices[idx].RevokedAt = &revokedAt
		s.trustedDevices[identityID] = devices
		return nil
	}
	return ErrTrustedDeviceNotFound
}

func (s *Store) DeleteAllIdentityData(identityID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.totpFactors, identityID)
	delete(s.backupCodes, identityID)
	delete(s.trustedDevices, identityID)
	for enrollmentID, enrollment := range s.enrollments {
		if enrollment.IdentityID == identityID {
			delete(s.enrollments, enrollmentID)
		}
	}
	return nil
}

func (s *Store) ListFactorsByIdentity(identityID string) ([]Factor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	factor, ok := s.totpFactors[identityID]
	if !ok {
		return []Factor{}, nil
	}
	return []Factor{
		{
			ID:         identityID + ":totp",
			Method:     "totp",
			Verified:   true,
			EnrolledAt: factor.EnrolledAt,
			LastUsedAt: factor.LastUsedAt,
		},
	}, nil
}
