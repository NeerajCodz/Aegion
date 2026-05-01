package authtoken

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/aegion/aegion/core/eventbus"
)

func TestRotator_EventPublishFailuresAreNonFatal(t *testing.T) {
	gen, err := NewGenerator(GeneratorConfig{Secret: []byte("secret-one")})
	if err != nil {
		t.Fatalf("NewGenerator failed: %v", err)
	}

	rotator := NewRotator(RotatorConfig{
		Generator:   gen,
		EventBus:    eventbus.New(eventbus.Config{}), // DB unavailable => publish errors are logged
		GracePeriod: 10 * time.Millisecond,
	})

	ctx := context.Background()
	if err := rotator.Rotate(ctx, []byte("secret-two")); err != nil {
		t.Fatalf("Rotate failed: %v", err)
	}

	if err := rotator.RotateWithCallback(ctx, []byte("secret-three"), nil); err != nil {
		t.Fatalf("RotateWithCallback failed: %v", err)
	}

	time.Sleep(30 * time.Millisecond)
	if rotator.IsRotating() {
		t.Fatalf("rotation should be completed")
	}
}

func TestGeneratorValidate_InvalidSignatureEncoding(t *testing.T) {
	gen, err := NewGenerator(GeneratorConfig{
		Secret: []byte("secret-signature-check"),
		TTL:    time.Minute,
	})
	if err != nil {
		t.Fatalf("NewGenerator failed: %v", err)
	}

	moduleID := base64.RawURLEncoding.EncodeToString([]byte("password"))
	timestamp := base64.RawURLEncoding.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	token := moduleID + TokenSeparator + timestamp + TokenSeparator + "not-base64!"

	got, err := gen.Validate(token)
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken, got %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil token on invalid signature encoding")
	}
}
