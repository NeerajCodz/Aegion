package crypto

// HashOpaqueToken returns a stable SHA-256 base64 digest for opaque bearer secrets.
func HashOpaqueToken(token string) (string, error) {
	return cOpaqueHash(token)
}

// ValidateOpaqueToken compares a provided token to a stored hash in constant time.
func ValidateOpaqueToken(token, expectedHash string) bool {
	if token == "" || expectedHash == "" {
		return false
	}
	return cOpaqueValidate(token, expectedHash)
}

// OpaqueTokenPrefix returns a non-secret lookup prefix.
func OpaqueTokenPrefix(token string, length int) (string, error) {
	if length <= 0 {
		return "", nil
	}
	return cOpaquePrefix(token, length)
}
