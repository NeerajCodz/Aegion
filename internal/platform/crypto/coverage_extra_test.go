package crypto

import (
	"errors"
	"testing"
)

func TestCrypto_HashVerifyAndDecryptSuccessPaths(t *testing.T) {
	password := "GoodPass1!"

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}

	match, err := VerifyPassword(password, hash)
	if err != nil {
		t.Fatalf("VerifyPassword (correct password) failed: %v", err)
	}
	if !match {
		t.Fatalf("expected password verification to succeed")
	}

	match, err = VerifyPassword(password+"x", hash)
	if err != nil {
		t.Fatalf("VerifyPassword (wrong password) failed: %v", err)
	}
	if match {
		t.Fatalf("expected password verification to fail for wrong password")
	}

	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	ciphertext, err := EncryptField(key, []byte("hello"), []byte("aad"))
	if err != nil {
		t.Fatalf("EncryptField failed: %v", err)
	}

	plaintext, err := DecryptField(key, ciphertext, []byte("aad"))
	if err != nil {
		t.Fatalf("DecryptField failed: %v", err)
	}
	if string(plaintext) != "hello" {
		t.Fatalf("unexpected plaintext %q", string(plaintext))
	}
}

func TestGenerateKey_ErrorPathViaSeam(t *testing.T) {
	orig := generateKeyFn
	generateKeyFn = func([]byte) int { return 1 }
	t.Cleanup(func() { generateKeyFn = orig })

	key, err := GenerateKey()
	if !errors.Is(err, ErrRngFailed) {
		t.Fatalf("expected ErrRngFailed, got %v", err)
	}
	if key != nil {
		t.Fatalf("expected nil key on RNG failure")
	}
}
