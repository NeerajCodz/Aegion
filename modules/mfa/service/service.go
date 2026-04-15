package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"
	"github.com/aegion/aegion/internal/platform/secrettoken"
	mfapb "github.com/aegion/aegion/internal/proto/mfa/v1"
	"github.com/aegion/aegion/modules/mfa/store"
	"github.com/google/uuid"
)

var (
	ErrInvalidIdentity      = errors.New("identity_id is required")
	ErrInvalidEnrollment    = errors.New("enrollment is invalid or expired")
	ErrInvalidTOTPCode      = errors.New("totp code is invalid")
	ErrInvalidBackupCode    = errors.New("backup code is invalid")
	ErrCipherKeyRequired    = errors.New("cipher key is required")
	ErrTrustedDeviceToken   = errors.New("trusted device token is invalid")
	errTrustedDeviceEntropy = errors.New("failed to generate trusted device token")
	errSecretEntropy        = errors.New("failed to generate totp secret")
)

const trustedDeviceLookupPrefixLength = secrettoken.DefaultLookupPrefixLength

type Config struct {
	Issuer                 string
	EnrollmentTTL          time.Duration
	TOTPPeriod             time.Duration
	TOTPDigits             int
	TOTPAllowedTimeWindows int
	BackupCodeCount        int
	TrustedDeviceTTL       time.Duration
	CipherKey              []byte
}

type Repository interface {
	SaveEnrollment(enrollment store.Enrollment) error
	GetEnrollment(enrollmentID string) (store.Enrollment, error)
	DeleteEnrollment(enrollmentID string) error
	UpsertTOTPFactor(factor store.TOTPFactor) error
	GetTOTPFactor(identityID string) (store.TOTPFactor, error)
	UpdateTOTPLastUsed(identityID string, usedAt time.Time) error
	ReplaceBackupCodes(identityID string, codes []store.BackupCode) error
	ListBackupCodes(identityID string) ([]store.BackupCode, error)
	MarkBackupCodeUsed(identityID, codeID string, usedAt time.Time) error
	SaveTrustedDevice(device store.TrustedDevice) error
	GetTrustedDevice(identityID, tokenHash, tokenPrefix string) (store.TrustedDevice, error)
	TouchTrustedDevice(identityID, deviceID string, touchedAt time.Time) error
	DeleteTrustedDevice(identityID, deviceID string, revokedAt time.Time) error
	DeleteAllIdentityData(identityID string) error
	ListFactorsByIdentity(identityID string) ([]store.Factor, error)
}

// Service contains MFA business logic.
type Service struct {
	repo Repository
	cfg  Config
}

type TOTPEnrollmentStartResponse struct {
	EnrollmentID string `json:"enrollment_id"`
	Secret       string `json:"secret"`
	OTPAuthURL   string `json:"otpauth_url"`
	ExpiresIn    int    `json:"expires_in"`
}

type TOTPEnrollmentFinishRequest struct {
	IdentityID   string `json:"identity_id"`
	EnrollmentID string `json:"enrollment_id"`
	Code         string `json:"code"`
}

type TOTPEnrollmentFinishResponse struct {
	BackupCodes []string `json:"backup_codes"`
}

// New creates a new MFA service.
func New(repo Repository, cfg Config) *Service {
	if cfg.Issuer == "" {
		cfg.Issuer = "Aegion"
	}
	if cfg.EnrollmentTTL <= 0 {
		cfg.EnrollmentTTL = 10 * time.Minute
	}
	if cfg.TOTPPeriod <= 0 {
		cfg.TOTPPeriod = 30 * time.Second
	}
	if cfg.TOTPDigits <= 0 {
		cfg.TOTPDigits = 6
	}
	if cfg.TOTPAllowedTimeWindows <= 0 {
		cfg.TOTPAllowedTimeWindows = 1
	}
	if cfg.BackupCodeCount <= 0 {
		cfg.BackupCodeCount = 12
	}
	if cfg.TrustedDeviceTTL <= 0 {
		cfg.TrustedDeviceTTL = 30 * 24 * time.Hour
	}
	cfg.CipherKey = cloneBytes(cfg.CipherKey)
	return &Service{repo: repo, cfg: cfg}
}

func (s *Service) StartTOTPEnrollment(_ context.Context, identityID, accountName string) (*TOTPEnrollmentStartResponse, error) {
	identityID = strings.TrimSpace(identityID)
	if identityID == "" {
		return nil, ErrInvalidIdentity
	}
	if len(s.cfg.CipherKey) != platformcrypto.KeySize {
		return nil, ErrCipherKeyRequired
	}

	secret, err := generateTOTPSecret()
	if err != nil {
		return nil, errSecretEntropy
	}

	enrollmentID := uuid.NewString()
	ciphertext, err := platformcrypto.EncryptField(s.cfg.CipherKey, []byte(secret), []byte("mfa-enrollment:"+enrollmentID))
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	if err := s.repo.SaveEnrollment(store.Enrollment{
		ID:               enrollmentID,
		IdentityID:       identityID,
		SecretCiphertext: ciphertext,
		ExpiresAt:        now.Add(s.cfg.EnrollmentTTL),
		CreatedAt:        now,
	}); err != nil {
		return nil, err
	}

	accountName = strings.TrimSpace(accountName)
	if accountName == "" {
		accountName = identityID
	}
	return &TOTPEnrollmentStartResponse{
		EnrollmentID: enrollmentID,
		Secret:       secret,
		OTPAuthURL:   buildOTPAuthURL(s.cfg.Issuer, accountName, secret, s.cfg.TOTPDigits, int(s.cfg.TOTPPeriod.Seconds())),
		ExpiresIn:    int(s.cfg.EnrollmentTTL.Seconds()),
	}, nil
}

func (s *Service) CompleteTOTPEnrollment(_ context.Context, req *TOTPEnrollmentFinishRequest) (*TOTPEnrollmentFinishResponse, error) {
	if req == nil || strings.TrimSpace(req.IdentityID) == "" {
		return nil, ErrInvalidIdentity
	}
	if len(s.cfg.CipherKey) != platformcrypto.KeySize {
		return nil, ErrCipherKeyRequired
	}

	enrollment, err := s.repo.GetEnrollment(strings.TrimSpace(req.EnrollmentID))
	if err != nil {
		return nil, ErrInvalidEnrollment
	}
	if enrollment.IdentityID != strings.TrimSpace(req.IdentityID) || time.Now().UTC().After(enrollment.ExpiresAt) {
		return nil, ErrInvalidEnrollment
	}

	secret, err := s.decryptEnrollmentSecret(enrollment)
	if err != nil {
		return nil, err
	}
	if !s.verifyTOTP(secret, req.Code, time.Now().UTC()) {
		return nil, ErrInvalidTOTPCode
	}

	now := time.Now().UTC()
	factorCiphertext, err := platformcrypto.EncryptField(s.cfg.CipherKey, []byte(secret), []byte("mfa-totp:"+enrollment.IdentityID))
	if err != nil {
		return nil, err
	}
	if err := s.repo.UpsertTOTPFactor(store.TOTPFactor{
		IdentityID:       enrollment.IdentityID,
		SecretCiphertext: factorCiphertext,
		EnrolledAt:       now,
		LastUsedAt:       now,
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		return nil, err
	}
	if err := s.repo.DeleteEnrollment(enrollment.ID); err != nil {
		return nil, err
	}

	backupCodes, err := s.generateAndStoreBackupCodes(enrollment.IdentityID)
	if err != nil {
		return nil, err
	}
	return &TOTPEnrollmentFinishResponse{BackupCodes: backupCodes}, nil
}

func (s *Service) VerifyTOTP(_ context.Context, identityID, code string) error {
	identityID = strings.TrimSpace(identityID)
	if identityID == "" {
		return ErrInvalidIdentity
	}

	factor, err := s.repo.GetTOTPFactor(identityID)
	if err != nil {
		return ErrInvalidTOTPCode
	}
	secret, err := platformcrypto.DecryptField(s.cfg.CipherKey, factor.SecretCiphertext, []byte("mfa-totp:"+identityID))
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if !s.verifyTOTP(string(secret), code, now) {
		return ErrInvalidTOTPCode
	}
	return s.repo.UpdateTOTPLastUsed(identityID, now)
}

func (s *Service) VerifyBackupCode(_ context.Context, identityID, code string) error {
	identityID = strings.TrimSpace(identityID)
	if identityID == "" {
		return ErrInvalidIdentity
	}
	codes, err := s.repo.ListBackupCodes(identityID)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	for _, candidate := range codes {
		if candidate.UsedAt != nil {
			continue
		}
		ok, verifyErr := platformcrypto.VerifyPassword(normalizeBackupCode(code), candidate.CodeHash)
		if verifyErr != nil {
			return verifyErr
		}
		if !ok {
			continue
		}
		return s.repo.MarkBackupCodeUsed(identityID, candidate.ID, now)
	}

	return ErrInvalidBackupCode
}

func (s *Service) HasEnrolledFactor(_ context.Context, identityID string) (bool, error) {
	factors, err := s.repo.ListFactorsByIdentity(strings.TrimSpace(identityID))
	if err != nil {
		return false, err
	}
	return len(factors) > 0, nil
}

func (s *Service) RegenerateBackupCodes(_ context.Context, identityID string) ([]string, error) {
	identityID = strings.TrimSpace(identityID)
	if identityID == "" {
		return nil, ErrInvalidIdentity
	}
	if _, err := s.repo.GetTOTPFactor(identityID); err != nil {
		return nil, ErrInvalidIdentity
	}
	return s.generateAndStoreBackupCodes(identityID)
}

func (s *Service) RememberTrustedDevice(_ context.Context, identityID, label string) (string, time.Time, error) {
	identityID = strings.TrimSpace(identityID)
	if identityID == "" {
		return "", time.Time{}, ErrInvalidIdentity
	}

	token, err := randomTrustedDeviceToken()
	if err != nil {
		return "", time.Time{}, errTrustedDeviceEntropy
	}
	now := time.Now().UTC()
	expiresAt := now.Add(s.cfg.TrustedDeviceTTL)
	if err := s.repo.SaveTrustedDevice(store.TrustedDevice{
		ID:          uuid.NewString(),
		IdentityID:  identityID,
		TokenHash:   secrettoken.Hash(token),
		TokenPrefix: secrettoken.Prefix(token, trustedDeviceLookupPrefixLength),
		Label:       strings.TrimSpace(label),
		ExpiresAt:   expiresAt,
		CreatedAt:   now,
	}); err != nil {
		return "", time.Time{}, err
	}
	return token, expiresAt, nil
}

func (s *Service) ValidateTrustedDevice(_ context.Context, identityID, token string) (bool, error) {
	identityID = strings.TrimSpace(identityID)
	token = strings.TrimSpace(token)
	if identityID == "" || token == "" {
		return false, nil
	}

	device, err := s.repo.GetTrustedDevice(identityID, secrettoken.Hash(token), secrettoken.Prefix(token, trustedDeviceLookupPrefixLength))
	if err != nil {
		if errors.Is(err, store.ErrTrustedDeviceNotFound) {
			return false, nil
		}
		return false, err
	}
	if device.RevokedAt != nil || time.Now().UTC().After(device.ExpiresAt) {
		return false, nil
	}
	if err := s.repo.TouchTrustedDevice(identityID, device.ID, time.Now().UTC()); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) RevokeTrustedDevice(_ context.Context, identityID, token string) error {
	identityID = strings.TrimSpace(identityID)
	token = strings.TrimSpace(token)
	if identityID == "" || token == "" {
		return ErrTrustedDeviceToken
	}

	device, err := s.repo.GetTrustedDevice(identityID, secrettoken.Hash(token), secrettoken.Prefix(token, trustedDeviceLookupPrefixLength))
	if err != nil {
		return ErrTrustedDeviceToken
	}
	return s.repo.DeleteTrustedDevice(identityID, device.ID, time.Now().UTC())
}

func (s *Service) ResetIdentity(_ context.Context, identityID string) error {
	identityID = strings.TrimSpace(identityID)
	if identityID == "" {
		return ErrInvalidIdentity
	}
	return s.repo.DeleteAllIdentityData(identityID)
}

func (s *Service) GetStatus(_ context.Context, identityID string) (*mfapb.MFAStatusResponse, error) {
	factors, err := s.repo.ListFactorsByIdentity(strings.TrimSpace(identityID))
	if err != nil {
		return nil, err
	}
	methods := make([]string, 0, len(factors))
	for _, factor := range factors {
		methods = append(methods, factor.Method)
	}
	return &mfapb.MFAStatusResponse{
		MfaEnrolled:     len(factors) > 0,
		HighestAal:      highestAALForFactors(factors),
		EnrolledMethods: methods,
	}, nil
}

func (s *Service) GetEnrolledFactors(_ context.Context, identityID string) ([]*mfapb.Factor, error) {
	factors, err := s.repo.ListFactorsByIdentity(strings.TrimSpace(identityID))
	if err != nil {
		return nil, err
	}
	result := make([]*mfapb.Factor, 0, len(factors))
	for _, factor := range factors {
		result = append(result, &mfapb.Factor{
			Id:         factor.ID,
			Method:     factor.Method,
			Verified:   factor.Verified,
			EnrolledAt: factor.EnrolledAt.Unix(),
			LastUsedAt: factor.LastUsedAt.Unix(),
		})
	}
	return result, nil
}

func (s *Service) decryptEnrollmentSecret(enrollment store.Enrollment) (string, error) {
	plaintext, err := platformcrypto.DecryptField(s.cfg.CipherKey, enrollment.SecretCiphertext, []byte("mfa-enrollment:"+enrollment.ID))
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func (s *Service) generateAndStoreBackupCodes(identityID string) ([]string, error) {
	plaintextCodes := make([]string, 0, s.cfg.BackupCodeCount)
	storedCodes := make([]store.BackupCode, 0, s.cfg.BackupCodeCount)
	batchID := uuid.NewString()
	now := time.Now().UTC()

	for idx := 0; idx < s.cfg.BackupCodeCount; idx++ {
		plain, err := generateBackupCode()
		if err != nil {
			return nil, err
		}
		hash, err := platformcrypto.HashPassword(normalizeBackupCode(plain))
		if err != nil {
			return nil, err
		}
		plaintextCodes = append(plaintextCodes, plain)
		storedCodes = append(storedCodes, store.BackupCode{
			ID:         uuid.NewString(),
			IdentityID: identityID,
			CodeHash:   hash,
			BatchID:    batchID,
			CreatedAt:  now,
		})
	}

	if err := s.repo.ReplaceBackupCodes(identityID, storedCodes); err != nil {
		return nil, err
	}
	return plaintextCodes, nil
}

func (s *Service) verifyTOTP(secret, code string, now time.Time) bool {
	code = normalizeTOTPCode(code)
	if len(code) != s.cfg.TOTPDigits {
		return false
	}
	decoded, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return false
	}
	step := int64(s.cfg.TOTPPeriod.Seconds())
	counter := now.Unix() / step
	for offset := -s.cfg.TOTPAllowedTimeWindows; offset <= s.cfg.TOTPAllowedTimeWindows; offset++ {
		if generateTOTPCode(decoded, counter+int64(offset), s.cfg.TOTPDigits) == code {
			return true
		}
	}
	return false
}

func generateTOTPSecret() (string, error) {
	buf, err := platformcrypto.RandomBytes(20)
	if err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf), nil
}

func buildOTPAuthURL(issuer, accountName, secret string, digits, period int) string {
	label := url.PathEscape(strings.TrimSpace(issuer) + ":" + strings.TrimSpace(accountName))
	query := url.Values{}
	query.Set("secret", secret)
	query.Set("issuer", strings.TrimSpace(issuer))
	query.Set("algorithm", "SHA1")
	query.Set("digits", fmt.Sprintf("%d", digits))
	query.Set("period", fmt.Sprintf("%d", period))
	return "otpauth://totp/" + label + "?" + query.Encode()
}

func generateTOTPCode(secret []byte, counter int64, digits int) string {
	msg := make([]byte, 8)
	binary.BigEndian.PutUint64(msg, uint64(counter))
	mac := hmac.New(sha1.New, secret)
	_, _ = mac.Write(msg)
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	binaryCode := (int(sum[offset])&0x7f)<<24 |
		(int(sum[offset+1])&0xff)<<16 |
		(int(sum[offset+2])&0xff)<<8 |
		(int(sum[offset+3]) & 0xff)
	modulus := 1
	for idx := 0; idx < digits; idx++ {
		modulus *= 10
	}
	return fmt.Sprintf("%0*d", digits, binaryCode%modulus)
}

func generateBackupCode() (string, error) {
	buf, err := platformcrypto.RandomBytes(8)
	if err != nil {
		return "", err
	}
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf)
	encoded = strings.ToUpper(encoded)
	if len(encoded) < 12 {
		return encoded, nil
	}
	return encoded[:4] + "-" + encoded[4:8] + "-" + encoded[8:12], nil
}

func randomTrustedDeviceToken() (string, error) {
	buf, err := platformcrypto.RandomBytes(32)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func highestAALForFactors(factors []store.Factor) string {
	if len(factors) == 0 {
		return "aal1"
	}
	return "aal2"
}

func normalizeTOTPCode(code string) string {
	code = strings.TrimSpace(code)
	code = strings.ReplaceAll(code, " ", "")
	code = strings.ReplaceAll(code, "-", "")
	return code
}

func normalizeBackupCode(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	code = strings.ReplaceAll(code, " ", "")
	code = strings.ReplaceAll(code, "-", "")
	return code
}

func cloneBytes(value []byte) []byte {
	if len(value) == 0 {
		return nil
	}
	cloned := make([]byte, len(value))
	copy(cloned, value)
	return cloned
}
