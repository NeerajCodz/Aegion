package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"
	"github.com/aegion/aegion/modules/mfa/store"
)

type mfaRepoStub struct {
	base *store.Store

	saveEnrollmentFn      func(store.Enrollment) error
	getEnrollmentFn       func(string) (store.Enrollment, error)
	deleteEnrollmentFn    func(string) error
	upsertTOTPFactorFn    func(store.TOTPFactor) error
	getTOTPFactorFn       func(string) (store.TOTPFactor, error)
	updateTOTPLastUsedFn  func(string, time.Time) error
	replaceBackupCodesFn  func(string, []store.BackupCode) error
	listBackupCodesFn     func(string) ([]store.BackupCode, error)
	markBackupCodeUsedFn  func(string, string, time.Time) error
	saveTrustedDeviceFn   func(store.TrustedDevice) error
	getTrustedDeviceFn    func(string, string, string) (store.TrustedDevice, error)
	touchTrustedDeviceFn  func(string, string, time.Time) error
	deleteTrustedDeviceFn func(string, string, time.Time) error
	deleteAllIdentityFn   func(string) error
	listFactorsFn         func(string) ([]store.Factor, error)
}

func newMFARepoStub() *mfaRepoStub {
	return &mfaRepoStub{base: store.New()}
}

func (m *mfaRepoStub) SaveEnrollment(e store.Enrollment) error {
	if m.saveEnrollmentFn != nil {
		return m.saveEnrollmentFn(e)
	}
	return m.base.SaveEnrollment(e)
}
func (m *mfaRepoStub) GetEnrollment(id string) (store.Enrollment, error) {
	if m.getEnrollmentFn != nil {
		return m.getEnrollmentFn(id)
	}
	return m.base.GetEnrollment(id)
}
func (m *mfaRepoStub) DeleteEnrollment(id string) error {
	if m.deleteEnrollmentFn != nil {
		return m.deleteEnrollmentFn(id)
	}
	return m.base.DeleteEnrollment(id)
}
func (m *mfaRepoStub) UpsertTOTPFactor(f store.TOTPFactor) error {
	if m.upsertTOTPFactorFn != nil {
		return m.upsertTOTPFactorFn(f)
	}
	return m.base.UpsertTOTPFactor(f)
}
func (m *mfaRepoStub) GetTOTPFactor(identityID string) (store.TOTPFactor, error) {
	if m.getTOTPFactorFn != nil {
		return m.getTOTPFactorFn(identityID)
	}
	return m.base.GetTOTPFactor(identityID)
}
func (m *mfaRepoStub) UpdateTOTPLastUsed(identityID string, usedAt time.Time) error {
	if m.updateTOTPLastUsedFn != nil {
		return m.updateTOTPLastUsedFn(identityID, usedAt)
	}
	return m.base.UpdateTOTPLastUsed(identityID, usedAt)
}
func (m *mfaRepoStub) ReplaceBackupCodes(identityID string, codes []store.BackupCode) error {
	if m.replaceBackupCodesFn != nil {
		return m.replaceBackupCodesFn(identityID, codes)
	}
	return m.base.ReplaceBackupCodes(identityID, codes)
}
func (m *mfaRepoStub) ListBackupCodes(identityID string) ([]store.BackupCode, error) {
	if m.listBackupCodesFn != nil {
		return m.listBackupCodesFn(identityID)
	}
	return m.base.ListBackupCodes(identityID)
}
func (m *mfaRepoStub) MarkBackupCodeUsed(identityID, codeID string, usedAt time.Time) error {
	if m.markBackupCodeUsedFn != nil {
		return m.markBackupCodeUsedFn(identityID, codeID, usedAt)
	}
	return m.base.MarkBackupCodeUsed(identityID, codeID, usedAt)
}
func (m *mfaRepoStub) SaveTrustedDevice(device store.TrustedDevice) error {
	if m.saveTrustedDeviceFn != nil {
		return m.saveTrustedDeviceFn(device)
	}
	return m.base.SaveTrustedDevice(device)
}
func (m *mfaRepoStub) GetTrustedDevice(identityID, tokenHash, tokenPrefix string) (store.TrustedDevice, error) {
	if m.getTrustedDeviceFn != nil {
		return m.getTrustedDeviceFn(identityID, tokenHash, tokenPrefix)
	}
	return m.base.GetTrustedDevice(identityID, tokenHash, tokenPrefix)
}
func (m *mfaRepoStub) TouchTrustedDevice(identityID, deviceID string, touchedAt time.Time) error {
	if m.touchTrustedDeviceFn != nil {
		return m.touchTrustedDeviceFn(identityID, deviceID, touchedAt)
	}
	return m.base.TouchTrustedDevice(identityID, deviceID, touchedAt)
}
func (m *mfaRepoStub) DeleteTrustedDevice(identityID, deviceID string, revokedAt time.Time) error {
	if m.deleteTrustedDeviceFn != nil {
		return m.deleteTrustedDeviceFn(identityID, deviceID, revokedAt)
	}
	return m.base.DeleteTrustedDevice(identityID, deviceID, revokedAt)
}
func (m *mfaRepoStub) DeleteAllIdentityData(identityID string) error {
	if m.deleteAllIdentityFn != nil {
		return m.deleteAllIdentityFn(identityID)
	}
	return m.base.DeleteAllIdentityData(identityID)
}
func (m *mfaRepoStub) ListFactorsByIdentity(identityID string) ([]store.Factor, error) {
	if m.listFactorsFn != nil {
		return m.listFactorsFn(identityID)
	}
	return m.base.ListFactorsByIdentity(identityID)
}

func TestMFAServiceAdditionalStartCompleteAndVerifyBranches(t *testing.T) {
	const key = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	keyBytes := []byte(key)
	now := time.Now().UTC()

	t.Run("start enrollment validation and repository errors", func(t *testing.T) {
		repo := newMFARepoStub()
		svc := New(repo, Config{CipherKey: keyBytes})

		if _, err := svc.StartTOTPEnrollment(context.Background(), " ", "account"); !errors.Is(err, ErrInvalidIdentity) {
			t.Fatalf("StartTOTPEnrollment(empty identity) = %v", err)
		}
		if _, err := New(repo, Config{CipherKey: []byte("short")}).StartTOTPEnrollment(context.Background(), "identity-1", "account"); !errors.Is(err, ErrCipherKeyRequired) {
			t.Fatalf("StartTOTPEnrollment(invalid key) = %v", err)
		}

		boom := errors.New("save failed")
		repo.saveEnrollmentFn = func(store.Enrollment) error { return boom }
		if _, err := svc.StartTOTPEnrollment(context.Background(), "identity-1", ""); !errors.Is(err, boom) {
			t.Fatalf("StartTOTPEnrollment(save error) = %v", err)
		}
		repo.saveEnrollmentFn = nil

		start, err := svc.StartTOTPEnrollment(context.Background(), "identity-1", "")
		if err != nil {
			t.Fatalf("StartTOTPEnrollment(default account) error = %v", err)
		}
		if !strings.Contains(start.OTPAuthURL, "identity-1") {
			t.Fatalf("StartTOTPEnrollment(default account) otpauth url = %q", start.OTPAuthURL)
		}
	})

	makeEnrollment := func(t *testing.T, identityID, enrollmentID, secret string, expiresAt time.Time) store.Enrollment {
		t.Helper()
		ciphertext, err := platformcrypto.EncryptField(keyBytes, []byte(secret), []byte("mfa-enrollment:"+enrollmentID))
		if err != nil {
			t.Fatalf("encrypt enrollment secret: %v", err)
		}
		return store.Enrollment{
			ID:               enrollmentID,
			IdentityID:       identityID,
			SecretCiphertext: ciphertext,
			ExpiresAt:        expiresAt,
			CreatedAt:        now,
		}
	}

	t.Run("complete enrollment error branches", func(t *testing.T) {
		repo := newMFARepoStub()
		svc := New(repo, Config{
			CipherKey:              keyBytes,
			TOTPPeriod:             30 * time.Second,
			TOTPDigits:             6,
			TOTPAllowedTimeWindows: 1,
			BackupCodeCount:        2,
		})

		if _, err := svc.CompleteTOTPEnrollment(context.Background(), nil); !errors.Is(err, ErrInvalidIdentity) {
			t.Fatalf("CompleteTOTPEnrollment(nil req) = %v", err)
		}
		if _, err := New(repo, Config{CipherKey: []byte("short")}).CompleteTOTPEnrollment(context.Background(), &TOTPEnrollmentFinishRequest{
			IdentityID: "identity-1", EnrollmentID: "enr-1", Code: "000000",
		}); !errors.Is(err, ErrCipherKeyRequired) {
			t.Fatalf("CompleteTOTPEnrollment(invalid key) = %v", err)
		}

		repo.getEnrollmentFn = func(string) (store.Enrollment, error) { return store.Enrollment{}, store.ErrEnrollmentNotFound }
		if _, err := svc.CompleteTOTPEnrollment(context.Background(), &TOTPEnrollmentFinishRequest{
			IdentityID: "identity-1", EnrollmentID: "missing", Code: "000000",
		}); !errors.Is(err, ErrInvalidEnrollment) {
			t.Fatalf("CompleteTOTPEnrollment(missing enrollment) = %v", err)
		}

		secret := "JBSWY3DPEHPK3PXP"
		code := generateTOTPCodeForTest(t, secret, svc.cfg, now)

		repo.getEnrollmentFn = func(string) (store.Enrollment, error) {
			return makeEnrollment(t, "other-identity", "enr-mismatch", secret, now.Add(time.Minute)), nil
		}
		if _, err := svc.CompleteTOTPEnrollment(context.Background(), &TOTPEnrollmentFinishRequest{
			IdentityID: "identity-1", EnrollmentID: "enr-mismatch", Code: code,
		}); !errors.Is(err, ErrInvalidEnrollment) {
			t.Fatalf("CompleteTOTPEnrollment(identity mismatch) = %v", err)
		}

		repo.getEnrollmentFn = func(string) (store.Enrollment, error) {
			return makeEnrollment(t, "identity-1", "enr-expired", secret, now.Add(-time.Minute)), nil
		}
		if _, err := svc.CompleteTOTPEnrollment(context.Background(), &TOTPEnrollmentFinishRequest{
			IdentityID: "identity-1", EnrollmentID: "enr-expired", Code: code,
		}); !errors.Is(err, ErrInvalidEnrollment) {
			t.Fatalf("CompleteTOTPEnrollment(expired enrollment) = %v", err)
		}

		repo.getEnrollmentFn = func(string) (store.Enrollment, error) {
			return store.Enrollment{
				ID:               "enr-bad-cipher",
				IdentityID:       "identity-1",
				SecretCiphertext: "not-a-valid-ciphertext",
				ExpiresAt:        now.Add(time.Minute),
				CreatedAt:        now,
			}, nil
		}
		if _, err := svc.CompleteTOTPEnrollment(context.Background(), &TOTPEnrollmentFinishRequest{
			IdentityID: "identity-1", EnrollmentID: "enr-bad-cipher", Code: code,
		}); err == nil {
			t.Fatalf("CompleteTOTPEnrollment(bad ciphertext) expected error")
		}

		repo.getEnrollmentFn = func(string) (store.Enrollment, error) {
			return makeEnrollment(t, "identity-1", "enr-wrong-code", secret, now.Add(time.Minute)), nil
		}
		if _, err := svc.CompleteTOTPEnrollment(context.Background(), &TOTPEnrollmentFinishRequest{
			IdentityID: "identity-1", EnrollmentID: "enr-wrong-code", Code: "000000",
		}); !errors.Is(err, ErrInvalidTOTPCode) {
			t.Fatalf("CompleteTOTPEnrollment(invalid code) = %v", err)
		}

		repo.getEnrollmentFn = func(string) (store.Enrollment, error) {
			return makeEnrollment(t, "identity-1", "enr-upsert-fail", secret, now.Add(time.Minute)), nil
		}
		repo.upsertTOTPFactorFn = func(store.TOTPFactor) error { return errors.New("upsert failed") }
		if _, err := svc.CompleteTOTPEnrollment(context.Background(), &TOTPEnrollmentFinishRequest{
			IdentityID: "identity-1", EnrollmentID: "enr-upsert-fail", Code: code,
		}); err == nil || !strings.Contains(err.Error(), "upsert failed") {
			t.Fatalf("CompleteTOTPEnrollment(upsert error) = %v", err)
		}
		repo.upsertTOTPFactorFn = nil

		repo.getEnrollmentFn = func(string) (store.Enrollment, error) {
			return makeEnrollment(t, "identity-1", "enr-delete-fail", secret, now.Add(time.Minute)), nil
		}
		repo.deleteEnrollmentFn = func(string) error { return errors.New("delete enrollment failed") }
		if _, err := svc.CompleteTOTPEnrollment(context.Background(), &TOTPEnrollmentFinishRequest{
			IdentityID: "identity-1", EnrollmentID: "enr-delete-fail", Code: code,
		}); err == nil || !strings.Contains(err.Error(), "delete enrollment failed") {
			t.Fatalf("CompleteTOTPEnrollment(delete enrollment error) = %v", err)
		}
		repo.deleteEnrollmentFn = nil

		repo.getEnrollmentFn = func(string) (store.Enrollment, error) {
			return makeEnrollment(t, "identity-1", "enr-replace-fail", secret, now.Add(time.Minute)), nil
		}
		repo.replaceBackupCodesFn = func(string, []store.BackupCode) error { return errors.New("replace backup codes failed") }
		if _, err := svc.CompleteTOTPEnrollment(context.Background(), &TOTPEnrollmentFinishRequest{
			IdentityID: "identity-1", EnrollmentID: "enr-replace-fail", Code: code,
		}); err == nil || !strings.Contains(err.Error(), "replace backup codes failed") {
			t.Fatalf("CompleteTOTPEnrollment(replace backup error) = %v", err)
		}
	})

	t.Run("verify totp and backup code error branches", func(t *testing.T) {
		repo := newMFARepoStub()
		svc := New(repo, Config{
			CipherKey:              keyBytes,
			TOTPPeriod:             30 * time.Second,
			TOTPDigits:             6,
			TOTPAllowedTimeWindows: 1,
		})

		if err := svc.VerifyTOTP(context.Background(), " ", "123456"); !errors.Is(err, ErrInvalidIdentity) {
			t.Fatalf("VerifyTOTP(empty identity) = %v", err)
		}

		repo.getTOTPFactorFn = func(string) (store.TOTPFactor, error) { return store.TOTPFactor{}, store.ErrTOTPFactorNotFound }
		if err := svc.VerifyTOTP(context.Background(), "identity-1", "123456"); !errors.Is(err, ErrInvalidTOTPCode) {
			t.Fatalf("VerifyTOTP(missing factor) = %v", err)
		}

		repo.getTOTPFactorFn = func(string) (store.TOTPFactor, error) {
			return store.TOTPFactor{IdentityID: "identity-1", SecretCiphertext: "bad-ciphertext"}, nil
		}
		if err := svc.VerifyTOTP(context.Background(), "identity-1", "123456"); err == nil {
			t.Fatalf("VerifyTOTP(decrypt error) expected error")
		}

		secret := "JBSWY3DPEHPK3PXP"
		ciphertext, err := platformcrypto.EncryptField(keyBytes, []byte(secret), []byte("mfa-totp:identity-1"))
		if err != nil {
			t.Fatalf("encrypt factor secret: %v", err)
		}
		repo.getTOTPFactorFn = func(string) (store.TOTPFactor, error) {
			return store.TOTPFactor{IdentityID: "identity-1", SecretCiphertext: ciphertext}, nil
		}
		if err := svc.VerifyTOTP(context.Background(), "identity-1", "000000"); !errors.Is(err, ErrInvalidTOTPCode) {
			t.Fatalf("VerifyTOTP(invalid code) = %v", err)
		}

		validCode := generateTOTPCodeForTest(t, secret, svc.cfg, time.Now().UTC())
		repo.updateTOTPLastUsedFn = func(string, time.Time) error { return errors.New("touch factor failed") }
		if err := svc.VerifyTOTP(context.Background(), "identity-1", validCode); err == nil || !strings.Contains(err.Error(), "touch factor failed") {
			t.Fatalf("VerifyTOTP(update last used error) = %v", err)
		}

		if err := svc.VerifyBackupCode(context.Background(), " ", "A"); !errors.Is(err, ErrInvalidIdentity) {
			t.Fatalf("VerifyBackupCode(empty identity) = %v", err)
		}
		repo.listBackupCodesFn = func(string) ([]store.BackupCode, error) { return nil, errors.New("list backup failed") }
		if err := svc.VerifyBackupCode(context.Background(), "identity-1", "CODE"); err == nil || !strings.Contains(err.Error(), "list backup failed") {
			t.Fatalf("VerifyBackupCode(list error) = %v", err)
		}

		repo.listBackupCodesFn = func(string) ([]store.BackupCode, error) {
			return []store.BackupCode{{ID: "code-1", IdentityID: "identity-1", CodeHash: "bad-hash"}}, nil
		}
		if err := svc.VerifyBackupCode(context.Background(), "identity-1", "CODE"); err == nil {
			t.Fatalf("VerifyBackupCode(verify error) expected error")
		}

		codeHash, err := platformcrypto.HashPassword(normalizeBackupCode("ABCD-EFGH-IJKL"))
		if err != nil {
			t.Fatalf("hash backup code: %v", err)
		}
		repo.listBackupCodesFn = func(string) ([]store.BackupCode, error) {
			return []store.BackupCode{{ID: "code-2", IdentityID: "identity-1", CodeHash: codeHash}}, nil
		}
		repo.markBackupCodeUsedFn = func(string, string, time.Time) error { return errors.New("mark used failed") }
		if err := svc.VerifyBackupCode(context.Background(), "identity-1", "ABCD-EFGH-IJKL"); err == nil || !strings.Contains(err.Error(), "mark used failed") {
			t.Fatalf("VerifyBackupCode(mark used error) = %v", err)
		}
	})
}

func TestMFAServiceAdditionalTrustedDeviceAndHelpers(t *testing.T) {
	const key = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	now := time.Now().UTC()

	t.Run("trusted device and factor-list error branches", func(t *testing.T) {
		repo := newMFARepoStub()
		svc := New(repo, Config{CipherKey: []byte(key), TrustedDeviceTTL: time.Hour})

		if _, _, err := svc.RememberTrustedDevice(context.Background(), " ", "browser"); !errors.Is(err, ErrInvalidIdentity) {
			t.Fatalf("RememberTrustedDevice(empty identity) = %v", err)
		}
		repo.saveTrustedDeviceFn = func(store.TrustedDevice) error { return errors.New("save trusted device failed") }
		if _, _, err := svc.RememberTrustedDevice(context.Background(), "identity-1", "browser"); err == nil || !strings.Contains(err.Error(), "save trusted device failed") {
			t.Fatalf("RememberTrustedDevice(save error) = %v", err)
		}

		repo.getTrustedDeviceFn = func(string, string, string) (store.TrustedDevice, error) {
			return store.TrustedDevice{}, store.ErrTrustedDeviceNotFound
		}
		valid, err := svc.ValidateTrustedDevice(context.Background(), "identity-1", "token")
		if err != nil || valid {
			t.Fatalf("ValidateTrustedDevice(not found) valid=%v err=%v", valid, err)
		}
		repo.getTrustedDeviceFn = func(string, string, string) (store.TrustedDevice, error) {
			return store.TrustedDevice{}, errors.New("lookup failed")
		}
		valid, err = svc.ValidateTrustedDevice(context.Background(), "identity-1", "token")
		if err == nil || valid {
			t.Fatalf("ValidateTrustedDevice(unexpected error) valid=%v err=%v", valid, err)
		}
		repo.getTrustedDeviceFn = func(string, string, string) (store.TrustedDevice, error) {
			revoked := now.Add(-time.Minute)
			return store.TrustedDevice{ID: "dev-1", RevokedAt: &revoked, ExpiresAt: now.Add(time.Hour)}, nil
		}
		valid, err = svc.ValidateTrustedDevice(context.Background(), "identity-1", "token")
		if err != nil || valid {
			t.Fatalf("ValidateTrustedDevice(revoked) valid=%v err=%v", valid, err)
		}
		repo.getTrustedDeviceFn = func(string, string, string) (store.TrustedDevice, error) {
			return store.TrustedDevice{ID: "dev-2", ExpiresAt: now.Add(time.Hour)}, nil
		}
		repo.touchTrustedDeviceFn = func(string, string, time.Time) error { return errors.New("touch failed") }
		valid, err = svc.ValidateTrustedDevice(context.Background(), "identity-1", "token")
		if err == nil || valid {
			t.Fatalf("ValidateTrustedDevice(touch error) valid=%v err=%v", valid, err)
		}

		repo.listFactorsFn = func(string) ([]store.Factor, error) { return nil, errors.New("factor list failed") }
		if _, err := svc.HasEnrolledFactor(context.Background(), "identity-1"); err == nil {
			t.Fatalf("HasEnrolledFactor(expected error)")
		}
		if _, err := svc.GetStatus(context.Background(), "identity-1"); err == nil {
			t.Fatalf("GetStatus(expected error)")
		}
		if _, err := svc.GetEnrolledFactors(context.Background(), "identity-1"); err == nil {
			t.Fatalf("GetEnrolledFactors(expected error)")
		}
	})

	t.Run("helper error and fallback branches", func(t *testing.T) {
		svc := New(newMFARepoStub(), Config{CipherKey: []byte(key)})
		if _, err := svc.decryptEnrollmentSecret(store.Enrollment{ID: "enr-1", SecretCiphertext: "bad-cipher"}); err == nil {
			t.Fatalf("decryptEnrollmentSecret(expected error)")
		}

		svc.repo = &mfaRepoStub{
			base: newMFARepoStub().base,
			replaceBackupCodesFn: func(string, []store.BackupCode) error {
				return errors.New("replace failed")
			},
		}
		if _, err := svc.generateAndStoreBackupCodes("identity-1"); err == nil {
			t.Fatalf("generateAndStoreBackupCodes(replace error) expected error")
		}

		if svc.verifyTOTP("JBSWY3DPEHPK3PXP", "123", now) {
			t.Fatalf("verifyTOTP(short code) expected false")
		}
		if svc.verifyTOTP("invalid-secret", "123456", now) {
			t.Fatalf("verifyTOTP(invalid secret) expected false")
		}
		if highestAALForFactors(nil) != "aal1" {
			t.Fatalf("highestAALForFactors(nil) expected aal1")
		}
		if got := cloneBytes(nil); got != nil {
			t.Fatalf("cloneBytes(nil) = %#v", got)
		}
		if got := cloneBytes([]byte("abc")); string(got) != "abc" {
			t.Fatalf("cloneBytes(copy) = %q", string(got))
		}
	})
}

