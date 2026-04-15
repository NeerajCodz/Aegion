package crypto

import (
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	sessionCookieKind          = "session_cookie"
	sessionContextEnvelopeKind = "session_context"
	defaultSessionEnvelopeTTL  = 5 * time.Minute
)

// SignSessionCookieValue returns a versioned, signed session cookie value.
func SignSessionCookieValue(secret []byte, token string, now time.Time) (string, error) {
	return cSignSessionCookie(secret, token, now.UTC().Unix())
}

// VerifySessionCookieValue validates a signed cookie value and returns the token.
func VerifySessionCookieValue(secret []byte, signed string, maxAge time.Duration, now time.Time) (string, error) {
	maxAgeSeconds := uint64(0)
	if maxAge > 0 {
		maxAgeSeconds = uint64(maxAge / time.Second)
	}
	return cVerifySessionCookie(secret, signed, maxAgeSeconds, now.UTC().Unix())
}

// CanonicalSessionContextPayload builds the payload used for signed session headers.
func CanonicalSessionContextPayload(headers map[string]string) []byte {
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var builder strings.Builder
	for i, key := range keys {
		if i > 0 {
			builder.WriteByte('\n')
		}
		builder.WriteString(strings.ToLower(strings.TrimSpace(key)))
		builder.WriteByte(':')
		builder.WriteString(strings.TrimSpace(headers[key]))
	}
	return []byte(builder.String())
}

// SignSessionContextHeaders signs the canonicalized session header payload.
func SignSessionContextHeaders(secret []byte, headers map[string]string, now time.Time) (string, error) {
	return SignEnvelope(sessionContextEnvelopeKind, secret, CanonicalSessionContextPayload(headers), now)
}

// VerifySessionContextHeaders verifies the canonicalized session header payload.
func VerifySessionContextHeaders(secret []byte, headers map[string]string, envelope string, now time.Time) bool {
	return VerifyEnvelope(sessionContextEnvelopeKind, secret, CanonicalSessionContextPayload(headers), envelope, defaultSessionEnvelopeTTL, now)
}

// ReadRequestHeaderMap extracts headers into a deterministic map for signing helpers.
func ReadRequestHeaderMap(r *http.Request, names []string) map[string]string {
	out := make(map[string]string, len(names))
	for _, name := range names {
		out[name] = r.Header.Get(name)
	}
	return out
}
