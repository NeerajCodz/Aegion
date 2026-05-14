package xlog

import (
	"strings"
	"unicode"
)

func normalizeEventName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return "log.event"
	}
	var b strings.Builder
	lastDot := false
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDot = false
		case unicode.IsSpace(r), r == '_' || r == '-' || r == '/' || r == ':':
			if !lastDot && b.Len() > 0 {
				b.WriteByte('.')
				lastDot = true
			}
		}
	}
	out := strings.Trim(b.String(), ".")
	if out == "" {
		return "log.event"
	}
	return out
}
