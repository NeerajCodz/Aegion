package catalog

import (
	"embed"
	"errors"
	"io/fs"
	"sync"
	"testing"
)

func TestCatalogAdditionalBranches(t *testing.T) {
	if _, err := Lookup("does-not-exist"); err == nil {
		t.Fatal("Lookup(unknown) expected error")
	}

	googleA, err := Lookup("  GOOGLE ")
	if err != nil {
		t.Fatalf("Lookup(trim/case) error = %v", err)
	}
	if googleA.Slug != "google" {
		t.Fatalf("Lookup(trim/case) unexpected slug: %q", googleA.Slug)
	}

	names1 := Names()
	names2 := Names()
	if len(names1) == 0 || len(names2) == 0 {
		t.Fatal("Names() should return embedded presets")
	}
	orig := names2[0]
	names1[0] = "mutated"
	if names2[0] != orig {
		t.Fatalf("Names() should return independent slices, got %q want %q", names2[0], orig)
	}

	all1 := All()
	all2 := All()
	if len(all1) == 0 || len(all2) == 0 {
		t.Fatal("All() should return embedded providers")
	}
	origSlug := all2[0].Slug
	all1[0].Slug = "mutated"
	if all2[0].Slug != origSlug {
		t.Fatalf("All() should return independent values, got %q want %q", all2[0].Slug, origSlug)
	}
}

func TestCatalogErrorFallbackBranches(t *testing.T) {
	origFS := presetFS
	origOnce := loadOnce
	origErr := loadErr
	origBySlug := presetBySlug
	origOrder := presetOrder
	t.Cleanup(func() {
		presetFS = origFS
		loadOnce = origOnce
		loadErr = origErr
		presetBySlug = origBySlug
		presetOrder = origOrder
	})

	// Force ensureLoaded to read from an empty embedded FS to exercise load-error paths.
	presetFS = embed.FS{}
	loadOnce = sync.Once{}
	loadErr = nil
	presetBySlug = nil
	presetOrder = nil

	if _, err := Lookup("google"); err == nil {
		t.Fatal("Lookup should return loader error when presets directory is unavailable")
	}
	if got := All(); got != nil {
		t.Fatalf("All() should return nil when loader fails, got %#v", got)
	}
	if got := Names(); got != nil {
		t.Fatalf("Names() should return nil when loader fails, got %#v", got)
	}
}

type stubDirEntry struct {
	name string
	dir  bool
}

func (e stubDirEntry) Name() string               { return e.name }
func (e stubDirEntry) IsDir() bool                { return e.dir }
func (e stubDirEntry) Type() fs.FileMode          { return 0 }
func (e stubDirEntry) Info() (fs.FileInfo, error) { return nil, errors.New("not implemented") }

func TestCatalogLoaderHookBranches(t *testing.T) {
	origReadDir := readPresetsDirHook
	origReadFile := readPresetFileHook
	origOnce := loadOnce
	origErr := loadErr
	origBySlug := presetBySlug
	origOrder := presetOrder
	t.Cleanup(func() {
		readPresetsDirHook = origReadDir
		readPresetFileHook = origReadFile
		loadOnce = origOnce
		loadErr = origErr
		presetBySlug = origBySlug
		presetOrder = origOrder
	})

	reset := func() {
		loadOnce = sync.Once{}
		loadErr = nil
		presetBySlug = nil
		presetOrder = nil
	}

	t.Run("skip directories and non-json files", func(t *testing.T) {
		reset()
		readPresetsDirHook = func() ([]fs.DirEntry, error) {
			return []fs.DirEntry{
				stubDirEntry{name: "subdir", dir: true},
				stubDirEntry{name: "readme.txt", dir: false},
				stubDirEntry{name: "google.json", dir: false},
			}, nil
		}
		readPresetFileHook = func(name string) ([]byte, error) {
			if name != "google.json" {
				t.Fatalf("unexpected file read %q", name)
			}
			return []byte(`{"slug":"google","display_name":"Google","client_id":"id","redirect_uri":"https://app.example/cb","enabled":true}`), nil
		}
		if _, err := Lookup("google"); err != nil {
			t.Fatalf("Lookup(google) err=%v", err)
		}
		if _, err := Lookup("readme"); err == nil {
			t.Fatal("expected unknown provider for skipped non-json file")
		}
	})

	t.Run("read file error", func(t *testing.T) {
		reset()
		readPresetsDirHook = func() ([]fs.DirEntry, error) {
			return []fs.DirEntry{stubDirEntry{name: "bad.json"}}, nil
		}
		readPresetFileHook = func(name string) ([]byte, error) {
			return nil, errors.New("read failed")
		}
		if _, err := Lookup("bad"); err == nil || err.Error() != "read failed" {
			t.Fatalf("expected read failure, got %v", err)
		}
	})

	t.Run("invalid json and missing slug and default preset", func(t *testing.T) {
		reset()
		readPresetsDirHook = func() ([]fs.DirEntry, error) {
			return []fs.DirEntry{stubDirEntry{name: "bad.json"}}, nil
		}
		readPresetFileHook = func(name string) ([]byte, error) {
			return []byte(`{`), nil
		}
		if _, err := Lookup("bad"); err == nil {
			t.Fatal("expected decode error")
		}

		reset()
		readPresetsDirHook = func() ([]fs.DirEntry, error) {
			return []fs.DirEntry{stubDirEntry{name: "missing.json"}}, nil
		}
		readPresetFileHook = func(name string) ([]byte, error) {
			return []byte(`{"display_name":"Missing Slug"}`), nil
		}
		if _, err := Lookup("missing"); err == nil {
			t.Fatal("expected missing slug error")
		}

		reset()
		readPresetsDirHook = func() ([]fs.DirEntry, error) {
			return []fs.DirEntry{stubDirEntry{name: "custom.json"}}, nil
		}
		readPresetFileHook = func(name string) ([]byte, error) {
			return []byte(`{"slug":"custom","display_name":"Custom"}`), nil
		}
		provider, err := Lookup("custom")
		if err != nil {
			t.Fatalf("Lookup(custom) err=%v", err)
		}
		if provider.Preset != "custom" {
			t.Fatalf("expected default preset to match slug, got %q", provider.Preset)
		}
	})
}
