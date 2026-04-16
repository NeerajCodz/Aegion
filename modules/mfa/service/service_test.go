package service

import (
	"context"
	"encoding/base32"
	"errors"
	"testing"
	"time"

	"github.com/aegion/aegion/modules/mfa/store"
)

func TestTOTPEnrollmentAndVerificationFlow(t *testing.T) {
	repo := store.New()
	svc := New(repo, Config{
		Issuer:                 "Aegion Test",
		EnrollmentTTL:          time.Minute,
		TOTPPeriod:             30 * time.Second,
		TOTPDigits:             6,
		TOTPAllowedTimeWindows: 1,
		BackupCodeCount:        4,
		TrustedDeviceTTL:       time.Hour,
		CipherKey:              []byte("0123456789abcdef0123456789abcdef"),
	})

	start, err := svc.StartTOTPEnrollment(context.Background(), "identity-1", "user@example.com")
	if err != nil {
		t.Fatalf("start enrollment: %v", err)
	}
	if start.Secret == "" || start.EnrollmentID == "" || start.OTPAuthURL == "" {
		t.Fatalf("expected enrollment payload, got %+v", start)
	}

	code := generateTOTPCodeForTest(t, start.Secret, svc.cfg, time.Now().UTC())
	finish, err := svc.CompleteTOTPEnrollment(context.Background(), &TOTPEnrollmentFinishRequest{
		IdentityID:   "identity-1",
		EnrollmentID: start.EnrollmentID,
		Code:         code,
	})
	if err != nil {
		t.Fatalf("complete enrollment: %v", err)
	}
	if len(finish.BackupCodes) != 4 {
		t.Fatalf("expected 4 backup codes, got %d", len(finish.BackupCodes))
	}

	if err := svc.VerifyTOTP(context.Background(), "identity-1", code); err != nil {
		t.Fatalf("verify totp: %v", err)
	}

	if err := svc.VerifyBackupCode(context.Background(), "identity-1", finish.BackupCodes[0]); err != nil {
		t.Fatalf("verify backup code: %v", err)
	}
	if err := svc.VerifyBackupCode(context.Background(), "identity-1", finish.BackupCodes[0]); err == nil {
		t.Fatal("expected reused backup code to fail")
	}

	enrolled, err := svc.HasEnrolledFactor(context.Background(), "identity-1")
	if err != nil {
		t.Fatalf("has enrolled factor: %v", err)
	}
	if !enrolled {
		t.Fatal("expected identity to have enrolled factor")
	}
}

func TestTrustedDeviceLifecycle(t *testing.T) {
	repo := store.New()
	svc := New(repo, Config{
		CipherKey:        []byte("0123456789abcdef0123456789abcdef"),
		TrustedDeviceTTL: time.Hour,
	})

	token, expiresAt, err := svc.RememberTrustedDevice(context.Background(), "identity-1", "browser")
	if err != nil {
		t.Fatalf("remember trusted device: %v", err)
	}
	if token == "" || expiresAt.IsZero() {
		t.Fatalf("expected trusted device token and expiry, got %q %v", token, expiresAt)
	}

	valid, err := svc.ValidateTrustedDevice(context.Background(), "identity-1", token)
	if err != nil {
		t.Fatalf("validate trusted device: %v", err)
	}
	if !valid {
		t.Fatal("expected trusted device token to validate")
	}

	if err := svc.RevokeTrustedDevice(context.Background(), "identity-1", token); err != nil {
		t.Fatalf("revoke trusted device: %v", err)
	}
	valid, err = svc.ValidateTrustedDevice(context.Background(), "identity-1", token)
	if err != nil {
		t.Fatalf("validate revoked trusted device: %v", err)
	}
	if valid {
		t.Fatal("expected revoked trusted device token to fail")
	}
}

func TestGetStatusAndFactors(t *testing.T) {
	repo := store.New()
	svc := New(repo, Config{
		CipherKey: []byte("0123456789abcdef0123456789abcdef"),
	})
	if err := repo.UpsertTOTPFactor(store.TOTPFactor{
		IdentityID:       "identity-1",
		SecretCiphertext: "ciphertext",
		EnrolledAt:       time.Unix(1700000000, 0).UTC(),
		LastUsedAt:       time.Unix(1700000300, 0).UTC(),
		CreatedAt:        time.Unix(1700000000, 0).UTC(),
		UpdatedAt:        time.Unix(1700000300, 0).UTC(),
	}); err != nil {
		t.Fatalf("upsert factor: %v", err)
	}

	status, err := svc.GetStatus(context.Background(), "identity-1")
	if err != nil {
		t.Fatalf("get status: %v", err)
	}
	if !status.GetMfaEnrolled() || status.GetHighestAal() != "aal2" {
		t.Fatalf("unexpected status: %+v", status)
	}

	factors, err := svc.GetEnrolledFactors(context.Background(), "identity-1")
	if err != nil {
		t.Fatalf("get factors: %v", err)
	}
	if len(factors) != 1 || factors[0].GetMethod() != "totp" {
		t.Fatalf("unexpected factors: %+v", factors)
	}
}

func TestRegenerateBackupCodesAndResetIdentity(t *testing.T) {
	repo := store.New()
	svc := New(repo, Config{
		EnrollmentTTL:          time.Minute,
		TOTPPeriod:             30 * time.Second,
		TOTPDigits:             6,
		TOTPAllowedTimeWindows: 1,
		BackupCodeCount:        3,
		TrustedDeviceTTL:       time.Hour,
		CipherKey:              []byte("0123456789abcdef0123456789abcdef"),
	})

	if _, err := svc.RegenerateBackupCodes(context.Background(), " "); !errors.Is(err, ErrInvalidIdentity) {
		t.Fatalf("expected invalid identity error, got %v", err)
	}
	if _, err := svc.RegenerateBackupCodes(context.Background(), "missing"); !errors.Is(err, ErrInvalidIdentity) {
		t.Fatalf("expected invalid identity for missing factor, got %v", err)
	}

	start, err := svc.StartTOTPEnrollment(context.Background(), "identity-regen", "regen@example.com")
	if err != nil {
		t.Fatalf("start enrollment: %v", err)
	}
	code := generateTOTPCodeForTest(t, start.Secret, svc.cfg, time.Now().UTC())
	if _, err := svc.CompleteTOTPEnrollment(context.Background(), &TOTPEnrollmentFinishRequest{
		IdentityID:   "identity-regen",
		EnrollmentID: start.EnrollmentID,
		Code:         code,
	}); err != nil {
		t.Fatalf("complete enrollment: %v", err)
	}

	regenerated, err := svc.RegenerateBackupCodes(context.Background(), "identity-regen")
	if err != nil {
		t.Fatalf("regenerate backup codes: %v", err)
	}
	if len(regenerated) != 3 {
		t.Fatalf("expected 3 backup codes, got %d", len(regenerated))
	}

	if err := svc.ResetIdentity(context.Background(), " "); !errors.Is(err, ErrInvalidIdentity) {
		t.Fatalf("expected invalid identity from reset, got %v", err)
	}
	if err := svc.ResetIdentity(context.Background(), "identity-regen"); err != nil {
		t.Fatalf("reset identity: %v", err)
	}
	enrolled, err := svc.HasEnrolledFactor(context.Background(), "identity-regen")
	if err != nil {
		t.Fatalf("has enrolled factor after reset: %v", err)
	}
	if enrolled {
		t.Fatal("expected no enrolled factors after identity reset")
	}
}

func TestTrustedDeviceInputValidationBranches(t *testing.T) {
	svc := New(store.New(), Config{
		CipherKey:        []byte("0123456789abcdef0123456789abcdef"),
		TrustedDeviceTTL: time.Hour,
	})

	valid, err := svc.ValidateTrustedDevice(context.Background(), " ", "token")
	if err != nil || valid {
		t.Fatalf("expected invalid identity to return false,nil; got valid=%v err=%v", valid, err)
	}
	valid, err = svc.ValidateTrustedDevice(context.Background(), "identity-1", " ")
	if err != nil || valid {
		t.Fatalf("expected empty token to return false,nil; got valid=%v err=%v", valid, err)
	}

	if err := svc.RevokeTrustedDevice(context.Background(), "", "token"); !errors.Is(err, ErrTrustedDeviceToken) {
		t.Fatalf("expected trusted-device token error for empty identity, got %v", err)
	}
	if err := svc.RevokeTrustedDevice(context.Background(), "identity-1", "missing"); !errors.Is(err, ErrTrustedDeviceToken) {
		t.Fatalf("expected trusted-device token error for unknown token, got %v", err)
	}
}

func generateTOTPCodeForTest(t *testing.T, secret string, cfg Config, now time.Time) string {
	t.Helper()
	decoded, err := base32Decode(secret)
	if err != nil {
		t.Fatalf("decode secret: %v", err)
	}
	return generateTOTPCode(decoded, now.Unix()/int64(cfg.TOTPPeriod.Seconds()), cfg.TOTPDigits)
}

func base32Decode(secret string) ([]byte, error) {
	return base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
}
