package xlog

import (
	"encoding/hex"
	"fmt"
	"strings"

	platformcrypto "github.com/aegion/aegion/internal/platform/crypto"
)

var defaultDenyPatterns = []string{
	"password",
	"token",
	"authorization",
	"cookie",
	"secret",
	"card.number",
	"card_number",
	"private_key",
}

var defaultHashPatterns = []string{
	"user.email",
	"http.client_ip",
}

// Redactor prevents sensitive fields from leaving the process.
type Redactor struct {
	deny []string
	hash []string
}

// NewRedactor creates a field redactor.
func NewRedactor(extraDeny []string) *Redactor {
	deny := append([]string{}, defaultDenyPatterns...)
	deny = append(deny, extraDeny...)
	return &Redactor{
		deny: normalizePatterns(deny),
		hash: normalizePatterns(defaultHashPatterns),
	}
}

func normalizePatterns(patterns []string) []string {
	out := make([]string, 0, len(patterns))
	for _, p := range patterns {
		p = strings.ToLower(strings.TrimSpace(p))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (r *Redactor) Apply(key string, value any) any {
	if r == nil {
		return value
	}
	normalized := strings.ToLower(strings.TrimSpace(key))
	for _, pattern := range r.deny {
		if normalized == pattern || strings.Contains(normalized, "."+pattern) || strings.Contains(normalized, pattern+".") {
			return "[REDACTED]"
		}
	}
	for _, pattern := range r.hash {
		if normalized == pattern {
			sum := platformcrypto.SHA256Digest([]byte(fmt.Sprint(value)))
			return hex.EncodeToString(sum[:])
		}
	}
	return value
}
