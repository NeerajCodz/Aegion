package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestSendVerificationCode_AdditionalErrorPaths(t *testing.T) {
	identityID := uuid.New()

	t.Run("generic rate limit backend error is propagated", func(t *testing.T) {
		st := newMemoryStore()
		st.checkRateLimitErr = errors.New("rate backend unavailable")
		svc := makeService(st, nil)

		err := svc.SendVerificationCode(context.Background(), "user@example.com", identityID)
		if err == nil || err.Error() != "rate backend unavailable" {
			t.Fatalf("expected propagated rate-limit backend error, got %v", err)
		}
	})

	t.Run("create error is propagated", func(t *testing.T) {
		st := newMemoryStore()
		st.createErr = errors.New("insert failed")
		svc := makeService(st, nil)

		err := svc.SendVerificationCode(context.Background(), "user@example.com", identityID)
		if err == nil || err.Error() != "insert failed" {
			t.Fatalf("expected propagated create error, got %v", err)
		}
	})

	t.Run("courier error is propagated", func(t *testing.T) {
		st := newMemoryStore()
		svc := makeService(st, &mockCourier{
			sendFn: func(context.Context, string, string, string) error {
				return errors.New("smtp down")
			},
		})

		err := svc.SendVerificationCode(context.Background(), "user@example.com", identityID)
		if err == nil || err.Error() != "smtp down" {
			t.Fatalf("expected propagated courier error, got %v", err)
		}
	})
}
