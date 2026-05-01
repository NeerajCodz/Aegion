package main

import (
	"crypto/sha256"
	"net/http"
	"testing"

	"github.com/aegion/aegion/internal/platform/config"
)

func TestDeriveCipherKeyBranches(t *testing.T) {
	if got := deriveCipherKey(nil); got != nil {
		t.Fatalf("deriveCipherKey(nil) = %v, want nil", got)
	}

	if got := deriveCipherKey(&config.Config{}); got != nil {
		t.Fatalf("deriveCipherKey(empty secrets) = %v, want nil", got)
	}

	cfg := &config.Config{}
	cfg.Secrets.Cipher = []string{"  top-secret  "}
	got := deriveCipherKey(cfg)
	want := sha256.Sum256([]byte("top-secret"))
	if len(got) != len(want) || string(got) != string(want[:]) {
		t.Fatalf("deriveCipherKey(secret) mismatch: got=%x want=%x", got, want)
	}
}

func TestAppendVaryHeaderBranches(t *testing.T) {
	h := http.Header{}
	h.Add("Vary", "Origin, Accept-Encoding")

	appendVaryHeader(h, " origin ")
	if got := h.Values("Vary"); len(got) != 1 {
		t.Fatalf("appendVaryHeader(duplicate) added entry: %#v", got)
	}

	appendVaryHeader(h, " ")
	if got := h.Values("Vary"); len(got) != 1 {
		t.Fatalf("appendVaryHeader(empty) changed entry: %#v", got)
	}

	appendVaryHeader(h, "Accept")
	if got := h.Values("Vary"); len(got) != 2 {
		t.Fatalf("appendVaryHeader(new value) expected 2 entries, got %#v", got)
	}
}
