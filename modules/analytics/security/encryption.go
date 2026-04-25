package security

import (
	"crypto/rand"
	"encoding/base64"
	"errors"

	"github.com/aegion/aegion/internal/platform/crypto"
)

var (
	ErrEncryptionFailed = errors.New("encryption failed")
	ErrDecryptionFailed = errors.New("decryption failed")
	ErrInvalidKey       = errors.New("invalid encryption key")
)

// EncryptionManager handles at-rest encryption for sensitive data
type EncryptionManager struct {
	key []byte
}

// NewEncryptionManager creates a new encryption manager
func NewEncryptionManager(key []byte) (*EncryptionManager, error) {
	if len(key) == 0 {
		var err error
		key, err = crypto.GenerateKey()
		if err != nil {
			return nil, err
		}
	}

	if len(key) != crypto.KeySize {
		return nil, ErrInvalidKey
	}

	return &EncryptionManager{
		key: key,
	}, nil
}

// EncryptString encrypts a string value
func (em *EncryptionManager) EncryptString(plaintext string, aad string) (string, error) {
	return em.EncryptBytes([]byte(plaintext), []byte(aad))
}

// EncryptBytes encrypts a byte slice
func (em *EncryptionManager) EncryptBytes(plaintext []byte, aad []byte) (string, error) {
	ciphertext, err := crypto.EncryptField(em.key, plaintext, aad)
	if err != nil {
		return "", ErrEncryptionFailed
	}
	return ciphertext, nil
}

// DecryptString decrypts a string value
func (em *EncryptionManager) DecryptString(ciphertext string, aad string) (string, error) {
	plaintext, err := em.DecryptBytes(ciphertext, []byte(aad))
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// DecryptBytes decrypts a byte slice
func (em *EncryptionManager) DecryptBytes(ciphertext string, aad []byte) ([]byte, error) {
	plaintext, err := crypto.DecryptField(em.key, ciphertext, aad)
	if err != nil {
		return nil, ErrDecryptionFailed
	}
	return plaintext, nil
}

// GenerateKey generates a new encryption key
func GenerateKey() (string, error) {
	key, err := crypto.GenerateKey()
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(key), nil
}

// DecodeKey decodes a base64-encoded key
func DecodeKey(encodedKey string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(encodedKey)
}

// EncodeKey encodes a key to base64
func EncodeKey(key []byte) string {
	return base64.StdEncoding.EncodeToString(key)
}

// GenerateSecret generates a cryptographically secure random secret
func GenerateSecret(length int) (string, error) {
	if length <= 0 {
		return "", errors.New("secret length must be positive")
	}

	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(bytes), nil
}
