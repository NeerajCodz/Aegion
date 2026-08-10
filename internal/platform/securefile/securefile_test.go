package securefile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReadRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	value, err := ReadRegularFile(path, 16)
	if err != nil {
		t.Fatalf("ReadRegularFile() error = %v", err)
	}
	if got, want := string(value), "secret"; got != want {
		t.Fatalf("ReadRegularFile() = %q, want %q", got, want)
	}
}

func TestReadRegularFileRejectsInvalidInputs(t *testing.T) {
	dir := t.TempDir()
	emptyPath := filepath.Join(dir, "empty")
	if err := os.WriteFile(emptyPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	largePath := filepath.Join(dir, "large")
	if err := os.WriteFile(largePath, []byte("too large"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		path string
		max  int64
		want error
	}{
		{name: "relative", path: "secret", max: 16, want: ErrPathNotAbsolute},
		{name: "directory", path: dir, max: 16, want: ErrNotRegularFile},
		{name: "empty", path: emptyPath, max: 16, want: ErrEmptyFile},
		{name: "large", path: largePath, max: 4, want: ErrFileTooLarge},
		{name: "zero limit", path: largePath, max: 0, want: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ReadRegularFile(test.path, test.max)
			if test.want == nil {
				if err == nil {
					t.Fatal("ReadRegularFile() error = nil")
				}
				return
			}
			if !errors.Is(err, test.want) {
				t.Fatalf("ReadRegularFile() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestReadRegularFileRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("create symlink: %v", err)
	}

	_, err := ReadRegularFile(link, 16)
	if !errors.Is(err, ErrNotRegularFile) {
		t.Fatalf("ReadRegularFile() error = %v, want %v", err, ErrNotRegularFile)
	}
}
