package service

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestCheckComplexity_MissingLowerNumberSpecial(t *testing.T) {
	s := New(newMemoryStore(), &mockHasher{}, defaultConfig())

	if err := s.checkComplexity("ALLUPPER1!"); !errors.Is(err, ErrPasswordTooWeak) {
		t.Fatalf("expected uppercase-only password to fail lowercase check, got %v", err)
	}
	if err := s.checkComplexity("NoNumber!"); !errors.Is(err, ErrPasswordTooWeak) {
		t.Fatalf("expected password without number to fail, got %v", err)
	}
	if err := s.checkComplexity("NoSpecial1"); !errors.Is(err, ErrPasswordTooWeak) {
		t.Fatalf("expected password without special char to fail, got %v", err)
	}
}

func TestChangePassword_NewPasswordValidationError(t *testing.T) {
	identityID := uuid.New()
	mem := newMemoryStore()
	mem.seedCredential(identityID, "user@example.com", "old-hash")

	s := New(mem, &mockHasher{
		verifyFn: func(password, hash string) (bool, error) { return true, nil },
	}, defaultConfig())

	err := s.ChangePassword(context.Background(), identityID, "OldPass1!", "short")
	if !errors.Is(err, ErrPasswordTooShort) {
		t.Fatalf("expected ErrPasswordTooShort, got %v", err)
	}
}

func TestValidatePassword_HIBPBreachedPath(t *testing.T) {
	password := "password"
	sum := sha1.Sum([]byte(password))
	hash := strings.ToUpper(hex.EncodeToString(sum[:]))
	suffix := hash[5:]

	origTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(suffix + ":42\n")),
		}, nil
	})
	defer func() { http.DefaultTransport = origTransport }()

	s := New(newMemoryStore(), &mockHasher{}, Config{
		MinLength:        8,
		RequireUppercase: false,
		RequireLowercase: false,
		RequireNumber:    false,
		RequireSpecial:   false,
		HIBPEnabled:      true,
		HistoryCount:     1,
	})

	err := s.ValidatePassword(context.Background(), password, "")
	if !errors.Is(err, ErrPasswordBreached) {
		t.Fatalf("expected ErrPasswordBreached, got %v", err)
	}
}
