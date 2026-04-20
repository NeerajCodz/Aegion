package store

import (
	"errors"
	"testing"
	"time"
)

func TestEnrollmentLifecycle(t *testing.T) {
	s := New()
	now := time.Now().UTC().Round(0)
	enrollment := Enrollment{
		ID:               "enroll-1",
		IdentityID:       "identity-1",
		SecretCiphertext: "cipher",
		ExpiresAt:        now.Add(time.Minute),
		CreatedAt:        now,
	}

	if err := s.SaveEnrollment(enrollment); err != nil {
		t.Fatalf("SaveEnrollment() error = %v", err)
	}
	got, err := s.GetEnrollment(enrollment.ID)
	if err != nil {
		t.Fatalf("GetEnrollment() error = %v", err)
	}
	if got.IdentityID != enrollment.IdentityID || got.SecretCiphertext != enrollment.SecretCiphertext {
		t.Fatalf("GetEnrollment() = %#v, want %#v", got, enrollment)
	}

	if err := s.DeleteEnrollment(enrollment.ID); err != nil {
		t.Fatalf("DeleteEnrollment() error = %v", err)
	}
	if _, err := s.GetEnrollment(enrollment.ID); !errors.Is(err, ErrEnrollmentNotFound) {
		t.Fatalf("GetEnrollment(after delete) error = %v, want %v", err, ErrEnrollmentNotFound)
	}
}

func TestTOTPFactorLifecycle(t *testing.T) {
	s := New()
	now := time.Now().UTC().Round(0)
	factor := TOTPFactor{
		IdentityID:       "identity-1",
		SecretCiphertext: "cipher",
		EnrolledAt:       now,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if err := s.UpsertTOTPFactor(factor); err != nil {
		t.Fatalf("UpsertTOTPFactor() error = %v", err)
	}
	got, err := s.GetTOTPFactor("identity-1")
	if err != nil {
		t.Fatalf("GetTOTPFactor() error = %v", err)
	}
	if got.SecretCiphertext != "cipher" {
		t.Fatalf("GetTOTPFactor().SecretCiphertext = %q, want cipher", got.SecretCiphertext)
	}

	usedAt := now.Add(2 * time.Minute)
	if err := s.UpdateTOTPLastUsed("identity-1", usedAt); err != nil {
		t.Fatalf("UpdateTOTPLastUsed() error = %v", err)
	}
	got, err = s.GetTOTPFactor("identity-1")
	if err != nil {
		t.Fatalf("GetTOTPFactor(after update) error = %v", err)
	}
	if !got.LastUsedAt.Equal(usedAt) || !got.UpdatedAt.Equal(usedAt) {
		t.Fatalf("GetTOTPFactor() timestamps = (%v,%v), want both %v", got.LastUsedAt, got.UpdatedAt, usedAt)
	}

	if _, err := s.GetTOTPFactor("missing"); !errors.Is(err, ErrTOTPFactorNotFound) {
		t.Fatalf("GetTOTPFactor(missing) error = %v, want %v", err, ErrTOTPFactorNotFound)
	}
	if err := s.UpdateTOTPLastUsed("missing", now); !errors.Is(err, ErrTOTPFactorNotFound) {
		t.Fatalf("UpdateTOTPLastUsed(missing) error = %v, want %v", err, ErrTOTPFactorNotFound)
	}
}

func TestBackupCodesLifecycleAndCloning(t *testing.T) {
	s := New()
	now := time.Now().UTC().Round(0)

	codes := []BackupCode{
		{ID: "code-1", IdentityID: "identity-1", CodeHash: "h1", BatchID: "b1", CreatedAt: now},
		{ID: "code-2", IdentityID: "identity-1", CodeHash: "h2", BatchID: "b1", CreatedAt: now},
	}
	if err := s.ReplaceBackupCodes("identity-1", codes); err != nil {
		t.Fatalf("ReplaceBackupCodes() error = %v", err)
	}

	listed, err := s.ListBackupCodes("identity-1")
	if err != nil {
		t.Fatalf("ListBackupCodes() error = %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("ListBackupCodes() len = %d, want 2", len(listed))
	}

	// Ensure returned slice is cloned.
	listed[0].CodeHash = "mutated"
	listedAgain, err := s.ListBackupCodes("identity-1")
	if err != nil {
		t.Fatalf("ListBackupCodes(second) error = %v", err)
	}
	if listedAgain[0].CodeHash != "h1" {
		t.Fatalf("ListBackupCodes() should return clone, got mutated value %q", listedAgain[0].CodeHash)
	}

	usedAt := now.Add(time.Minute)
	if err := s.MarkBackupCodeUsed("identity-1", "code-1", usedAt); err != nil {
		t.Fatalf("MarkBackupCodeUsed() error = %v", err)
	}
	listedAfter, err := s.ListBackupCodes("identity-1")
	if err != nil {
		t.Fatalf("ListBackupCodes(after mark) error = %v", err)
	}
	if listedAfter[0].UsedAt == nil || !listedAfter[0].UsedAt.Equal(usedAt) {
		t.Fatalf("MarkBackupCodeUsed() did not set UsedAt correctly: %#v", listedAfter[0].UsedAt)
	}
	if err := s.MarkBackupCodeUsed("identity-1", "missing", usedAt); !errors.Is(err, ErrBackupCodeNotFound) {
		t.Fatalf("MarkBackupCodeUsed(missing) error = %v, want %v", err, ErrBackupCodeNotFound)
	}
}

func TestTrustedDeviceLifecycle(t *testing.T) {
	s := New()
	now := time.Now().UTC().Round(0)
	device := TrustedDevice{
		ID:          "device-1",
		IdentityID:  "identity-1",
		TokenHash:   "hash-1",
		TokenPrefix: "pref",
		Label:       "Laptop",
		ExpiresAt:   now.Add(24 * time.Hour),
		CreatedAt:   now,
	}
	if err := s.SaveTrustedDevice(device); err != nil {
		t.Fatalf("SaveTrustedDevice() error = %v", err)
	}

	got, err := s.GetTrustedDevice("identity-1", "hash-1", "pref")
	if err != nil {
		t.Fatalf("GetTrustedDevice() error = %v", err)
	}
	if got.ID != "device-1" {
		t.Fatalf("GetTrustedDevice().ID = %q, want device-1", got.ID)
	}

	touchedAt := now.Add(time.Hour)
	if err := s.TouchTrustedDevice("identity-1", "device-1", touchedAt); err != nil {
		t.Fatalf("TouchTrustedDevice() error = %v", err)
	}
	got, err = s.GetTrustedDevice("identity-1", "hash-1", "pref")
	if err != nil {
		t.Fatalf("GetTrustedDevice(after touch) error = %v", err)
	}
	if got.LastUsedAt == nil || !got.LastUsedAt.Equal(touchedAt) {
		t.Fatalf("TouchTrustedDevice() did not set LastUsedAt correctly: %#v", got.LastUsedAt)
	}

	revokedAt := now.Add(2 * time.Hour)
	if err := s.DeleteTrustedDevice("identity-1", "device-1", revokedAt); err != nil {
		t.Fatalf("DeleteTrustedDevice() error = %v", err)
	}
	got, err = s.GetTrustedDevice("identity-1", "hash-1", "pref")
	if err != nil {
		t.Fatalf("GetTrustedDevice(after revoke) error = %v", err)
	}
	if got.RevokedAt == nil || !got.RevokedAt.Equal(revokedAt) {
		t.Fatalf("DeleteTrustedDevice() did not set RevokedAt correctly: %#v", got.RevokedAt)
	}

	if _, err := s.GetTrustedDevice("identity-1", "missing", "pref"); !errors.Is(err, ErrTrustedDeviceNotFound) {
		t.Fatalf("GetTrustedDevice(missing) error = %v, want %v", err, ErrTrustedDeviceNotFound)
	}
	if err := s.TouchTrustedDevice("identity-1", "missing", now); !errors.Is(err, ErrTrustedDeviceNotFound) {
		t.Fatalf("TouchTrustedDevice(missing) error = %v, want %v", err, ErrTrustedDeviceNotFound)
	}
	if err := s.DeleteTrustedDevice("identity-1", "missing", now); !errors.Is(err, ErrTrustedDeviceNotFound) {
		t.Fatalf("DeleteTrustedDevice(missing) error = %v, want %v", err, ErrTrustedDeviceNotFound)
	}
}

func TestDeleteAllIdentityDataAndFactors(t *testing.T) {
	s := New()
	now := time.Now().UTC().Round(0)

	_ = s.SaveEnrollment(Enrollment{ID: "enroll-1", IdentityID: "identity-1", ExpiresAt: now.Add(time.Minute), CreatedAt: now})
	_ = s.SaveEnrollment(Enrollment{ID: "enroll-2", IdentityID: "identity-2", ExpiresAt: now.Add(time.Minute), CreatedAt: now})
	_ = s.UpsertTOTPFactor(TOTPFactor{IdentityID: "identity-1", SecretCiphertext: "cipher", EnrolledAt: now, CreatedAt: now, UpdatedAt: now})
	_ = s.ReplaceBackupCodes("identity-1", []BackupCode{{ID: "code-1", IdentityID: "identity-1", CodeHash: "h1", BatchID: "b1", CreatedAt: now}})
	_ = s.SaveTrustedDevice(TrustedDevice{ID: "device-1", IdentityID: "identity-1", TokenHash: "hash", TokenPrefix: "pref", ExpiresAt: now.Add(time.Hour), CreatedAt: now})

	factors, err := s.ListFactorsByIdentity("identity-1")
	if err != nil {
		t.Fatalf("ListFactorsByIdentity() error = %v", err)
	}
	if len(factors) != 1 || factors[0].Method != "totp" {
		t.Fatalf("ListFactorsByIdentity() = %#v, want one totp factor", factors)
	}

	if err := s.DeleteAllIdentityData("identity-1"); err != nil {
		t.Fatalf("DeleteAllIdentityData() error = %v", err)
	}
	if _, err := s.GetEnrollment("enroll-1"); !errors.Is(err, ErrEnrollmentNotFound) {
		t.Fatalf("GetEnrollment(after DeleteAllIdentityData) error = %v, want %v", err, ErrEnrollmentNotFound)
	}
	if _, err := s.GetEnrollment("enroll-2"); err != nil {
		t.Fatalf("DeleteAllIdentityData() should not remove other identities, got %v", err)
	}
	emptyFactors, err := s.ListFactorsByIdentity("identity-1")
	if err != nil {
		t.Fatalf("ListFactorsByIdentity(after delete) error = %v", err)
	}
	if len(emptyFactors) != 0 {
		t.Fatalf("ListFactorsByIdentity(after delete) len = %d, want 0", len(emptyFactors))
	}
}

func TestNullTimeHelpers(t *testing.T) {
	if got := nullTime(time.Time{}); got != nil {
		t.Fatalf("nullTime(zero) = %#v, want nil", got)
	}
	now := time.Now()
	got := nullTime(now)
	if _, ok := got.(time.Time); !ok {
		t.Fatalf("nullTime(non-zero) expected time.Time, got %T", got)
	}

	if got := nullTimePtr(nil); got != nil {
		t.Fatalf("nullTimePtr(nil) = %#v, want nil", got)
	}
	zero := time.Time{}
	if got := nullTimePtr(&zero); got != nil {
		t.Fatalf("nullTimePtr(zero) = %#v, want nil", got)
	}
	if got := nullTimePtr(&now); got == nil {
		t.Fatalf("nullTimePtr(non-zero) = nil, want time.Time")
	}
}
