package containerconfig

import (
	"testing"

	"github.com/spf13/afero"
)

func TestConfigFileManager_Write(t *testing.T) {
	fs := afero.NewMemMapFs()
	mgr := NewConfigFileManager(fs)

	err := mgr.Write("mycontainer", "app.conf", "test content")
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Verify file was written
	content, err := afero.ReadFile(fs, "/etc/containers/config/mycontainer/app.conf")
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}
	if string(content) != "test content" {
		t.Errorf("expected 'test content', got %q", string(content))
	}
}

func TestConfigFileManager_Write_CreatesDirectory(t *testing.T) {
	fs := afero.NewMemMapFs()
	mgr := NewConfigFileManager(fs)

	// Directory doesn't exist yet
	err := mgr.Write("newcontainer", "config.yaml", "yaml content")
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Verify directory was created
	exists, err := afero.DirExists(fs, "/etc/containers/config/newcontainer")
	if err != nil {
		t.Fatalf("failed to check directory: %v", err)
	}
	if !exists {
		t.Error("expected directory to be created")
	}
}

func TestConfigFileManager_IsChanged_NewFile(t *testing.T) {
	fs := afero.NewMemMapFs()
	mgr := NewConfigFileManager(fs)

	// File doesn't exist yet
	changed, err := mgr.IsChanged("webapp", "config.json", "new content")
	if err != nil {
		t.Fatalf("IsChanged failed: %v", err)
	}
	if !changed {
		t.Error("expected new file to be marked as changed")
	}
}

func TestConfigFileManager_IsChanged_UnchangedFile(t *testing.T) {
	fs := afero.NewMemMapFs()
	mgr := NewConfigFileManager(fs)

	// Write initial file
	mgr.Write("webapp", "config.json", "same content")

	// Check with same content
	changed, err := mgr.IsChanged("webapp", "config.json", "same content")
	if err != nil {
		t.Fatalf("IsChanged failed: %v", err)
	}
	if changed {
		t.Error("expected unchanged file to not be marked as changed")
	}
}

func TestConfigFileManager_IsChanged_ModifiedFile(t *testing.T) {
	fs := afero.NewMemMapFs()
	mgr := NewConfigFileManager(fs)

	// Write initial file
	mgr.Write("webapp", "config.json", "old content")

	// Check with different content
	changed, err := mgr.IsChanged("webapp", "config.json", "new content")
	if err != nil {
		t.Fatalf("IsChanged failed: %v", err)
	}
	if !changed {
		t.Error("expected modified file to be marked as changed")
	}
}

func TestConfigFileManager_ListFiles_EmptyDirectory(t *testing.T) {
	fs := afero.NewMemMapFs()
	mgr := NewConfigFileManager(fs)

	// Directory doesn't exist
	files, err := mgr.ListFiles("nonexistent")
	if err != nil {
		t.Fatalf("ListFiles failed: %v", err)
	}
	if files != nil {
		t.Errorf("expected nil for nonexistent directory, got %v", files)
	}
}

func TestConfigFileManager_ListFiles_WithFiles(t *testing.T) {
	fs := afero.NewMemMapFs()
	mgr := NewConfigFileManager(fs)

	// Write multiple files
	mgr.Write("webapp", "app.conf", "content1")
	mgr.Write("webapp", "db.conf", "content2")
	mgr.Write("webapp", "cache.conf", "content3")

	files, err := mgr.ListFiles("webapp")
	if err != nil {
		t.Fatalf("ListFiles failed: %v", err)
	}
	if len(files) != 3 {
		t.Errorf("expected 3 files, got %d: %v", len(files), files)
	}

	// Check that all files are present
	fileMap := make(map[string]bool)
	for _, f := range files {
		fileMap[f] = true
	}
	for _, expected := range []string{"app.conf", "db.conf", "cache.conf"} {
		if !fileMap[expected] {
			t.Errorf("expected file %q not found in %v", expected, files)
		}
	}
}

func TestConfigFileManager_ListFiles_IgnoresDirectories(t *testing.T) {
	fs := afero.NewMemMapFs()
	mgr := NewConfigFileManager(fs)

	// Create a subdirectory within the container config dir
	mgr.Write("webapp", "app.conf", "content")
	fs.MkdirAll("/etc/containers/config/webapp/subdir", 0755)

	files, err := mgr.ListFiles("webapp")
	if err != nil {
		t.Fatalf("ListFiles failed: %v", err)
	}
	if len(files) != 1 {
		t.Errorf("expected 1 file (ignoring subdirectories), got %d: %v", len(files), files)
	}
}

func TestConfigFileManager_RemoveFile(t *testing.T) {
	fs := afero.NewMemMapFs()
	mgr := NewConfigFileManager(fs)

	// Write file
	mgr.Write("webapp", "old.conf", "content")

	// Remove it
	err := mgr.RemoveFile("webapp", "old.conf")
	if err != nil {
		t.Fatalf("RemoveFile failed: %v", err)
	}

	// Verify it's gone
	files, _ := mgr.ListFiles("webapp")
	if len(files) != 0 {
		t.Errorf("expected no files after removal, got %v", files)
	}
}

func TestConfigFileManager_RemoveFile_NonexistentOK(t *testing.T) {
	fs := afero.NewMemMapFs()
	mgr := NewConfigFileManager(fs)

	// Removing a nonexistent file should not error
	err := mgr.RemoveFile("webapp", "nonexistent.conf")
	if err != nil {
		t.Fatalf("RemoveFile of nonexistent file failed: %v", err)
	}
}

func TestConfigFileManager_RemoveAll(t *testing.T) {
	fs := afero.NewMemMapFs()
	mgr := NewConfigFileManager(fs)

	// Write multiple files
	mgr.Write("webapp", "app.conf", "content1")
	mgr.Write("webapp", "db.conf", "content2")

	// Remove all
	err := mgr.RemoveAll("webapp")
	if err != nil {
		t.Fatalf("RemoveAll failed: %v", err)
	}

	// Verify directory is gone
	exists, _ := afero.DirExists(fs, "/etc/containers/config/webapp")
	if exists {
		t.Error("expected directory to be removed")
	}
}

func TestConfigFileManager_RemoveAll_NonexistentOK(t *testing.T) {
	fs := afero.NewMemMapFs()
	mgr := NewConfigFileManager(fs)

	// Removing nonexistent directory should not error
	err := mgr.RemoveAll("nonexistent")
	if err != nil {
		t.Fatalf("RemoveAll of nonexistent directory failed: %v", err)
	}
}

func TestConfigFileManager_ListContainers_Empty(t *testing.T) {
	fs := afero.NewMemMapFs()
	mgr := NewConfigFileManager(fs)

	containers, err := mgr.ListContainers()
	if err != nil {
		t.Fatalf("ListContainers failed: %v", err)
	}
	if containers != nil {
		t.Errorf("expected nil for empty base directory, got %v", containers)
	}
}

func TestConfigFileManager_ListContainers_WithContainers(t *testing.T) {
	fs := afero.NewMemMapFs()
	mgr := NewConfigFileManager(fs)

	// Create config files for multiple containers
	mgr.Write("webapp", "app.conf", "content1")
	mgr.Write("database", "db.conf", "content2")
	mgr.Write("cache", "cache.conf", "content3")

	containers, err := mgr.ListContainers()
	if err != nil {
		t.Fatalf("ListContainers failed: %v", err)
	}
	if len(containers) != 3 {
		t.Errorf("expected 3 containers, got %d: %v", len(containers), containers)
	}

	// Check that all containers are present
	containerMap := make(map[string]bool)
	for _, c := range containers {
		containerMap[c] = true
	}
	for _, expected := range []string{"webapp", "database", "cache"} {
		if !containerMap[expected] {
			t.Errorf("expected container %q not found in %v", expected, containers)
		}
	}
}

func TestConfigFileManager_ListContainers_IgnoresFiles(t *testing.T) {
	fs := afero.NewMemMapFs()
	mgr := NewConfigFileManager(fs)

	// Create a container
	mgr.Write("webapp", "app.conf", "content")

	// Create a file directly in base dir (should be ignored)
	afero.WriteFile(fs, "/etc/containers/config/somefile.txt", []byte("ignore"), 0644)

	containers, err := mgr.ListContainers()
	if err != nil {
		t.Fatalf("ListContainers failed: %v", err)
	}
	if len(containers) != 1 || containers[0] != "webapp" {
		t.Errorf("expected only 'webapp', got %v", containers)
	}
}
