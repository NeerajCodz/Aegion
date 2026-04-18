package router

import (
	"net/http"
	"testing"
)

func TestAppendVaryHeaderBranches(t *testing.T) {
	header := http.Header{}

	appendVaryHeader(header, "   ")
	if len(header.Values("Vary")) != 0 {
		t.Fatalf("expected blank vary value to be ignored, got %#v", header.Values("Vary"))
	}

	header.Add("Vary", "Origin, Accept-Encoding")
	appendVaryHeader(header, "origin")
	if len(header.Values("Vary")) != 1 {
		t.Fatalf("expected duplicate vary value to be ignored, got %#v", header.Values("Vary"))
	}

	appendVaryHeader(header, "Authorization")
	if len(header.Values("Vary")) != 2 {
		t.Fatalf("expected new vary value to be appended, got %#v", header.Values("Vary"))
	}
}
