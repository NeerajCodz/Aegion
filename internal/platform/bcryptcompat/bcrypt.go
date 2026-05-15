package bcryptcompat

import (
	"errors"

	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"
)

// Cost constants are kept for call-site compatibility.
const (
	MinCost     = 4
	DefaultCost = 10
)

// ErrMismatchedHashAndPassword mirrors bcrypt mismatch semantics.
var ErrMismatchedHashAndPassword = errors.New("crypto/bcrypt: hashedPassword is not the hash of the given password")

// GenerateFromPassword hashes a password using the Go-native crypto engine.
func GenerateFromPassword(password []byte, _ int) ([]byte, error) {
	hash, err := platformcrypto.HashPassword(string(password))
	if err != nil {
		return nil, err
	}
	return []byte(hash), nil
}

// CompareHashAndPassword verifies a password against a Go-native hash.
func CompareHashAndPassword(hashedPassword, password []byte) error {
	matches, err := platformcrypto.VerifyPassword(string(password), string(hashedPassword))
	if err != nil {
		return err
	}
	if !matches {
		return ErrMismatchedHashAndPassword
	}
	return nil
}
