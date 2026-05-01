package crypto

import (
	"net/http"
	"sort"
	"strings"
	"time"
)

const identityEnvelopeKind = "identity_headers"

// CanonicalHeaderPayload builds a deterministic header payload from the provided allowlist.
func CanonicalHeaderPayload(header http.Header, signedHeaders []string) []byte {
	canonical := make([]string, 0, len(signedHeaders))
	for _, name := range signedHeaders {
		value := strings.TrimSpace(header.Get(name))
		if value == "" {
			continue
		}
		canonical = append(canonical, strings.ToLower(strings.TrimSpace(name))+":"+value)
	}
	sort.Strings(canonical)
	return []byte(strings.Join(canonical, "\n"))
}

// SignIdentityHeaders signs selected headers with a versioned envelope.
func SignIdentityHeaders(secret []byte, header http.Header, signedHeaders []string, now time.Time) (string, error) {
	payload := CanonicalHeaderPayload(header, signedHeaders)
	if len(payload) == 0 {
		return "", nil
	}
	return SignEnvelope(identityEnvelopeKind, secret, payload, now)
}

// VerifyIdentityHeaders verifies a versioned envelope for selected headers.
func VerifyIdentityHeaders(secret []byte, header http.Header, signedHeaders []string, envelope string, maxAge time.Duration, now time.Time) bool {
	return VerifyEnvelope(identityEnvelopeKind, secret, CanonicalHeaderPayload(header, signedHeaders), envelope, maxAge, now)
}
