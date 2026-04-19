package crypto

import (
	"errors"
	"testing"
)

func TestRandomHelpers(t *testing.T) {
	t.Run("fill random bytes", func(t *testing.T) {
		buf := make([]byte, 17)
		if err := FillRandomBytes(buf); err != nil {
			t.Fatalf("FillRandomBytes() error = %v", err)
		}
		zero := make([]byte, len(buf))
		if ConstantTimeCompare(buf, zero) {
			t.Fatalf("FillRandomBytes() produced all-zero output unexpectedly")
		}
	})

	t.Run("random bytes bounds", func(t *testing.T) {
		if _, err := RandomBytes(-1); !errors.Is(err, ErrInvalidRandomBound) {
			t.Fatalf("RandomBytes(-1) error = %v, want %v", err, ErrInvalidRandomBound)
		}

		b, err := RandomBytes(9)
		if err != nil {
			t.Fatalf("RandomBytes(9) error = %v", err)
		}
		if len(b) != 9 {
			t.Fatalf("RandomBytes(9) len = %d, want 9", len(b))
		}
	})

	t.Run("random int bounds", func(t *testing.T) {
		if _, err := RandomIntN(0); !errors.Is(err, ErrInvalidRandomBound) {
			t.Fatalf("RandomIntN(0) error = %v, want %v", err, ErrInvalidRandomBound)
		}

		for i := 0; i < 32; i++ {
			v, err := RandomIntN(7)
			if err != nil {
				t.Fatalf("RandomIntN(7) error = %v", err)
			}
			if v < 0 || v >= 7 {
				t.Fatalf("RandomIntN(7) = %d, out of range", v)
			}
		}
	})
}

func TestRandomHelpersRNGFailure(t *testing.T) {
	orig := generateKeyFn
	generateKeyFn = func([]byte) int { return 1 }
	t.Cleanup(func() { generateKeyFn = orig })

	if err := FillRandomBytes(make([]byte, 5)); !errors.Is(err, ErrRngFailed) {
		t.Fatalf("FillRandomBytes error = %v, want %v", err, ErrRngFailed)
	}

	if _, err := RandomBytes(5); !errors.Is(err, ErrRngFailed) {
		t.Fatalf("RandomBytes error = %v, want %v", err, ErrRngFailed)
	}

	if _, err := RandomIntN(5); !errors.Is(err, ErrRngFailed) {
		t.Fatalf("RandomIntN error = %v, want %v", err, ErrRngFailed)
	}
}
