package filestore

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/afero"

	"codeberg.org/xchangeee/syslet/internal/model"
	"codeberg.org/xchangeee/syslet/internal/util"
)

const (
	// DefaultContainerConfigDir is where bind-mounted container config files are stored.
	DefaultContainerConfigDir = "/etc/containers/config"
)

// ContainerConfigFileStore manages per-container config files and versioned configDirs.
//
// It translates ContainerUnitRef and ContainerMountPath to host-disk paths using
// BasenameWithHashSuffix, keeping the naming strategy in one place so that render,
// plan, and apply layers all operate on mount paths directly.
//
// Plain config files live under <baseDir>/<unit>/<mountPathHash>.
// Versioned configDirs live under <baseDir>/<unit>/<mountPathHash>/<version>/ with an
// atomic ..data symlink pointing to the current version.
type ContainerConfigFileStore struct {
	afs     afero.Fs
	baseDir string
}

// NewContainerConfigFileStore creates a store rooted at the default container config directory.
func NewContainerConfigFileStore(afs afero.Fs) *ContainerConfigFileStore {
	return &ContainerConfigFileStore{afs: afs, baseDir: DefaultContainerConfigDir}
}

// NewContainerConfigFileStoreAt creates a store with a custom base directory.
// Use this in tests that need a real filesystem (e.g. for symlink support).
func NewContainerConfigFileStoreAt(afs afero.Fs, baseDir string) *ContainerConfigFileStore {
	return &ContainerConfigFileStore{afs: afs, baseDir: baseDir}
}

func (s *ContainerConfigFileStore) BaseDirectory() string {
	return s.baseDir
}

// InternalFilename derives the host-disk filename for a container mount path.
func (s *ContainerConfigFileStore) InternalFilename(dest model.ContainerMountPath) string {
	return util.BasenameWithHashSuffix(string(dest))
}

// InternalDirname derives the host-disk directory name for a container dir-mount path.
func (s *ContainerConfigFileStore) InternalDirname(dest model.ContainerMountPath) string {
	return util.BasenameWithHashSuffix(string(dest))
}

// Resolve returns the full host-disk path for a container's mount path.
// Its signature matches render.ContainerResolverFactory so it can be passed directly.
func (s *ContainerConfigFileStore) Resolve(ref model.ContainerUnitRef, dest model.ContainerMountPath) string {
	return filepath.Join(s.baseDir, ref.Name(), s.InternalFilename(dest))
}

func (s *ContainerConfigFileStore) unitDir(ref model.ContainerUnitRef) string {
	return filepath.Join(s.baseDir, ref.Name())
}

// ListFiles returns the internal filenames deployed for a container unit.
func (s *ContainerConfigFileStore) ListFiles(ref model.ContainerUnitRef) ([]string, error) {
	entries, err := afero.ReadDir(s.afs, s.unitDir(ref))
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

// IsFileChanged reports whether the deployed file differs from content/mode.
// existing is the current on-disk content (empty string when the file does not exist).
// existingMode is the current on-disk permission bits (0 when the file does not exist).
// Both are returned so callers can avoid a second read when they need the old values.
func (s *ContainerConfigFileStore) IsFileChanged(ref model.ContainerUnitRef, dest model.ContainerMountPath, content string, mode os.FileMode) (changed bool, existing string, existingMode os.FileMode, err error) {
	return isFileChanged(s.afs, filepath.Join(s.unitDir(ref), s.InternalFilename(dest)), content, mode)
}

// WriteFile writes content with the given mode to the host-disk path for dest.
func (s *ContainerConfigFileStore) WriteFile(ref model.ContainerUnitRef, dest model.ContainerMountPath, content string, mode os.FileMode) error {
	dir := s.unitDir(ref)
	if err := s.afs.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating config dir %s: %w", dir, err)
	}
	path := filepath.Join(dir, s.InternalFilename(dest))
	if err := afero.WriteFile(s.afs, path, []byte(content), mode); err != nil {
		return err
	}
	// Chmod explicitly because WriteFile only sets perm on newly created files.
	return s.afs.Chmod(path, mode)
}

// ListStaleFiles returns deployed and stale internal filenames for ref.
// desiredFilenames is the set of internal filenames (from InternalFilename) that are still wanted.
func (s *ContainerConfigFileStore) ListStaleFiles(ref model.ContainerUnitRef, desiredFilenames map[string]bool) (deployed, stale []string, err error) {
	deployed, err = s.ListFiles(ref)
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

// RemoveFile removes a single file by its internal filename (as returned by list operations).
func (s *ContainerConfigFileStore) RemoveFile(ref model.ContainerUnitRef, internalFilename string) error {
	path := filepath.Join(s.unitDir(ref), internalFilename)
	err := s.afs.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// RemoveUnit removes the entire config directory for ref.
func (s *ContainerConfigFileStore) RemoveUnit(ref model.ContainerUnitRef) error {
	err := s.afs.RemoveAll(s.unitDir(ref))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// --- Versioned configDir operations ---
//
// Directory layout under <base>/<unit>/<mountPathHash>/:
//
//	..data          → <version>      (symlink, atomically updated)
//	<name>          → ..data/<name>  (per-file stable symlink for running containers)
//	<version>/
//	  <name>                         (versioned file content)

func (s *ContainerConfigFileStore) versionedDirPath(ref model.ContainerUnitRef, dest model.ContainerMountPath) string {
	return filepath.Join(s.baseDir, ref.Name(), s.InternalDirname(dest))
}

// CurrentDirVersion returns the active version number from the ..data symlink.
// Returns 0 if no version is deployed yet.
func (s *ContainerConfigFileStore) CurrentDirVersion(ref model.ContainerUnitRef, dest model.ContainerMountPath) (int, error) {
	dataLink := filepath.Join(s.versionedDirPath(ref, dest), "..data")
	target, err := s.readlink(dataLink)
	if err != nil {
		return 0, nil
	}
	v, err := strconv.Atoi(target)
	if err != nil {
		return 0, nil
	}
	return v, nil
}

// IsVersionedFileChanged reports whether a single file in the current version dir
// differs from the desired content or mode. Returns true when no version is deployed.
func (s *ContainerConfigFileStore) IsVersionedFileChanged(ref model.ContainerUnitRef, dest model.ContainerMountPath, version int, filename string, mode os.FileMode, content string) (bool, error) {
	if version == 0 {
		return true, nil
	}
	path := filepath.Join(s.versionedDirPath(ref, dest), strconv.Itoa(version), filename)
	raw, err := afero.ReadFile(s.afs, path)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("reading %s: %w", path, err)
	}
	if string(raw) != content {
		return true, nil
	}
	info, err := s.afs.Stat(path)
	if err != nil {
		return false, fmt.Errorf("stating %s: %w", path, err)
	}
	return info.Mode().Perm() != mode, nil
}

// WriteVersionedDirFile writes a single file into the versioned directory for dest.
// The ..data symlink is not touched; call UpdateDirSymlink after all files are written.
func (s *ContainerConfigFileStore) WriteVersionedDirFile(ref model.ContainerUnitRef, dest model.ContainerMountPath, version int, filename string, mode os.FileMode, content string) error {
	versionDir := filepath.Join(s.versionedDirPath(ref, dest), strconv.Itoa(version))
	if err := s.afs.MkdirAll(versionDir, 0755); err != nil {
		return fmt.Errorf("creating version dir: %w", err)
	}
	path := filepath.Join(versionDir, filename)
	if err := afero.WriteFile(s.afs, path, []byte(content), mode); err != nil {
		return fmt.Errorf("writing %s: %w", filename, err)
	}
	return s.afs.Chmod(path, mode)
}

// UpdateDirSymlink atomically swaps ..data to point to the new version and refreshes
// the per-file stable symlinks (<name> → ..data/<name>) for all files in the version dir.
func (s *ContainerConfigFileStore) UpdateDirSymlink(ref model.ContainerUnitRef, dest model.ContainerMountPath, version int) error {
	dirPath := s.versionedDirPath(ref, dest)
	if err := s.afs.MkdirAll(dirPath, 0755); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}

	versionStr := strconv.Itoa(version)
	dataLink := filepath.Join(dirPath, "..data")
	tmpLink := filepath.Join(dirPath, "..data-tmp")

	_ = s.afs.Remove(tmpLink) // best-effort: remove stale tmp link from a previous interrupted run
	if err := s.symlink(versionStr, tmpLink); err != nil {
		return fmt.Errorf("creating temp symlink: %w", err)
	}
	if err := s.afs.Rename(tmpLink, dataLink); err != nil {
		return fmt.Errorf("swapping ..data symlink: %w", err)
	}

	// Build the set of files in the new version dir.
	versionDir := filepath.Join(dirPath, versionStr)
	versionEntries, err := afero.ReadDir(s.afs, versionDir)
	if err != nil {
		return fmt.Errorf("listing version dir for symlinks: %w", err)
	}
	newFiles := make(map[string]bool, len(versionEntries))
	for _, e := range versionEntries {
		if !e.IsDir() {
			newFiles[e.Name()] = true
		}
	}

	// Build the set of existing per-file symlinks in dirPath.
	// Version dirs are integer-named directories; ..data* entries start with "..".
	dirEntries, err := afero.ReadDir(s.afs, dirPath)
	if err != nil {
		return fmt.Errorf("listing config dir for symlink delta: %w", err)
	}
	existingLinks := make(map[string]bool, len(dirEntries))
	for _, e := range dirEntries {
		if !e.IsDir() && !strings.HasPrefix(e.Name(), "..") {
			existingLinks[e.Name()] = true
		}
	}

	// Remove symlinks for files dropped in the new version.
	for name := range existingLinks {
		if !newFiles[name] {
			if err := s.afs.Remove(filepath.Join(dirPath, name)); err != nil {
				return fmt.Errorf("removing stale symlink %s: %w", name, err)
			}
		}
	}

	// Create symlinks for files new in this version.
	for name := range newFiles {
		if !existingLinks[name] {
			if err := s.symlink("..data/"+name, filepath.Join(dirPath, name)); err != nil {
				return fmt.Errorf("symlinking %s: %w", name, err)
			}
		}
	}
	return nil
}

// ListVersionedDirFiles returns the filenames stored in the given version directory.
// Returns nil if the version directory does not exist.
func (s *ContainerConfigFileStore) ListVersionedDirFiles(ref model.ContainerUnitRef, dest model.ContainerMountPath, version int) ([]string, error) {
	versionDir := filepath.Join(s.versionedDirPath(ref, dest), strconv.Itoa(version))
	entries, err := afero.ReadDir(s.afs, versionDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("listing version dir: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// ReadVersionedDirFiles returns the content of every file in the given version directory,
// keyed by filename. Returns an empty map when no version is deployed (version == 0)
// or the version directory does not exist.
func (s *ContainerConfigFileStore) ReadVersionedDirFiles(ref model.ContainerUnitRef, dest model.ContainerMountPath, version int) (map[string]string, error) {
	if version == 0 {
		return map[string]string{}, nil
	}
	versionDir := filepath.Join(s.versionedDirPath(ref, dest), strconv.Itoa(version))
	entries, err := afero.ReadDir(s.afs, versionDir)
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("listing version dir: %w", err)
	}
	result := make(map[string]string, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		raw, err := afero.ReadFile(s.afs, filepath.Join(versionDir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", e.Name(), err)
		}
		result[e.Name()] = string(raw)
	}
	return result, nil
}

// ReadVersionedDirFileModes returns the permission bits of every file in the given
// version directory, keyed by filename. Returns an empty map when no version is
// deployed (version == 0) or the version directory does not exist.
func (s *ContainerConfigFileStore) ReadVersionedDirFileModes(ref model.ContainerUnitRef, dest model.ContainerMountPath, version int) (map[string]os.FileMode, error) {
	if version == 0 {
		return map[string]os.FileMode{}, nil
	}
	versionDir := filepath.Join(s.versionedDirPath(ref, dest), strconv.Itoa(version))
	entries, err := afero.ReadDir(s.afs, versionDir)
	if os.IsNotExist(err) {
		return map[string]os.FileMode{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("listing version dir: %w", err)
	}
	result := make(map[string]os.FileMode, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			result[e.Name()] = e.Mode().Perm()
		}
	}
	return result, nil
}

// PruneOldVersions removes all version directories under dest except keepVersion.
func (s *ContainerConfigFileStore) PruneOldVersions(ref model.ContainerUnitRef, dest model.ContainerMountPath, keepVersion int) error {
	dirPath := s.versionedDirPath(ref, dest)
	entries, err := afero.ReadDir(s.afs, dirPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("listing config dir for pruning: %w", err)
	}
	keepName := strconv.Itoa(keepVersion)
	for _, e := range entries {
		if !e.IsDir() || e.Name() == keepName {
			continue
		}
		if err := s.afs.RemoveAll(filepath.Join(dirPath, e.Name())); err != nil {
			return fmt.Errorf("removing old version %s: %w", e.Name(), err)
		}
	}
	return nil
}

// ListStaleDirs returns configDir internal dirnames (mountPathHash subdirectories) for ref
// that are not present in desiredDirnames.
func (s *ContainerConfigFileStore) ListStaleDirs(ref model.ContainerUnitRef, desiredDirnames map[string]bool) ([]string, error) {
	unitDir := s.unitDir(ref)
	entries, err := afero.ReadDir(s.afs, unitDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var stale []string
	for _, e := range entries {
		if e.IsDir() && !desiredDirnames[e.Name()] {
			stale = append(stale, e.Name())
		}
	}
	return stale, nil
}

// RemoveDir removes a configDir subdirectory by its internal dirname.
func (s *ContainerConfigFileStore) RemoveDir(ref model.ContainerUnitRef, internalDirname string) error {
	path := filepath.Join(s.unitDir(ref), internalDirname)
	err := s.afs.RemoveAll(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (s *ContainerConfigFileStore) symlink(oldname, newname string) error {
	linker, ok := s.afs.(afero.Linker)
	if !ok {
		return fmt.Errorf("filesystem does not support symlinks")
	}
	return linker.SymlinkIfPossible(oldname, newname)
}

func (s *ContainerConfigFileStore) readlink(name string) (string, error) {
	reader, ok := s.afs.(afero.LinkReader)
	if !ok {
		return "", fmt.Errorf("filesystem does not support readlink")
	}
	return reader.ReadlinkIfPossible(name)
}
