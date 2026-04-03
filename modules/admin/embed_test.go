package admin

import (
	"io/fs"
	"testing"
)

func TestGetSPAFiles_ReturnsReadableFS(t *testing.T) {
	spaFS := GetSPAFiles()

	entries, err := fs.ReadDir(spaFS, ".")
	if err != nil {
		t.Fatalf("expected embedded SPA filesystem to be readable: %v", err)
	}

	if len(entries) == 0 {
		t.Fatalf("expected embedded SPA dist to contain files")
	}
}
