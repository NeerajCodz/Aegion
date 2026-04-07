package main

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestLoadAdminMigrations_SortsAndFilters(t *testing.T) {
	fsys := fstest.MapFS{
		"migrations/0002_second.up.sql": {Data: []byte("SELECT 2;")},
		"migrations/0001_first.up.sql":  {Data: []byte("SELECT 1;")},
		"migrations/0002_second.down.sql": {Data: []byte(
			"SELECT 'ignored';",
		)},
		"migrations/notes.txt": {Data: []byte("ignore me")},
	}

	migrations, err := loadAdminMigrations(fsys)
	if err != nil {
		t.Fatalf("loadAdminMigrations returned error: %v", err)
	}
	if len(migrations) != 2 {
		t.Fatalf("expected 2 up migrations, got %d", len(migrations))
	}
	if migrations[0].Version != 1 || migrations[0].Name != "first" {
		t.Fatalf("unexpected first migration: %+v", migrations[0])
	}
	if migrations[1].Version != 2 || migrations[1].Name != "second" {
		t.Fatalf("unexpected second migration: %+v", migrations[1])
	}
}

func TestLoadAdminMigrations_ErrorsOnInvalidFilename(t *testing.T) {
	fsys := fstest.MapFS{
		"migrations/not-valid.up.sql": {Data: []byte("SELECT 1;")},
	}

	_, err := loadAdminMigrations(fsys)
	if err == nil || !strings.Contains(err.Error(), "invalid migration filename") {
		t.Fatalf("expected invalid filename error, got %v", err)
	}
}

func TestLoadAdminMigrations_ErrorsOnDuplicateVersions(t *testing.T) {
	fsys := fstest.MapFS{
		"migrations/0001_first.up.sql":  {Data: []byte("SELECT 1;")},
		"migrations/0001_second.up.sql": {Data: []byte("SELECT 2;")},
	}

	_, err := loadAdminMigrations(fsys)
	if err == nil || !strings.Contains(err.Error(), "duplicate migration version") {
		t.Fatalf("expected duplicate version error, got %v", err)
	}
}
