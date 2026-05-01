package admin

import (
	"errors"
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

func TestGetSPAFiles_SubFailureFallback(t *testing.T) {
	orig := subSPAFiles
	t.Cleanup(func() { subSPAFiles = orig })

	subSPAFiles = func(fsys fs.FS, dir string) (fs.FS, error) {
		return nil, errors.New("sub failed")
	}

	spaFS := GetSPAFiles()
	entries, err := fs.ReadDir(spaFS, ".")
	if err != nil {
		t.Fatalf("expected fallback filesystem to still be readable, got error: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no entries in fallback filesystem, got %d", len(entries))
	}
}

func TestGetMigrationFiles_ReturnsReadableFS(t *testing.T) {
	migrationFS := GetMigrationFiles()

	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		t.Fatalf("expected embedded migration filesystem to be readable: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("expected embedded admin migrations to contain files")
	}
}
