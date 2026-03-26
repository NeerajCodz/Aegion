// Package service provides magic link authentication business logic.
package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/google/uuid"

	"github.com/aegion/aegion/modules/magic_link/store"
)

var (
	ErrInvalidCode    = errors.New("invalid or expired code")
	ErrRateLimited    = errors.New("too many requests, please wait before trying again")
	ErrRecipientEmpty = errors.New("recipient is required")
)

// Config holds magic link service configuration.
type Config struct {
	BaseURL      string        // Base URL for magic links
	CodeLength   int           // OTP code length
	CodeCharset  string        // Characters to use for OTP
	LinkLifespan time.Duration // Magic link validity
	CodeLifespan time.Duration // OTP code validity
	RateLimit    int           // Max requests per window
	RateWindow   time.Duration // Rate limit window
}

// Courier interface for sending messages.
type Courier interface {
	SendMagicLinkEmail(ctx context.Context, to string, link string, code string) error
}

// Service handles magic link authentication.
type Service struct {
	store   *store.Store
	courier Courier
	config  Config
}

// New creates a new magic link service.
func New(store *store.Store, courier Courier, config Config) *Service {
	if config.CodeLength == 0 {
		config.CodeLength = 6
	}
	if config.CodeCharset == "" {
		config.CodeCharset = "0123456789"
	}
	if config.LinkLifespan == 0 {
		config.LinkLifespan = 15 * time.Minute
	}
	if config.CodeLifespan == 0 {
		config.CodeLifespan = 15 * time.Minute
	}
	if config.RateLimit == 0 {
		config.RateLimit = 5
	}
	if config.RateWindow == 0 {
		config.RateWindow = time.Hour
	}

	store.SetCodeConfig(config.CodeLength, config.CodeCharset)

	return &Service{
		store:   store,
		courier: courier,
		config:  config,
	}
}

// SendLoginCode sends a magic link/OTP code for passwordless login.
func (s *Service) SendLoginCode(ctx context.Context, email string) error {
	if email == "" {
		return ErrRecipientEmpty
	}

	// Check rate limit
	rateLimitKey := fmt.Sprintf("login:%s", email)
	if err := s.store.CheckRateLimit(ctx, rateLimitKey, s.config.RateLimit, s.config.RateWindow); err != nil {
		if errors.Is(err, store.ErrRateLimited) {
			return ErrRateLimited
		}
		return err
	}

	// Invalidate any previous codes
	if err := s.store.InvalidatePrevious(ctx, email, store.CodeTypeLogin); err != nil {
		return err
	}

	// Create new code
	code, err := s.store.Create(ctx, email, store.CodeTypeLogin, nil, s.config.LinkLifespan)
	if err != nil {
		return err
	}

	// Build magic link URL
	link := s.buildMagicLink(code.Token, "login")

	// Send email
	if s.courier != nil {
		return s.courier.SendMagicLinkEmail(ctx, email, link, code.Code)
	}

	return nil
}

// VerifyCode verifies an OTP code and returns the recipient.
func (s *Service) VerifyCode(ctx context.Context, email, otpCode string) (string, *uuid.UUID, error) {
	code, err := s.store.GetByCode(ctx, email, otpCode, store.CodeTypeLogin)
	if err != nil {
		if errors.Is(err, store.ErrCodeNotFound) || errors.Is(err, store.ErrCodeExpired) {
			return "", nil, ErrInvalidCode
		}
		return "", nil, err
	}

	// Mark as used
	if err := s.store.MarkUsed(ctx, code.ID); err != nil {
		if errors.Is(err, store.ErrCodeUsed) {
			return "", nil, ErrInvalidCode
		}
		return "", nil, err
	}

	return code.Recipient, code.IdentityID, nil
}

// VerifyMagicLink verifies a magic link token and returns the recipient.
func (s *Service) VerifyMagicLink(ctx context.Context, token string) (string, *uuid.UUID, error) {
	code, err := s.store.GetByToken(ctx, token)
	if err != nil {
		if errors.Is(err, store.ErrCodeNotFound) || errors.Is(err, store.ErrCodeExpired) {
			return "", nil, ErrInvalidCode
		}
		return "", nil, err
	}

	// Mark as used
	if err := s.store.MarkUsed(ctx, code.ID); err != nil {
		if errors.Is(err, store.ErrCodeUsed) {
			return "", nil, ErrInvalidCode
		}
		return "", nil, err
	}

	return code.Recipient, code.IdentityID, nil
}

// SendVerificationCode sends a verification code for an identity.
func (s *Service) SendVerificationCode(ctx context.Context, email string, identityID uuid.UUID) error {
	if email == "" {
		return ErrRecipientEmpty
	}

	// Check rate limit
	rateLimitKey := fmt.Sprintf("verify:%s", identityID.String())
	if err := s.store.CheckRateLimit(ctx, rateLimitKey, s.config.RateLimit, s.config.RateWindow); err != nil {
		if errors.Is(err, store.ErrRateLimited) {
			return ErrRateLimited
		}
		return err
	}

	// Invalidate previous codes
	if err := s.store.InvalidatePrevious(ctx, email, store.CodeTypeVerification); err != nil {
		return err
	}

	// Create code
	code, err := s.store.Create(ctx, email, store.CodeTypeVerification, &identityID, s.config.CodeLifespan)
	if err != nil {
		return err
	}

	// Build magic link
	link := s.buildMagicLink(code.Token, "verification")

	// Send email
	if s.courier != nil {
		return s.courier.SendMagicLinkEmail(ctx, email, link, code.Code)
	}

	return nil
}

// VerifyVerificationCode verifies a verification code.
func (s *Service) VerifyVerificationCode(ctx context.Context, email, otpCode string) (*uuid.UUID, error) {
	code, err := s.store.GetByCode(ctx, email, otpCode, store.CodeTypeVerification)
	if err != nil {
		if errors.Is(err, store.ErrCodeNotFound) || errors.Is(err, store.ErrCodeExpired) {
			return nil, ErrInvalidCode
		}
		return nil, err
	}

	if code.IdentityID == nil {
		return nil, ErrInvalidCode
	}

	// Mark as used
	if err := s.store.MarkUsed(ctx, code.ID); err != nil {
		if errors.Is(err, store.ErrCodeUsed) {
			return nil, ErrInvalidCode
		}
		return nil, err
	}

	return code.IdentityID, nil
}

// SendRecoveryCode sends a recovery code for password reset.
func (s *Service) SendRecoveryCode(ctx context.Context, email string, identityID uuid.UUID) error {
	if email == "" {
		return ErrRecipientEmpty
	}

	// Check rate limit
	rateLimitKey := fmt.Sprintf("recover:%s", email)
	if err := s.store.CheckRateLimit(ctx, rateLimitKey, 3, s.config.RateWindow); err != nil {
		if errors.Is(err, store.ErrRateLimited) {
			return ErrRateLimited
		}
		return err
	}

	// Invalidate previous codes
	if err := s.store.InvalidatePrevious(ctx, email, store.CodeTypeRecovery); err != nil {
		return err
	}

	// Create code
	code, err := s.store.Create(ctx, email, store.CodeTypeRecovery, &identityID, s.config.CodeLifespan)
	if err != nil {
		return err
	}

	// Build magic link
	link := s.buildMagicLink(code.Token, "recovery")

	// Send email
	if s.courier != nil {
		return s.courier.SendMagicLinkEmail(ctx, email, link, code.Code)
	}

	return nil
}

// VerifyRecoveryCode verifies a recovery code.
func (s *Service) VerifyRecoveryCode(ctx context.Context, email, otpCode string) (*uuid.UUID, error) {
	code, err := s.store.GetByCode(ctx, email, otpCode, store.CodeTypeRecovery)
	if err != nil {
		if errors.Is(err, store.ErrCodeNotFound) || errors.Is(err, store.ErrCodeExpired) {
			return nil, ErrInvalidCode
		}
		return nil, err
	}

	if code.IdentityID == nil {
		return nil, ErrInvalidCode
	}

	// Mark as used
	if err := s.store.MarkUsed(ctx, code.ID); err != nil {
		if errors.Is(err, store.ErrCodeUsed) {
			return nil, ErrInvalidCode
		}
		return nil, err
	}

	return code.IdentityID, nil
}

// Cleanup removes expired codes.
func (s *Service) Cleanup(ctx context.Context) (int64, error) {
	return s.store.Cleanup(ctx)
}

// buildMagicLink constructs a magic link URL.
func (s *Service) buildMagicLink(token, flowType string) string {
	base := s.config.BaseURL
	if base == "" {
		base = "http://localhost:8080"
	}

	u, err := url.Parse(base)
	if err != nil {
		return ""
	}

	u.Path = fmt.Sprintf("/self-service/%s/methods/link/verify", flowType)
	q := u.Query()
	q.Set("token", token)
	u.RawQuery = q.Encode()

	return u.String()
}
