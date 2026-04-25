package security

import (
	"testing"
)

func TestEncryptionManagerCreation(t *testing.T) {
	em, err := NewEncryptionManager(nil)
	if err != nil {
		t.Fatalf("NewEncryptionManager failed: %v", err)
	}

	if em == nil {
		t.Error("EncryptionManager should not be nil")
	}
}

func TestEncryptAndDecryptString(t *testing.T) {
	em, _ := NewEncryptionManager(nil)

	plaintext := "secret data"
	aad := "user123"

	encrypted, err := em.EncryptString(plaintext, aad)
	if err != nil {
		t.Fatalf("EncryptString failed: %v", err)
	}

	decrypted, err := em.DecryptString(encrypted, aad)
	if err != nil {
		t.Fatalf("DecryptString failed: %v", err)
	}

	if decrypted != plaintext {
		t.Errorf("Decrypted text %q does not match original %q", decrypted, plaintext)
	}
}

func TestEncryptAndDecryptBytes(t *testing.T) {
	em, _ := NewEncryptionManager(nil)

	plaintext := []byte("secret bytes")
	aad := []byte("user123")

	encrypted, err := em.EncryptBytes(plaintext, aad)
	if err != nil {
		t.Fatalf("EncryptBytes failed: %v", err)
	}

	decrypted, err := em.DecryptBytes(encrypted, aad)
	if err != nil {
		t.Fatalf("DecryptBytes failed: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Errorf("Decrypted bytes do not match original")
	}
}

func TestDecryptWithWrongAAD(t *testing.T) {
	em, _ := NewEncryptionManager(nil)

	plaintext := "secret data"
	aad := "user123"

	encrypted, _ := em.EncryptString(plaintext, aad)

	// Try to decrypt with wrong AAD
	_, err := em.DecryptString(encrypted, "wrong_aad")
	if err == nil {
		t.Error("DecryptString should fail with wrong AAD")
	}
}

func TestGenerateKey(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	if key == "" {
		t.Error("GenerateKey returned empty key")
	}

	// Should be decodable
	_, err = DecodeKey(key)
	if err != nil {
		t.Fatalf("DecodeKey failed: %v", err)
	}
}

func TestKeyEncoding(t *testing.T) {
	original := []byte("test_key_content!")

	encoded := EncodeKey(original)
	decoded, _ := DecodeKey(encoded)

	if string(decoded) != string(original) {
		t.Error("Decoded key does not match original")
	}
}

func TestGenerateSecret(t *testing.T) {
	secret, err := GenerateSecret(32)
	if err != nil {
		t.Fatalf("GenerateSecret failed: %v", err)
	}

	if secret == "" {
		t.Error("GenerateSecret returned empty secret")
	}

	// Generate multiple secrets, they should be different
	secret2, _ := GenerateSecret(32)
	if secret == secret2 {
		t.Error("Generated secrets should be unique")
	}
}

func TestGenerateSecretInvalidLength(t *testing.T) {
	_, err := GenerateSecret(0)
	if err == nil {
		t.Error("GenerateSecret should fail with length 0")
	}

	_, err = GenerateSecret(-1)
	if err == nil {
		t.Error("GenerateSecret should fail with negative length")
	}
}
