package filestore

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/spf13/afero"

	"codeberg.org/xchangeee/syslet/internal/model"
)

func newContainerStore(t *testing.T) *ContainerConfigFileStore {
	t.Helper()
	return NewContainerConfigFileStoreAt(afero.NewOsFs(), t.TempDir())
}

var testRef = model.ContainerUnitRef("webapp")
var testDest = model.ContainerMountPath("/etc/app")

// versionedDir returns the versioned configDir path for a unit+mount using only the public API.
func versionedDir(store *ContainerConfigFileStore, ref model.ContainerUnitRef, dest model.ContainerMountPath) string {
	return filepath.Join(store.BaseDirectory(), ref.Name(), store.InternalDirname(dest))
}

// seedContainerFile writes a container config file directly to the filesystem,
// bypassing the store implementation.
func seedContainerFile(t *testing.T, store *ContainerConfigFileStore, ref model.ContainerUnitRef, dest model.ContainerMountPath, content string, mode os.FileMode) {
	t.Helper()
	path := store.Resolve(ref, dest)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("seedContainerFile MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("seedContainerFile WriteFile: %v", err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("seedContainerFile Chmod: %v", err)
	}
}

// seedVersionFiles creates the version directory and a single file within it,
// without creating any symlinks. Use this to set up state before testing UpdateDirSymlink.
func seedVersionFiles(t *testing.T, store *ContainerConfigFileStore, ref model.ContainerUnitRef, dest model.ContainerMountPath, version int, filename, content string, mode os.FileMode) {
	t.Helper()
	versionDir := filepath.Join(versionedDir(store, ref, dest), strconv.Itoa(version))
	if err := os.MkdirAll(versionDir, 0755); err != nil {
		t.Fatalf("seedVersionFiles MkdirAll: %v", err)
	}
	path := filepath.Join(versionDir, filename)
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("seedVersionFiles WriteFile: %v", err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("seedVersionFiles Chmod: %v", err)
	}
}

// seedDeployedVersion creates a fully deployed versioned dir: version files, the ..data
// symlink, and the stable per-file symlink. Calling it again with a higher version
// advances the deployment (replaces the ..data symlink; per-file symlink is recreated).
func seedDeployedVersion(t *testing.T, store *ContainerConfigFileStore, ref model.ContainerUnitRef, dest model.ContainerMountPath, version int, filename, content string, mode os.FileMode) {
	t.Helper()
	dirPath := versionedDir(store, ref, dest)
	seedVersionFiles(t, store, ref, dest, version, filename, content, mode)

	dataLink := filepath.Join(dirPath, "..data")
	_ = os.Remove(dataLink)
	if err := os.Symlink(strconv.Itoa(version), dataLink); err != nil {
		t.Fatalf("seedDeployedVersion ..data symlink: %v", err)
	}

	fileLink := filepath.Join(dirPath, filename)
	_ = os.Remove(fileLink)
	if err := os.Symlink("..data/"+filename, fileLink); err != nil {
		t.Fatalf("seedDeployedVersion file symlink: %v", err)
	}
}

func TestNewContainerConfigFileStore_UsesDefaultBaseDir(t *testing.T) {
	store := NewContainerConfigFileStore(afero.NewOsFs())
	if store.BaseDirectory() != DefaultContainerConfigDir {
		t.Errorf("expected base dir %q, got %q", DefaultContainerConfigDir, store.BaseDirectory())
	}
}

func TestContainerConfigFileStore_Resolve_ReturnsBaseUnitAndHashedFilename(t *testing.T) {
	store := newContainerStore(t)
	ref := model.ContainerUnitRef("mycontainer")
	dest := model.ContainerMountPath("/etc/app/config.json")
	got := store.Resolve(ref, dest)
	want := filepath.Join(store.BaseDirectory(), ref.Name(), store.InternalFilename(dest))
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestContainerConfigFileStore_IsFileChanged_FileAbsent_ReturnsChanged(t *testing.T) {
	store := newContainerStore(t)

	changed, existing, existingMode, err := store.IsFileChanged(testRef, testDest, "new content", 0644)
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

func TestContainerConfigFileStore_IsFileChanged_ContentAndModeMatch_ReturnsUnchanged(t *testing.T) {
	store := newContainerStore(t)
	seedContainerFile(t, store, testRef, testDest, "same content", 0644)

	changed, existing, existingMode, err := store.IsFileChanged(testRef, testDest, "same content", 0644)
	if err != nil {
		t.Fatalf("IsFileChanged: %v", err)
	}
	if changed {
		t.Error("expected unchanged file to not be marked as changed")
	}
	if existing != "same content" {
		t.Errorf("expected existing %q, got %q", "same content", existing)
	}
	if existingMode != 0644 {
		t.Errorf("expected existingMode 0644, got %04o", existingMode)
	}
}

func TestContainerConfigFileStore_IsFileChanged_ContentDiffers_ReturnsChanged(t *testing.T) {
	store := newContainerStore(t)
	seedContainerFile(t, store, testRef, testDest, "old content", 0644)

	changed, existing, existingMode, err := store.IsFileChanged(testRef, testDest, "new content", 0644)
	if err != nil {
		t.Fatalf("IsFileChanged: %v", err)
	}
	if !changed {
		t.Error("expected modified file to be marked as changed")
	}
	if existing != "old content" {
		t.Errorf("expected existing %q, got %q", "old content", existing)
	}
	if existingMode != 0644 {
		t.Errorf("expected existingMode 0644, got %04o", existingMode)
	}
}

func TestContainerConfigFileStore_IsFileChanged_ModeDiffers_ReturnsChanged(t *testing.T) {
	store := newContainerStore(t)
	seedContainerFile(t, store, testRef, testDest, "content", 0644)

	changed, _, existingMode, err := store.IsFileChanged(testRef, testDest, "content", 0755)
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

func TestContainerConfigFileStore_ListStaleFiles_ReturnsFilesNotInDesiredSet(t *testing.T) {
	store := newContainerStore(t)
	dest2 := model.ContainerMountPath("/etc/other")
	seedContainerFile(t, store, testRef, testDest, "content1", 0644)
	seedContainerFile(t, store, testRef, dest2, "content2", 0644)

	desired := map[string]bool{store.InternalFilename(testDest): true}
	deployed, stale, err := store.ListStaleFiles(testRef, desired)
	if err != nil {
		t.Fatalf("ListStaleFiles: %v", err)
	}
	if len(deployed) != 2 {
		t.Errorf("expected 2 deployed files, got %d", len(deployed))
	}
	if len(stale) != 1 || stale[0] != store.InternalFilename(dest2) {
		t.Errorf("expected stale=[%s], got %v", store.InternalFilename(dest2), stale)
	}
}

func TestContainerConfigFileStore_RemoveFile_DeletesFile(t *testing.T) {
	store := newContainerStore(t)
	seedContainerFile(t, store, testRef, testDest, "content", 0644)

	if err := store.RemoveFile(testRef, store.InternalFilename(testDest)); err != nil {
		t.Fatalf("RemoveFile: %v", err)
	}

	if _, err := os.Stat(store.Resolve(testRef, testDest)); !os.IsNotExist(err) {
		t.Error("expected file to be removed")
	}
}

func TestContainerConfigFileStore_RemoveFile_FileAbsent_Succeeds(t *testing.T) {
	store := newContainerStore(t)

	if err := store.RemoveFile(testRef, "nonexistent"); err != nil {
		t.Fatalf("RemoveFile of nonexistent file: %v", err)
	}
}

func TestContainerConfigFileStore_RemoveUnit_DeletesUnitDirectory(t *testing.T) {
	store := newContainerStore(t)
	seedContainerFile(t, store, testRef, testDest, "content", 0644)

	if err := store.RemoveUnit(testRef); err != nil {
		t.Fatalf("RemoveUnit: %v", err)
	}

	unitDir := filepath.Join(store.BaseDirectory(), testRef.Name())
	if _, err := os.Stat(unitDir); !os.IsNotExist(err) {
		t.Error("expected directory to be removed")
	}
}

func TestContainerConfigFileStore_RemoveUnit_DirectoryAbsent_Succeeds(t *testing.T) {
	store := newContainerStore(t)

	if err := store.RemoveUnit(testRef); err != nil {
		t.Fatalf("RemoveUnit of nonexistent directory: %v", err)
	}
}

func TestContainerConfigFileStore_CurrentDirVersion_NoSymlink_ReturnsZero(t *testing.T) {
	store := newContainerStore(t)

	v, err := store.CurrentDirVersion(testRef, testDest)
	if err != nil {
		t.Fatalf("CurrentDirVersion: %v", err)
	}
	if v != 0 {
		t.Errorf("expected version 0 for empty dir, got %d", v)
	}
}

func TestContainerConfigFileStore_IsVersionedFileChanged_VersionZero_ReturnsChanged(t *testing.T) {
	store := newContainerStore(t)

	changed, err := store.IsVersionedFileChanged(testRef, testDest, 0, "app.conf", 0644, "content")
	if err != nil {
		t.Fatalf("IsVersionedFileChanged: %v", err)
	}
	if !changed {
		t.Error("expected changed=true when no version deployed")
	}
}

func TestContainerConfigFileStore_WriteVersionedDirFile_WritesFileWithMode(t *testing.T) {
	store := newContainerStore(t)

	if err := store.WriteVersionedDirFile(testRef, testDest, 1, "app.conf", 0644, "hello"); err != nil {
		t.Fatalf("WriteVersionedDirFile: %v", err)
	}

	path := filepath.Join(versionedDir(store, testRef, testDest), "1", "app.conf")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading written file: %v", err)
	}
	if string(raw) != "hello" {
		t.Errorf("content: got %q, want %q", string(raw), "hello")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0644 {
		t.Errorf("mode: got %04o, want 0644", info.Mode().Perm())
	}
}

func TestContainerConfigFileStore_UpdateDirSymlink_CreatesDataLinkAndFileSymlinks(t *testing.T) {
	store := newContainerStore(t)
	seedVersionFiles(t, store, testRef, testDest, 1, "app.conf", "hello", 0644)

	if err := store.UpdateDirSymlink(testRef, testDest, 1); err != nil {
		t.Fatalf("UpdateDirSymlink: %v", err)
	}

	dirPath := versionedDir(store, testRef, testDest)

	target, err := os.Readlink(filepath.Join(dirPath, "..data"))
	if err != nil {
		t.Fatalf("reading ..data symlink: %v", err)
	}
	if target != "1" {
		t.Errorf("..data target: got %q, want %q", target, "1")
	}

	fileLink, err := os.Readlink(filepath.Join(dirPath, "app.conf"))
	if err != nil {
		t.Fatalf("reading app.conf symlink: %v", err)
	}
	if fileLink != "..data/app.conf" {
		t.Errorf("app.conf link target: got %q, want %q", fileLink, "..data/app.conf")
	}

	content, err := os.ReadFile(filepath.Join(dirPath, "app.conf"))
	if err != nil {
		t.Fatalf("reading via symlink: %v", err)
	}
	if string(content) != "hello" {
		t.Errorf("content: got %q, want %q", string(content), "hello")
	}
}

func TestContainerConfigFileStore_CurrentDirVersion_AfterDeploy_ReturnsVersion(t *testing.T) {
	store := newContainerStore(t)
	seedDeployedVersion(t, store, testRef, testDest, 1, "app.conf", "x", 0644)

	v, err := store.CurrentDirVersion(testRef, testDest)
	if err != nil {
		t.Fatalf("CurrentDirVersion: %v", err)
	}
	if v != 1 {
		t.Errorf("expected version 1, got %d", v)
	}
}

func TestContainerConfigFileStore_IsVersionedFileChanged_ContentAndModeMatch_ReturnsUnchanged(t *testing.T) {
	store := newContainerStore(t)
	seedDeployedVersion(t, store, testRef, testDest, 1, "app.conf", "content", 0644)

	changed, err := store.IsVersionedFileChanged(testRef, testDest, 1, "app.conf", 0644, "content")
	if err != nil {
		t.Fatalf("IsVersionedFileChanged: %v", err)
	}
	if changed {
		t.Error("expected unchanged after write with same content and mode")
	}
}

func TestContainerConfigFileStore_IsVersionedFileChanged_ContentDiffers_ReturnsChanged(t *testing.T) {
	store := newContainerStore(t)
	seedDeployedVersion(t, store, testRef, testDest, 1, "app.conf", "old", 0644)

	changed, err := store.IsVersionedFileChanged(testRef, testDest, 1, "app.conf", 0644, "new")
	if err != nil {
		t.Fatalf("IsVersionedFileChanged: %v", err)
	}
	if !changed {
		t.Error("expected changed when content differs")
	}
}

func TestContainerConfigFileStore_IsVersionedFileChanged_ModeDiffers_ReturnsChanged(t *testing.T) {
	store := newContainerStore(t)
	seedDeployedVersion(t, store, testRef, testDest, 1, "app.conf", "content", 0644)

	changed, err := store.IsVersionedFileChanged(testRef, testDest, 1, "app.conf", 0755, "content")
	if err != nil {
		t.Fatalf("IsVersionedFileChanged: %v", err)
	}
	if !changed {
		t.Error("expected changed when mode differs")
	}
}

func TestContainerConfigFileStore_PruneOldVersions_RemovesAllExceptKeepVersion(t *testing.T) {
	store := newContainerStore(t)
	seedDeployedVersion(t, store, testRef, testDest, 1, "app.conf", "v1", 0644)
	seedDeployedVersion(t, store, testRef, testDest, 2, "app.conf", "v2", 0644)

	if err := store.PruneOldVersions(testRef, testDest, 2); err != nil {
		t.Fatalf("PruneOldVersions: %v", err)
	}

	dirPath := versionedDir(store, testRef, testDest)
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var versionDirs []string
	for _, e := range entries {
		if e.IsDir() {
			versionDirs = append(versionDirs, e.Name())
		}
	}
	if len(versionDirs) != 1 || versionDirs[0] != "2" {
		t.Errorf("expected only version dir \"2\", got %v", versionDirs)
	}
}

func TestContainerConfigFileStore_ListStaleDirs_ReturnsDirsNotInDesiredSet(t *testing.T) {
	store := newContainerStore(t)
	dest2 := model.ContainerMountPath("/etc/other")
	seedDeployedVersion(t, store, testRef, testDest, 1, "a.conf", "x", 0644)
	seedDeployedVersion(t, store, testRef, dest2, 1, "b.conf", "y", 0644)

	desired := map[string]bool{store.InternalDirname(testDest): true}
	stale, err := store.ListStaleDirs(testRef, desired)
	if err != nil {
		t.Fatalf("ListStaleDirs: %v", err)
	}
	if len(stale) != 1 || stale[0] != store.InternalDirname(dest2) {
		t.Errorf("expected stale=[%s], got %v", store.InternalDirname(dest2), stale)
	}
}

func TestContainerConfigFileStore_RemoveDir_DeletesVersionedDir(t *testing.T) {
	store := newContainerStore(t)
	seedDeployedVersion(t, store, testRef, testDest, 1, "app.conf", "x", 0644)

	if err := store.RemoveDir(testRef, store.InternalDirname(testDest)); err != nil {
		t.Fatalf("RemoveDir: %v", err)
	}

	if _, err := os.Stat(versionedDir(store, testRef, testDest)); !os.IsNotExist(err) {
		t.Error("expected configDir group to be removed")
	}
}

func TestContainerConfigFileStore_RemoveDir_DirectoryAbsent_Succeeds(t *testing.T) {
	store := newContainerStore(t)

	if err := store.RemoveDir(testRef, "nonexistent"); err != nil {
		t.Fatalf("RemoveDir of nonexistent dir: %v", err)
	}
}
