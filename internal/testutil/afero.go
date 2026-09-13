// Package testutil provides shared test helpers: mock implementations of internal
// interfaces, filesystem assertions, and logging utilities for use across test packages.
package testutil

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
)

// MockQuadletGenerator is a QuadletGenerator that writes to an afero.Fs instead of the real filesystem.
type MockQuadletGenerator struct {
	Fs    afero.Fs
	Files map[string]string // filename -> content
}

func (g *MockQuadletGenerator) Run(_ context.Context, _, _, normalDir, _ string) (int, error) {
	for name, content := range g.Files {
		path := filepath.Join(normalDir, name)
		if err := afero.WriteFile(g.Fs, path, []byte(content), 0644); err != nil {
			return 1, err
		}
	}
	return 0, nil
}

// AssertFileMode checks that a file on the given filesystem has the expected permission bits.
func AssertFileMode(t *testing.T, fs afero.Fs, path string, want os.FileMode) {
	t.Helper()
	info, err := fs.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%q) failed: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("mode of %q: got %04o, want %04o", path, got, want)
	}
}

// AssertFileContent checks the content of a file on the given filesystem.
func AssertFileContent(t *testing.T, fs afero.Fs, path, want string) {
	t.Helper()
	got, err := afero.ReadFile(fs, path)
	if err != nil {
		t.Fatalf("ReadFile(%q) failed: %v", path, err)
	}
	if string(got) != want {
		t.Errorf("content of %q:\ngot:  %q\nwant: %q", path, string(got), want)
	}
}
