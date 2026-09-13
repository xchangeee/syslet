package filestore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
)

func newBuildStore(t *testing.T) *BuildContextFileStore {
	t.Helper()
	return NewBuildContextFileStoreAt(afero.NewOsFs(), t.TempDir())
}

// seedBuildFile writes a build context file directly to the filesystem,
// bypassing the store implementation.
func seedBuildFile(t *testing.T, store *BuildContextFileStore, unitName, fileName, content string, mode os.FileMode) {
	t.Helper()
	path := store.Resolve(unitName, fileName)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("seedBuildFile MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("seedBuildFile WriteFile: %v", err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("seedBuildFile Chmod: %v", err)
	}
}

func TestNewBuildContextFileStore_UsesDefaultBaseDir(t *testing.T) {
	store := NewBuildContextFileStore(afero.NewOsFs())
	if store.BaseDirectory() != DefaultBuildContextDir {
		t.Errorf("expected base dir %q, got %q", DefaultBuildContextDir, store.BaseDirectory())
	}
}

func TestBuildContextFileStore_Resolve_ReturnsBaseUnitAndFileName(t *testing.T) {
	store := newBuildStore(t)
	got := store.Resolve("mybuild", "Containerfile")
	want := filepath.Join(store.BaseDirectory(), "mybuild", "Containerfile")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildContextFileStore_IsFileChanged_FileAbsent_ReturnsChanged(t *testing.T) {
	store := newBuildStore(t)

	changed, existing, existingMode, err := store.IsFileChanged("mybuild", "Containerfile", "FROM alpine", 0644)
	if err != nil {
		t.Fatalf("IsFileChanged: %v", err)
	}
	if !changed {
		t.Error("expected new file to be marked as changed")
	}
	if existing != "" {
		t.Errorf("expected empty existing for new file, got %q", existing)
	}
	if existingMode != 0 {
		t.Errorf("expected existingMode 0 for new file, got %04o", existingMode)
	}
}

func TestBuildContextFileStore_IsFileChanged_ContentAndModeMatch_ReturnsUnchanged(t *testing.T) {
	store := newBuildStore(t)
	seedBuildFile(t, store, "mybuild", "Containerfile", "FROM alpine", 0644)

	changed, existing, existingMode, err := store.IsFileChanged("mybuild", "Containerfile", "FROM alpine", 0644)
	if err != nil {
		t.Fatalf("IsFileChanged: %v", err)
	}
	if changed {
		t.Error("expected unchanged file to not be marked as changed")
	}
	if existing != "FROM alpine" {
		t.Errorf("expected existing %q, got %q", "FROM alpine", existing)
	}
	if existingMode != 0644 {
		t.Errorf("expected existingMode 0644, got %04o", existingMode)
	}
}

func TestBuildContextFileStore_IsFileChanged_ContentDiffers_ReturnsChanged(t *testing.T) {
	store := newBuildStore(t)
	seedBuildFile(t, store, "mybuild", "Containerfile", "FROM alpine", 0644)

	changed, existing, existingMode, err := store.IsFileChanged("mybuild", "Containerfile", "FROM ubuntu", 0644)
	if err != nil {
		t.Fatalf("IsFileChanged: %v", err)
	}
	if !changed {
		t.Error("expected modified file to be marked as changed")
	}
	if existing != "FROM alpine" {
		t.Errorf("expected existing %q, got %q", "FROM alpine", existing)
	}
	if existingMode != 0644 {
		t.Errorf("expected existingMode 0644, got %04o", existingMode)
	}
}

func TestBuildContextFileStore_IsFileChanged_ModeDiffers_ReturnsChanged(t *testing.T) {
	store := newBuildStore(t)
	seedBuildFile(t, store, "mybuild", "script.sh", "#!/bin/sh", 0644)

	changed, _, existingMode, err := store.IsFileChanged("mybuild", "script.sh", "#!/bin/sh", 0755)
	if err != nil {
		t.Fatalf("IsFileChanged: %v", err)
	}
	if !changed {
		t.Error("expected changed when mode differs")
	}
	if existingMode != 0644 {
		t.Errorf("expected existingMode 0644, got %04o", existingMode)
	}
}

func TestBuildContextFileStore_WriteFile_CreatesUnitDirectory(t *testing.T) {
	store := newBuildStore(t)

	if err := store.WriteFile("newbuild", "Containerfile", "FROM alpine", 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	unitDir := filepath.Join(store.BaseDirectory(), "newbuild")
	info, err := os.Stat(unitDir)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory to be created")
	}
}

func TestBuildContextFileStore_WriteFile_SetsFileMode(t *testing.T) {
	store := newBuildStore(t)

	if err := store.WriteFile("mybuild", "script.sh", "#!/bin/sh", 0755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	info, err := os.Stat(store.Resolve("mybuild", "script.sh"))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0755 {
		t.Errorf("expected mode 0755, got %04o", info.Mode().Perm())
	}
}

func TestBuildContextFileStore_ListStaleFiles_ReturnsFilesNotInDesiredSet(t *testing.T) {
	store := newBuildStore(t)
	seedBuildFile(t, store, "mybuild", "Containerfile", "FROM alpine", 0644)
	seedBuildFile(t, store, "mybuild", "old.sh", "old", 0644)

	desired := map[string]bool{"Containerfile": true}
	deployed, stale, err := store.ListStaleFiles("mybuild", desired)
	if err != nil {
		t.Fatalf("ListStaleFiles: %v", err)
	}
	if len(deployed) != 2 {
		t.Errorf("expected 2 deployed files, got %d", len(deployed))
	}
	if len(stale) != 1 || stale[0] != "old.sh" {
		t.Errorf("expected stale=[old.sh], got %v", stale)
	}
}

func TestBuildContextFileStore_RemoveFile_DeletesFile(t *testing.T) {
	store := newBuildStore(t)
	seedBuildFile(t, store, "mybuild", "old.sh", "content", 0644)

	if err := store.RemoveFile("mybuild", "old.sh"); err != nil {
		t.Fatalf("RemoveFile: %v", err)
	}

	if _, err := os.Stat(store.Resolve("mybuild", "old.sh")); !os.IsNotExist(err) {
		t.Error("expected file to be removed")
	}
}

func TestBuildContextFileStore_RemoveFile_FileAbsent_Succeeds(t *testing.T) {
	store := newBuildStore(t)

	if err := store.RemoveFile("mybuild", "nonexistent.sh"); err != nil {
		t.Fatalf("RemoveFile of nonexistent file: %v", err)
	}
}

func TestBuildContextFileStore_RemoveUnit_DeletesUnitDirectory(t *testing.T) {
	store := newBuildStore(t)
	seedBuildFile(t, store, "mybuild", "Containerfile", "FROM alpine", 0644)
	seedBuildFile(t, store, "mybuild", "setup.sh", "#!/bin/sh", 0755)

	if err := store.RemoveUnit("mybuild"); err != nil {
		t.Fatalf("RemoveUnit: %v", err)
	}

	unitDir := filepath.Join(store.BaseDirectory(), "mybuild")
	if _, err := os.Stat(unitDir); !os.IsNotExist(err) {
		t.Error("expected directory to be removed")
	}
}

func TestBuildContextFileStore_RemoveUnit_DirectoryAbsent_Succeeds(t *testing.T) {
	store := newBuildStore(t)

	if err := store.RemoveUnit("nonexistent"); err != nil {
		t.Fatalf("RemoveUnit of nonexistent directory: %v", err)
	}
}
