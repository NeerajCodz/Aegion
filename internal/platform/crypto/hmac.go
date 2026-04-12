package crypto

import "time"

// HMACSHA256Hex signs arbitrary bytes with HMAC-SHA256 and returns a hex digest.
func HMACSHA256Hex(secret, message []byte) (string, error) {
	return cHMACSHA256Hex(secret, message)
}

// SignEnvelope returns a versioned signature envelope over the provided payload.
func SignEnvelope(kind string, secret, payload []byte, now time.Time) (string, error) {
	return cSignEnvelope(kind, secret, payload, now.UTC().Unix())
}

// VerifyEnvelope checks a versioned signature envelope over the provided payload.
func VerifyEnvelope(kind string, secret, payload []byte, envelope string, maxAge time.Duration, now time.Time) bool {
	maxAgeSeconds := uint64(0)
	if maxAge > 0 {
		maxAgeSeconds = uint64(maxAge / time.Second)
	}
	return cVerifyEnvelope(kind, secret, payload, envelope, maxAgeSeconds, now.UTC().Unix())
}
