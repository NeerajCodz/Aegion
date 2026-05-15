package xlog

import (
	"fmt"
	"regexp"
	"strings"
)

var fieldNamePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9_]*)*$|^(timestamp|duration_ms|environment)$`)

// Schema validates xlog records.
type Schema struct {
	mode string
}

// NewSchema creates a schema validator. Modes: off, shadow, warn, strict.
func NewSchema(mode string) *Schema {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "warn"
	}
	return &Schema{mode: mode}
}

func (s *Schema) Validate(fields map[string]any) error {
	if s == nil || s.mode == "off" || s.mode == "shadow" {
		return nil
	}
	required := []string{
		"event.name",
		"event.kind",
		"event.outcome",
		"timestamp",
		"duration_ms",
		"service.name",
		"service.version",
		"environment",
	}
	for _, key := range required {
		if _, ok := fields[key]; !ok {
			return fmt.Errorf("xlog missing required field %q", key)
		}
	}
	for key := range fields {
		if !fieldNamePattern.MatchString(key) {
			return fmt.Errorf("xlog field %q must be dot-separated lowercase", key)
		}
	}
	return nil
}
