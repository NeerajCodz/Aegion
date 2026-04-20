package bcryptcompat

import (
	"errors"
	"testing"
)

func TestGenerateAndComparePassword(t *testing.T) {
	hash, err := GenerateFromPassword([]byte("correct horse battery staple"), DefaultCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword() error = %v", err)
	}
	if len(hash) == 0 {
		t.Fatalf("GenerateFromPassword() returned empty hash")
	}
	if err := CompareHashAndPassword(hash, []byte("correct horse battery staple")); err != nil {
		t.Fatalf("CompareHashAndPassword(valid) error = %v", err)
	}
	if err := CompareHashAndPassword(hash, []byte("wrong")); !errors.Is(err, ErrMismatchedHashAndPassword) {
		t.Fatalf("CompareHashAndPassword(invalid) error = %v, want %v", err, ErrMismatchedHashAndPassword)
	}
}

func TestCostConstants(t *testing.T) {
	if MinCost != 4 || DefaultCost != 10 {
		t.Fatalf("unexpected cost constants: MinCost=%d DefaultCost=%d", MinCost, DefaultCost)
	}
}
