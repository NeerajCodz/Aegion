package secrettoken

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
)

const DefaultLookupPrefixLength = 12

// Hash returns a stable SHA-256 base64 digest for storing opaque bearer secrets.
func Hash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.StdEncoding.EncodeToString(sum[:])
}

// Validate compares a provided token to a stored hash in constant time.
func Validate(token, expectedHash string) bool {
	if token == "" || expectedHash == "" {
		return false
	}

	actual := Hash(token)
	if len(actual) != len(expectedHash) {
		return false
	}

	return subtle.ConstantTimeCompare([]byte(actual), []byte(expectedHash)) == 1
}

// Prefix returns a non-secret lookup prefix used to narrow bearer-token queries.
func Prefix(token string, length int) string {
	if length <= 0 {
		return ""
	}
	if len(token) <= length {
		return token
	}
	return token[:length]
}
