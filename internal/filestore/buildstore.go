// Package fs provides file store types for managing quadlet-related on-disk files.
//
// BuildContextFileStore manages build context files (Containerfile + sources) under
// <baseDir>/<unitName>/<filename> using identity naming — filenames are stored as-is.
//
// ContainerConfigFileStore manages per-container config files and versioned configDirs
// under <baseDir>/<unitName>/... using BasenameWithHashSuffix for collision-safe naming.
package filestore

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/afero"
)

const (
	// DefaultBuildContextDir is where build context files (Containerfile + sources) are stored.
	DefaultBuildContextDir = "/etc/containers/builds"
)

// BuildContextFileStore manages build context files for a single base directory.
// Files live under <baseDir>/<unitName>/<filename>; filenames are stored as-is (identity naming).
type BuildContextFileStore struct {
	afs     afero.Fs
	baseDir string
}

// NewBuildContextFileStore creates a store rooted at the default build context directory.
func NewBuildContextFileStore(afs afero.Fs) *BuildContextFileStore {
	return &BuildContextFileStore{afs: afs, baseDir: DefaultBuildContextDir}
}

// NewBuildContextFileStoreAt creates a store with a custom base directory.
// Use this in tests that need a real filesystem (e.g. for symlink support).
func NewBuildContextFileStoreAt(afs afero.Fs, baseDir string) *BuildContextFileStore {
	return &BuildContextFileStore{afs: afs, baseDir: baseDir}
}

func (s *BuildContextFileStore) BaseDirectory() string {
	return s.baseDir
}

// Resolve returns the full host-disk path for a file belonging to the named build unit.
// Its signature matches render.BuildResolverFactory so it can be passed directly.
func (s *BuildContextFileStore) Resolve(unitName, fileName string) string {
	return filepath.Join(s.baseDir, unitName, fileName)
}

func (s *BuildContextFileStore) unitDir(unitName string) string {
	return filepath.Join(s.baseDir, unitName)
}

func (s *BuildContextFileStore) listFiles(unitName string) ([]string, error) {
	entries, err := afero.ReadDir(s.afs, s.unitDir(unitName))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() {
			files = append(files, e.Name())
		}
	}
	return files, nil
}

// IsFileChanged checks if a build context file differs from what's deployed, including permissions.
// existing is the current on-disk content (empty string when the file does not exist).
// existingMode is the current on-disk permission bits (0 when the file does not exist).
// Both are returned so callers can avoid a second read when they need the old values.
func (s *BuildContextFileStore) IsFileChanged(unitName, fileName, content string, mode os.FileMode) (changed bool, existing string, existingMode os.FileMode, err error) {
	return isFileChanged(s.afs, filepath.Join(s.unitDir(unitName), fileName), content, mode)
}

// ListStaleFiles returns deployed and stale filenames for a build unit.
// stale contains filenames present on disk but absent from desiredFilenames.
func (s *BuildContextFileStore) ListStaleFiles(unitName string, desiredFilenames map[string]bool) (deployed, stale []string, err error) {
	deployed, err = s.listFiles(unitName)
	if err != nil {
		return
	}
	for _, f := range deployed {
		if !desiredFilenames[f] {
			stale = append(stale, f)
		}
	}
	return
}

// WriteFile writes a build context file with the given permission mode.
func (s *BuildContextFileStore) WriteFile(unitName, fileName, content string, mode os.FileMode) error {
	dir := s.unitDir(unitName)
	if err := s.afs.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating build dir %s: %w", dir, err)
	}
	path := filepath.Join(dir, fileName)
	if err := afero.WriteFile(s.afs, path, []byte(content), mode); err != nil {
		return err
	}
	// Chmod explicitly because WriteFile only sets perm on newly created files.
	return s.afs.Chmod(path, mode)
}

// RemoveFile removes a single build context file.
func (s *BuildContextFileStore) RemoveFile(unitName, fileName string) error {
	path := filepath.Join(s.unitDir(unitName), fileName)
	err := s.afs.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// RemoveUnit removes all build context files for a build unit.
func (s *BuildContextFileStore) RemoveUnit(unitName string) error {
	err := s.afs.RemoveAll(s.unitDir(unitName))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
