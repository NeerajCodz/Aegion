package database

import (
	"embed"
	"testing"
)

//go:embed testdata/migrations/*.sql
var testMigrations embed.FS

func TestLoadMigrations(t *testing.T) {
	migrator := NewMigrator(&DB{}, testMigrations, "testdata/migrations")

	migrations, err := migrator.loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations() error = %v", err)
	}

	if len(migrations) != 2 {
		t.Fatalf("expected 2 migrations, got %d", len(migrations))
	}

	if migrations[0].Version != 1 || migrations[1].Version != 2 {
		t.Fatalf("expected migrations sorted by version")
	}

	if migrations[0].UpSQL == "" || migrations[0].DownSQL == "" {
		t.Fatalf("expected migration 1 to have up/down SQL")
	}

	if migrations[1].UpSQL == "" || migrations[1].DownSQL == "" {
		t.Fatalf("expected migration 2 to have up/down SQL")
	}
}
