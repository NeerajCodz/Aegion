package secrettoken

import (
	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"
)

const DefaultLookupPrefixLength = 12

// Hash returns a stable SHA-256 base64 digest for storing opaque bearer secrets.
func Hash(token string) string {
	hash, err := platformcrypto.HashOpaqueToken(token)
	if err != nil {
		return ""
	}
	return hash
}

// Validate compares a provided token to a stored hash in constant time.
func Validate(token, expectedHash string) bool {
	if token == "" || expectedHash == "" {
		return false
	}
	return platformcrypto.ValidateOpaqueToken(token, expectedHash)
}

// Prefix returns a non-secret lookup prefix used to narrow bearer-token queries.
func Prefix(token string, length int) string {
	if length <= 0 {
		return ""
	}
	prefix, err := platformcrypto.OpaqueTokenPrefix(token, length)
	if err != nil {
		return ""
	}
	return prefix
}
