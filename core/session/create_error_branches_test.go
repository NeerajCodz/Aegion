package session

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestManagerCreate_TokenGenerationFailures(t *testing.T) {
	identityID := uuid.New()
	device := DeviceInfo{UserAgent: "ua", IPAddress: "127.0.0.1"}

	t.Run("fails when primary token entropy fails", func(t *testing.T) {
		m := newBehaviorManager()

		original := readTokenRandom
		readTokenRandom = func([]byte) (int, error) { return 0, errors.New("entropy failure") }
		defer func() { readTokenRandom = original }()

		_, err := m.Create(context.Background(), identityID, AuthMethodPassword, device)
		if !errors.Is(err, errTokenEntropyFailure) {
			t.Fatalf("expected errTokenEntropyFailure, got %v", err)
		}
	})

	t.Run("fails when logout token entropy fails", func(t *testing.T) {
		m := newBehaviorManager()

		original := readTokenRandom
		callCount := 0
		readTokenRandom = func(b []byte) (int, error) {
			callCount++
			if callCount == 2 {
				return 0, errors.New("entropy failure")
			}
			for i := range b {
				b[i] = byte(i)
			}
			return len(b), nil
		}
		defer func() { readTokenRandom = original }()

		_, err := m.Create(context.Background(), identityID, AuthMethodPassword, device)
		if !errors.Is(err, errTokenEntropyFailure) {
			t.Fatalf("expected errTokenEntropyFailure, got %v", err)
		}
		if callCount != 2 {
			t.Fatalf("expected two token entropy reads, got %d", callCount)
		}
	})
}
