package api

import (
	"archive/zip"
	"bytes"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
)

const (
	containerJSON = `{"type":"container","name":"web","unit":{"Container":{"Image":"nginx:latest"}}}`
	volumeJSON    = `{"type":"volume","name":"data","unit":{"Volume":{}}}`
	networkJSON   = `{"type":"network","name":"backend","unit":{"Network":{}}}`
)

func writeSpecFile(t *testing.T, fs afero.Fs, path, content string) {
	t.Helper()
	if err := afero.WriteFile(fs, path, []byte(content), 0644); err != nil {
		t.Fatalf("writeSpecFile(%q): %v", path, err)
	}
}

func makeZipFS(t *testing.T, fs afero.Fs, zipPath string, entries map[string]string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("makeZipFS create %q: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("makeZipFS write %q: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("makeZipFS close: %v", err)
	}
	if err := afero.WriteFile(fs, zipPath, buf.Bytes(), 0644); err != nil {
		t.Fatalf("makeZipFS write zip file: %v", err)
	}
}

// --- Directory loading tests ---

func TestLoadSpecsFromDirectory(t *testing.T) {
	fs := afero.NewMemMapFs()
	testDir := "/testspecs"
	if err := fs.Mkdir(testDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	writeSpecFile(t, fs, filepath.Join(testDir, "container.json"), containerJSON)
	writeSpecFile(t, fs, filepath.Join(testDir, "volume.json"), volumeJSON)
	writeSpecFile(t, fs, filepath.Join(testDir, "network.json"), networkJSON)

	result, err := LoadSpecsDir(fs, testDir)
	if err != nil {
		t.Fatalf("LoadSpecsDir failed: %v", err)
	}
	if len(result.Containers) != 1 {
		t.Errorf("expected 1 container, got %d", len(result.Containers))
	}
	if len(result.Volumes) != 1 {
		t.Errorf("expected 1 volume, got %d", len(result.Volumes))
	}
	if len(result.Networks) != 1 {
		t.Errorf("expected 1 network, got %d", len(result.Networks))
	}
}

func TestLoadSpecsFromDirectory_NonJSONFiles(t *testing.T) {
	fs := afero.NewMemMapFs()
	testDir := "/mixed"
	if err := fs.Mkdir(testDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	writeSpecFile(t, fs, filepath.Join(testDir, "container.json"), containerJSON)
	writeSpecFile(t, fs, filepath.Join(testDir, "README.md"), "# README")
	writeSpecFile(t, fs, filepath.Join(testDir, "notes.txt"), "some notes")
	writeSpecFile(t, fs, filepath.Join(testDir, "config.yaml"), "key: value")
	if err := fs.Mkdir(filepath.Join(testDir, "subdir"), 0755); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}
	writeSpecFile(t, fs, filepath.Join(testDir, "subdir", "volume.json"), volumeJSON)

	result, err := LoadSpecsDir(fs, testDir)
	if err != nil {
		t.Fatalf("LoadSpecsDir failed: %v", err)
	}
	if len(result.Containers) != 1 {
		t.Errorf("expected 1 container, got %d", len(result.Containers))
	}
	if len(result.Volumes) != 0 {
		t.Errorf("expected 0 volumes (subdir ignored), got %d", len(result.Volumes))
	}
	if result.Containers[0].Name != "web" {
		t.Errorf("expected name 'web', got %s", result.Containers[0].Name)
	}
}

func TestLoadSpecsFromDirectory_ErrorCases(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T, fs afero.Fs) string
		wantErr bool
	}{
		{
			name: "no json files",
			setup: func(t *testing.T, fs afero.Fs) string {
				if err := fs.Mkdir("/empty", 0755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				return "/empty"
			},
			wantErr: true,
		},
		{
			name: "invalid json",
			setup: func(t *testing.T, fs afero.Fs) string {
				if err := fs.Mkdir("/invalid", 0755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				writeSpecFile(t, fs, "/invalid/bad.json", `{"type":"container","name":"web"`)
				return "/invalid"
			},
			wantErr: true,
		},
		{
			name: "unknown spec type",
			setup: func(t *testing.T, fs afero.Fs) string {
				if err := fs.Mkdir("/unknown", 0755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				writeSpecFile(t, fs, "/unknown/x.json", `{"type":"unknown","name":"test"}`)
				return "/unknown"
			},
			wantErr: true,
		},
		{
			name: "nonexistent directory",
			setup: func(t *testing.T, fs afero.Fs) string {
				return "/nonexistent/directory"
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			path := tt.setup(t, fs)
			_, err := LoadSpecsDir(fs, path)
			if (err == nil) == tt.wantErr {
				t.Errorf("wantErr=%v, got err=%v", tt.wantErr, err)
			}
		})
	}
}

// --- Zip loading tests ---

func TestLoadSpecsFromZip(t *testing.T) {
	fs := afero.NewMemMapFs()
	makeZipFS(t, fs, "/specs.zip", map[string]string{
		"container.json":      containerJSON,
		"volume.json":         volumeJSON,
		"subdir/network.json": networkJSON,
	})

	result, err := LoadSpecsZip(fs, "/specs.zip")
	if err != nil {
		t.Fatalf("LoadSpecsZip failed: %v", err)
	}
	if len(result.Containers) != 1 {
		t.Errorf("expected 1 container, got %d", len(result.Containers))
	}
	if len(result.Volumes) != 1 {
		t.Errorf("expected 1 volume, got %d", len(result.Volumes))
	}
	if len(result.Networks) != 1 {
		t.Errorf("expected 1 network, got %d", len(result.Networks))
	}
}

func TestLoadSpecsFromZip_NonJSONFiles(t *testing.T) {
	fs := afero.NewMemMapFs()
	makeZipFS(t, fs, "/mixed.zip", map[string]string{
		"container.json": containerJSON,
		"README.txt":     "This is a readme",
	})

	result, err := LoadSpecsZip(fs, "/mixed.zip")
	if err != nil {
		t.Fatalf("LoadSpecsZip failed: %v", err)
	}
	if len(result.Containers) != 1 {
		t.Errorf("expected 1 container, got %d", len(result.Containers))
	}
}

func TestLoadSpecsFromZip_ErrorCases(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, fs afero.Fs) string
	}{
		{
			name: "empty zip",
			setup: func(t *testing.T, fs afero.Fs) string {
				makeZipFS(t, fs, "/empty.zip", nil)
				return "/empty.zip"
			},
		},
		{
			name: "not a zip file",
			setup: func(t *testing.T, fs afero.Fs) string {
				writeSpecFile(t, fs, "/notzip.zip", "not a zip file")
				return "/notzip.zip"
			},
		},
		{
			name: "invalid json in zip",
			setup: func(t *testing.T, fs afero.Fs) string {
				makeZipFS(t, fs, "/invalid.zip", map[string]string{
					"invalid.json": `{"type":"container"`,
				})
				return "/invalid.zip"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			path := tt.setup(t, fs)
			_, err := LoadSpecsZip(fs, path)
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

// --- LoadSpecsFS auto-detection tests ---

func TestLoadSpecs(t *testing.T) {
	t.Run("directory", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		if err := fs.Mkdir("/testdir", 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		writeSpecFile(t, fs, "/testdir/container.json", containerJSON)

		result, err := LoadSpecsFS(fs, "/testdir")
		if err != nil {
			t.Fatalf("LoadSpecsFS (directory) failed: %v", err)
		}
		if len(result.Containers) != 1 {
			t.Errorf("expected 1 container from directory, got %d", len(result.Containers))
		}
	})

	t.Run("zip", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		makeZipFS(t, fs, "/specs.zip", map[string]string{"volume.json": volumeJSON})

		result, err := LoadSpecsFS(fs, "/specs.zip")
		if err != nil {
			t.Fatalf("LoadSpecsFS (zip) failed: %v", err)
		}
		if len(result.Volumes) != 1 {
			t.Errorf("expected 1 volume from zip, got %d", len(result.Volumes))
		}
	})

	t.Run("invalid file", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		writeSpecFile(t, fs, "/notafile.txt", "not a zip or directory")

		_, err := LoadSpecsFS(fs, "/notafile.txt")
		if err == nil {
			t.Error("expected error for invalid path")
		}
	})

	t.Run("nonexistent", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		_, err := LoadSpecsFS(fs, "/nonexistent/path")
		if err == nil {
			t.Error("expected error for nonexistent path")
		}
	})
}

// --- unmarshalInto tests ---

func TestUnmarshalInto_AllTypes(t *testing.T) {
	tests := []struct {
		name      string
		json      string
		checkFunc func(t *testing.T, result LoadResult)
	}{
		{
			name: "container",
			json: `{"type":"container","name":"web","unit":{"Container":{"Image":"nginx"}}}`,
			checkFunc: func(t *testing.T, result LoadResult) {
				if len(result.Containers) != 1 {
					t.Fatalf("expected 1 container, got %d", len(result.Containers))
				}
				if result.Containers[0].Name != "web" {
					t.Errorf("expected name 'web', got %q", result.Containers[0].Name)
				}
			},
		},
		{
			name: "volume",
			json: `{"type":"volume","name":"data","unit":{"Volume":{}}}`,
			checkFunc: func(t *testing.T, result LoadResult) {
				if len(result.Volumes) != 1 || result.Volumes[0].Name != "data" {
					t.Errorf("expected volume 'data', got %+v", result.Volumes)
				}
			},
		},
		{
			name: "network",
			json: `{"type":"network","name":"backend","unit":{"Network":{}}}`,
			checkFunc: func(t *testing.T, result LoadResult) {
				if len(result.Networks) != 1 || result.Networks[0].Name != "backend" {
					t.Errorf("expected network 'backend', got %+v", result.Networks)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result LoadResult
			if err := unmarshalInto([]byte(tt.json), tt.name+".json", &result); err != nil {
				t.Fatalf("unmarshalInto failed: %v", err)
			}
			tt.checkFunc(t, result)
		})
	}
}

func TestUnmarshalInto_Errors(t *testing.T) {
	tests := []struct {
		name string
		json string
	}{
		{"invalid_json", `{"type":"container"`},
		{"unknown_type", `{"type":"unknown","name":"test"}`},
		{"missing_type", `{"name":"test"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result LoadResult
			if err := unmarshalInto([]byte(tt.json), tt.name+".json", &result); err == nil {
				t.Error("expected error for invalid spec")
			}
		})
	}
}
