package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// TestAdditionalErrorPaths tests non-pgx.ErrNoRows error handling
func TestAdditionalErrorPaths(t *testing.T) {
	ctx := context.Background()
	genericErr := errors.New("database connection lost")

	t.Run("GetAuthCode with non-pgx error", func(t *testing.T) {
		s := NewWithDB(&mockDB{queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return mockRow{err: genericErr}
		}})
		_, err := s.GetAuthCode(ctx, "code-1")
		if err == nil || err == ErrNotFound {
			t.Fatalf("expected generic error, got %v", err)
		}
	})

	t.Run("GetClient with non-pgx error", func(t *testing.T) {
		s := NewWithDB(&mockDB{queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return mockRow{err: genericErr}
		}})
		_, err := s.GetClient(ctx, "client-1")
		if err == nil || err == ErrNotFound {
			t.Fatalf("expected generic error, got %v", err)
		}
	})

	t.Run("GetConsentSession with non-pgx error", func(t *testing.T) {
		s := NewWithDB(&mockDB{queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return mockRow{err: genericErr}
		}})
		_, err := s.GetConsentSession(ctx, "client-1", "identity-1")
		if err == nil || err == ErrNotFound {
			t.Fatalf("expected generic error, got %v", err)
		}
	})

	t.Run("GetLoginChallenge with non-pgx error", func(t *testing.T) {
		s := NewWithDB(&mockDB{queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return mockRow{err: genericErr}
		}})
		_, err := s.GetLoginChallenge(ctx, "challenge-1")
		if err == nil || err == ErrNotFound {
			t.Fatalf("expected generic error, got %v", err)
		}
	})

	t.Run("GetConsentChallenge with non-pgx error", func(t *testing.T) {
		s := NewWithDB(&mockDB{queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return mockRow{err: genericErr}
		}})
		_, err := s.GetConsentChallenge(ctx, "challenge-1")
		if err == nil || err == ErrNotFound {
			t.Fatalf("expected generic error, got %v", err)
		}
	})

	t.Run("GetDeviceCodeByDeviceCode with non-pgx error", func(t *testing.T) {
		s := NewWithDB(&mockDB{queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return mockRow{err: genericErr}
		}})
		_, err := s.GetDeviceCodeByDeviceCode(ctx, "dc-1")
		if err == nil || err == ErrNotFound {
			t.Fatalf("expected generic error, got %v", err)
		}
	})

	t.Run("GetDeviceCodeByUserCode with non-pgx error", func(t *testing.T) {
		s := NewWithDB(&mockDB{queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return mockRow{err: genericErr}
		}})
		_, err := s.GetDeviceCodeByUserCode(ctx, "ABCD-EFGH")
		if err == nil || err == ErrNotFound {
			t.Fatalf("expected generic error, got %v", err)
		}
	})

	t.Run("GetRefreshToken with non-pgx error", func(t *testing.T) {
		s := NewWithDB(&mockDB{queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return mockRow{err: genericErr}
		}})
		_, err := s.GetRefreshToken(ctx, "rt-1")
		if err == nil || err == ErrNotFound {
			t.Fatalf("expected generic error, got %v", err)
		}
	})

	t.Run("GetAccessToken with non-pgx error", func(t *testing.T) {
		s := NewWithDB(&mockDB{queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return mockRow{err: genericErr}
		}})
		_, err := s.GetAccessToken(ctx, "jti-1")
		if err == nil || err == ErrNotFound {
			t.Fatalf("expected generic error, got %v", err)
		}
	})
}

// TestCleanupErrorHandling tests cleanup methods with exec errors
func TestCleanupErrorHandling(t *testing.T) {
	ctx := context.Background()
	execErr := errors.New("cleanup failed")

	t.Run("CleanupExpiredAuthCodes returns error on exec failure", func(t *testing.T) {
		s := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, execErr
		}})
		_, err := s.CleanupExpiredAuthCodes(ctx)
		if err == nil || !strings.Contains(err.Error(), "cleanup failed") {
			t.Fatalf("expected cleanup error, got %v", err)
		}
	})

	t.Run("CleanupExpiredDeviceCodes returns error on exec failure", func(t *testing.T) {
		s := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, execErr
		}})
		_, err := s.CleanupExpiredDeviceCodes(ctx)
		if err == nil || !strings.Contains(err.Error(), "cleanup failed") {
			t.Fatalf("expected cleanup error, got %v", err)
		}
	})

	t.Run("CleanupExpiredJWTAssertions returns error on exec failure", func(t *testing.T) {
		s := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, execErr
		}})
		_, err := s.CleanupExpiredJWTAssertions(ctx)
		if err == nil || !strings.Contains(err.Error(), "cleanup failed") {
			t.Fatalf("expected cleanup error, got %v", err)
		}
	})

	t.Run("CleanupExpiredTokens returns error on exec failure", func(t *testing.T) {
		s := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, execErr
		}})
		_, err := s.CleanupExpiredTokens(ctx)
		if err == nil || !strings.Contains(err.Error(), "cleanup failed") {
			t.Fatalf("expected cleanup error, got %v", err)
		}
	})

	t.Run("CleanupExpiredChallenges returns error on exec failure", func(t *testing.T) {
		s := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, execErr
		}})
		_, err := s.CleanupExpiredChallenges(ctx)
		if err == nil || !strings.Contains(err.Error(), "cleanup failed") {
			t.Fatalf("expected cleanup error, got %v", err)
		}
	})
}

// TestCreateErrorHandling tests create methods with non-duplicate exec errors
func TestCreateErrorHandling(t *testing.T) {
	ctx := context.Background()
	execErr := errors.New("constraint violation")

	t.Run("CreateClient returns generic error on non-duplicate exec failure", func(t *testing.T) {
		s := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, execErr
		}})
		err := s.CreateClient(ctx, &Client{Name: "client-a"})
		if err == nil || err == ErrAlreadyExists {
			t.Fatalf("expected generic exec error, got %v", err)
		}
	})

	t.Run("CreateAuthCode returns generic error on non-duplicate exec failure", func(t *testing.T) {
		s := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, execErr
		}})
		err := s.CreateAuthCode(ctx, &AuthCode{})
		if err == nil || err == ErrAlreadyExists {
			t.Fatalf("expected generic exec error, got %v", err)
		}
	})

	t.Run("CreateDeviceCode returns generic error on non-duplicate exec failure", func(t *testing.T) {
		s := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, execErr
		}})
		err := s.CreateDeviceCode(ctx, &DeviceCode{})
		if err == nil || err == ErrAlreadyExists {
			t.Fatalf("expected generic exec error, got %v", err)
		}
	})
}

// TestUpdateErrorHandling tests update methods with generic exec errors
func TestUpdateErrorHandling(t *testing.T) {
	ctx := context.Background()
	execErr := errors.New("update constraint failed")

	t.Run("UpdateClient returns generic error on exec failure", func(t *testing.T) {
		s := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, execErr
		}})
		err := s.UpdateClient(ctx, &Client{ID: "client-1"})
		if err == nil || err == ErrNotFound {
			t.Fatalf("expected generic exec error, got %v", err)
		}
	})

	t.Run("UpdateClientSecret returns generic error on exec failure", func(t *testing.T) {
		s := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, execErr
		}})
		err := s.UpdateClientSecret(ctx, "client-1", "hash")
		if err == nil || err == ErrNotFound {
			t.Fatalf("expected generic exec error, got %v", err)
		}
	})

	t.Run("AcceptLoginChallenge returns generic error on exec failure", func(t *testing.T) {
		s := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, execErr
		}})
		err := s.AcceptLoginChallenge(ctx, "challenge-1", "identity-1", "session-1")
		if err == nil || err == ErrNotFound {
			t.Fatalf("expected generic exec error, got %v", err)
		}
	})

	t.Run("AcceptConsentChallenge returns generic error on exec failure", func(t *testing.T) {
		s := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, execErr
		}})
		err := s.AcceptConsentChallenge(ctx, "challenge-1", []string{"openid"}, []string{"api"}, false, nil)
		if err == nil || err == ErrNotFound {
			t.Fatalf("expected generic exec error, got %v", err)
		}
	})

	t.Run("RejectConsentChallenge returns generic error on exec failure", func(t *testing.T) {
		s := NewWithDB(&mockDB{execFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, execErr
		}})
		err := s.RejectConsentChallenge(ctx, "challenge-1", "access_denied", "User denied")
		if err == nil || err == ErrNotFound {
			t.Fatalf("expected generic exec error, got %v", err)
		}
	})
}
