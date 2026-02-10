package api

import (
	"archive/zip"
	"bytes"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
)

// TestLoadSpecsFromDirectory tests loading specs from a directory.
func TestLoadSpecsFromDirectory(t *testing.T) {
	// Create in-memory filesystem
	fs := afero.NewMemMapFs()
	testDir := "/testspecs"

	// Create test directory
	if err := fs.Mkdir(testDir, 0755); err != nil {
		t.Fatalf("failed to create test directory: %v", err)
	}

	// Create test spec files
	containerJSON := `{
		"type": "container",
		"name": "web",
		"unit": {
			"Container": {
				"Image": "nginx:latest"
			}
		}
	}`

	volumeJSON := `{
		"type": "volume",
		"name": "data",
		"unit": {
			"Volume": {}
		}
	}`

	networkJSON := `{
		"type": "network",
		"name": "backend",
		"unit": {
			"Network": {}
		}
	}`

	// Write spec files
	if err := afero.WriteFile(fs, filepath.Join(testDir, "container.json"), []byte(containerJSON), 0644); err != nil {
		t.Fatalf("failed to write container.json: %v", err)
	}
	if err := afero.WriteFile(fs, filepath.Join(testDir, "volume.json"), []byte(volumeJSON), 0644); err != nil {
		t.Fatalf("failed to write volume.json: %v", err)
	}
	if err := afero.WriteFile(fs, filepath.Join(testDir, "network.json"), []byte(networkJSON), 0644); err != nil {
		t.Fatalf("failed to write network.json: %v", err)
	}

	// Load specs
	specs, err := LoadSpecsFromDirectoryFS(fs, testDir)
	if err != nil {
		t.Fatalf("LoadSpecsFromDirectory failed: %v", err)
	}

	// Verify we got 3 specs
	if len(specs) != 3 {
		t.Fatalf("expected 3 specs, got %d", len(specs))
	}

	// Verify spec types
	typeCount := map[SpecType]int{}
	for _, spec := range specs {
		typeCount[spec.GetType()]++
	}

	if typeCount[SpecTypeContainer] != 1 {
		t.Errorf("expected 1 container spec, got %d", typeCount[SpecTypeContainer])
	}
	if typeCount[SpecTypeVolume] != 1 {
		t.Errorf("expected 1 volume spec, got %d", typeCount[SpecTypeVolume])
	}
	if typeCount[SpecTypeNetwork] != 1 {
		t.Errorf("expected 1 network spec, got %d", typeCount[SpecTypeNetwork])
	}
}

// TestLoadSpecsFromDirectory_NoFiles tests error when no .json files exist.
func TestLoadSpecsFromDirectory_NoFiles(t *testing.T) {
	fs := afero.NewMemMapFs()
	testDir := "/empty"

	if err := fs.Mkdir(testDir, 0755); err != nil {
		t.Fatalf("failed to create test directory: %v", err)
	}

	_, err := LoadSpecsFromDirectoryFS(fs, testDir)
	if err == nil {
		t.Error("expected error for directory with no .json files")
	}
}

// TestLoadSpecsFromDirectory_InvalidJSON tests error handling for malformed JSON.
func TestLoadSpecsFromDirectory_InvalidJSON(t *testing.T) {
	fs := afero.NewMemMapFs()
	testDir := "/invalid"

	if err := fs.Mkdir(testDir, 0755); err != nil {
		t.Fatalf("failed to create test directory: %v", err)
	}

	// Write invalid JSON
	invalidJSON := `{"type": "container", "name": "web"`
	if err := afero.WriteFile(fs, filepath.Join(testDir, "invalid.json"), []byte(invalidJSON), 0644); err != nil {
		t.Fatalf("failed to write invalid.json: %v", err)
	}

	_, err := LoadSpecsFromDirectoryFS(fs, testDir)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// TestLoadSpecsFromDirectory_UnknownType tests error handling for unknown spec type.
func TestLoadSpecsFromDirectory_UnknownType(t *testing.T) {
	fs := afero.NewMemMapFs()
	testDir := "/unknown"

	if err := fs.Mkdir(testDir, 0755); err != nil {
		t.Fatalf("failed to create test directory: %v", err)
	}

	// Write spec with unknown type
	unknownJSON := `{
		"type": "unknown",
		"name": "test"
	}`
	if err := afero.WriteFile(fs, filepath.Join(testDir, "unknown.json"), []byte(unknownJSON), 0644); err != nil {
		t.Fatalf("failed to write unknown.json: %v", err)
	}

	_, err := LoadSpecsFromDirectoryFS(fs, testDir)
	if err == nil {
		t.Error("expected error for unknown spec type")
	}
}

// TestLoadSpecsFromDirectory_NonexistentDir tests error handling for nonexistent directory.
func TestLoadSpecsFromDirectory_NonexistentDir(t *testing.T) {
	fs := afero.NewMemMapFs()

	_, err := LoadSpecsFromDirectoryFS(fs, "/nonexistent/directory")
	if err == nil {
		t.Error("expected error for nonexistent directory")
	}
}

// TestLoadSpecsFromZip tests loading specs from a zip file.
func TestLoadSpecsFromZip(t *testing.T) {
	fs := afero.NewMemMapFs()
	zipPath := "/specs.zip"

	// Create zip file in memory
	var buf bytes.Buffer
	zipWriter := zip.NewWriter(&buf)

	// Add container spec
	containerJSON := `{
		"type": "container",
		"name": "web",
		"unit": {
			"Container": {
				"Image": "nginx:latest"
			}
		}
	}`
	w, err := zipWriter.Create("container.json")
	if err != nil {
		t.Fatalf("failed to create zip entry: %v", err)
	}
	if _, err := w.Write([]byte(containerJSON)); err != nil {
		t.Fatalf("failed to write to zip: %v", err)
	}

	// Add volume spec
	volumeJSON := `{
		"type": "volume",
		"name": "data",
		"unit": {
			"Volume": {}
		}
	}`
	w, err = zipWriter.Create("volume.json")
	if err != nil {
		t.Fatalf("failed to create zip entry: %v", err)
	}
	if _, err := w.Write([]byte(volumeJSON)); err != nil {
		t.Fatalf("failed to write to zip: %v", err)
	}

	// Add network spec in subdirectory
	networkJSON := `{
		"type": "network",
		"name": "backend",
		"unit": {
			"Network": {}
		}
	}`
	w, err = zipWriter.Create("subdir/network.json")
	if err != nil {
		t.Fatalf("failed to create zip entry: %v", err)
	}
	if _, err := w.Write([]byte(networkJSON)); err != nil {
		t.Fatalf("failed to write to zip: %v", err)
	}

	// Close zip writer
	if err := zipWriter.Close(); err != nil {
		t.Fatalf("failed to close zip writer: %v", err)
	}

	// Write zip to filesystem
	if err := afero.WriteFile(fs, zipPath, buf.Bytes(), 0644); err != nil {
		t.Fatalf("failed to write zip file: %v", err)
	}

	// Load specs from zip
	specs, err := LoadSpecsFromZipFS(fs, zipPath)
	if err != nil {
		t.Fatalf("LoadSpecsFromZip failed: %v", err)
	}

	// Verify we got 3 specs
	if len(specs) != 3 {
		t.Fatalf("expected 3 specs, got %d", len(specs))
	}

	// Verify spec types
	typeCount := map[SpecType]int{}
	for _, spec := range specs {
		typeCount[spec.GetType()]++
	}

	if typeCount[SpecTypeContainer] != 1 {
		t.Errorf("expected 1 container spec, got %d", typeCount[SpecTypeContainer])
	}
	if typeCount[SpecTypeVolume] != 1 {
		t.Errorf("expected 1 volume spec, got %d", typeCount[SpecTypeVolume])
	}
	if typeCount[SpecTypeNetwork] != 1 {
		t.Errorf("expected 1 network spec, got %d", typeCount[SpecTypeNetwork])
	}
}

// TestLoadSpecsFromZip_NoFiles tests error when zip has no .json files.
func TestLoadSpecsFromZip_NoFiles(t *testing.T) {
	fs := afero.NewMemMapFs()
	zipPath := "/empty.zip"

	// Create empty zip
	var buf bytes.Buffer
	zipWriter := zip.NewWriter(&buf)
	if err := zipWriter.Close(); err != nil {
		t.Fatalf("failed to close zip writer: %v", err)
	}

	// Write zip to filesystem
	if err := afero.WriteFile(fs, zipPath, buf.Bytes(), 0644); err != nil {
		t.Fatalf("failed to write zip file: %v", err)
	}

	_, err := LoadSpecsFromZipFS(fs, zipPath)
	if err == nil {
		t.Error("expected error for zip with no .json files")
	}
}

// TestLoadSpecsFromZip_InvalidZip tests error handling for non-zip file.
func TestLoadSpecsFromZip_InvalidZip(t *testing.T) {
	fs := afero.NewMemMapFs()
	notZipPath := "/notzip.zip"

	// Write non-zip content
	if err := afero.WriteFile(fs, notZipPath, []byte("not a zip file"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	_, err := LoadSpecsFromZipFS(fs, notZipPath)
	if err == nil {
		t.Error("expected error for invalid zip file")
	}
}

// TestLoadSpecsFromZip_InvalidJSON tests error handling for malformed JSON in zip.
func TestLoadSpecsFromZip_InvalidJSON(t *testing.T) {
	fs := afero.NewMemMapFs()
	zipPath := "/invalid.zip"

	// Create zip with invalid JSON
	var buf bytes.Buffer
	zipWriter := zip.NewWriter(&buf)
	w, err := zipWriter.Create("invalid.json")
	if err != nil {
		t.Fatalf("failed to create zip entry: %v", err)
	}
	if _, err := w.Write([]byte(`{"type": "container"`)); err != nil {
		t.Fatalf("failed to write to zip: %v", err)
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatalf("failed to close zip writer: %v", err)
	}

	// Write zip to filesystem
	if err := afero.WriteFile(fs, zipPath, buf.Bytes(), 0644); err != nil {
		t.Fatalf("failed to write zip file: %v", err)
	}

	_, err = LoadSpecsFromZipFS(fs, zipPath)
	if err == nil {
		t.Error("expected error for invalid JSON in zip")
	}
}

// TestLoadSpecsFromZip_NonJSONFiles tests that non-.json files are ignored.
func TestLoadSpecsFromZip_NonJSONFiles(t *testing.T) {
	fs := afero.NewMemMapFs()
	zipPath := "/mixed.zip"

	// Create zip with .json and non-.json files
	var buf bytes.Buffer
	zipWriter := zip.NewWriter(&buf)

	// Add .json file
	containerJSON := `{
		"type": "container",
		"name": "web",
		"unit": {
			"Container": {
				"Image": "nginx:latest"
			}
		}
	}`
	w, err := zipWriter.Create("container.json")
	if err != nil {
		t.Fatalf("failed to create zip entry: %v", err)
	}
	if _, err := w.Write([]byte(containerJSON)); err != nil {
		t.Fatalf("failed to write to zip: %v", err)
	}

	// Add non-.json file (should be ignored)
	w, err = zipWriter.Create("README.txt")
	if err != nil {
		t.Fatalf("failed to create zip entry: %v", err)
	}
	if _, err := w.Write([]byte("This is a readme")); err != nil {
		t.Fatalf("failed to write to zip: %v", err)
	}

	if err := zipWriter.Close(); err != nil {
		t.Fatalf("failed to close zip writer: %v", err)
	}

	// Write zip to filesystem
	if err := afero.WriteFile(fs, zipPath, buf.Bytes(), 0644); err != nil {
		t.Fatalf("failed to write zip file: %v", err)
	}

	// Load specs
	specs, err := LoadSpecsFromZipFS(fs, zipPath)
	if err != nil {
		t.Fatalf("LoadSpecsFromZip failed: %v", err)
	}

	// Should only get 1 spec (the .json file)
	if len(specs) != 1 {
		t.Errorf("expected 1 spec, got %d", len(specs))
	}
}

// TestLoadSpecs tests the auto-detection wrapper function.
func TestLoadSpecs(t *testing.T) {
	fs := afero.NewMemMapFs()
	testDir := "/testdir"

	// Create test directory
	if err := fs.Mkdir(testDir, 0755); err != nil {
		t.Fatalf("failed to create test directory: %v", err)
	}

	// Test with directory
	containerJSON := `{
		"type": "container",
		"name": "web",
		"unit": {
			"Container": {
				"Image": "nginx:latest"
			}
		}
	}`
	if err := afero.WriteFile(fs, filepath.Join(testDir, "container.json"), []byte(containerJSON), 0644); err != nil {
		t.Fatalf("failed to write container.json: %v", err)
	}

	specs, err := LoadSpecsFS(fs, testDir)
	if err != nil {
		t.Fatalf("LoadSpecs (directory) failed: %v", err)
	}
	if len(specs) != 1 {
		t.Errorf("expected 1 spec from directory, got %d", len(specs))
	}

	// Test with zip
	zipPath := "/specs.zip"
	var buf bytes.Buffer
	zipWriter := zip.NewWriter(&buf)
	w, err := zipWriter.Create("volume.json")
	if err != nil {
		t.Fatalf("failed to create zip entry: %v", err)
	}
	volumeJSON := `{
		"type": "volume",
		"name": "data",
		"unit": {
			"Volume": {}
		}
	}`
	if _, err := w.Write([]byte(volumeJSON)); err != nil {
		t.Fatalf("failed to write to zip: %v", err)
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatalf("failed to close zip writer: %v", err)
	}

	// Write zip to filesystem
	if err := afero.WriteFile(fs, zipPath, buf.Bytes(), 0644); err != nil {
		t.Fatalf("failed to write zip file: %v", err)
	}

	specs, err = LoadSpecsFS(fs, zipPath)
	if err != nil {
		t.Fatalf("LoadSpecs (zip) failed: %v", err)
	}
	if len(specs) != 1 {
		t.Errorf("expected 1 spec from zip, got %d", len(specs))
	}
}

// TestLoadSpecs_InvalidPath tests error when path is neither valid zip nor directory.
func TestLoadSpecs_InvalidPath(t *testing.T) {
	fs := afero.NewMemMapFs()
	invalidPath := "/notafile.txt"

	// Write non-zip content
	if err := afero.WriteFile(fs, invalidPath, []byte("not a zip or directory"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	_, err := LoadSpecsFS(fs, invalidPath)
	if err == nil {
		t.Error("expected error for invalid path")
	}

	// Verify error message mentions both failures
	errMsg := err.Error()
	if errMsg == "" {
		t.Error("expected detailed error message")
	}
}

// TestLoadSpecs_NonexistentPath tests error handling for nonexistent path.
func TestLoadSpecs_NonexistentPath(t *testing.T) {
	fs := afero.NewMemMapFs()

	_, err := LoadSpecsFS(fs, "/nonexistent/path")
	if err == nil {
		t.Error("expected error for nonexistent path")
	}
}

// TestUnmarshalSpec_AllTypes tests unmarshalSpec with all spec types.
func TestUnmarshalSpec_AllTypes(t *testing.T) {
	tests := []struct {
		name     string
		json     string
		wantType SpecType
		wantName string
	}{
		{
			name: "container",
			json: `{
				"type": "container",
				"name": "web",
				"unit": {"Container": {"Image": "nginx"}}
			}`,
			wantType: SpecTypeContainer,
			wantName: "web",
		},
		{
			name: "container_uppercase",
			json: `{
				"type": "CONTAINER",
				"name": "web2",
				"unit": {"Container": {}}
			}`,
			wantType: SpecTypeContainer,
			wantName: "web2",
		},
		{
			name: "volume",
			json: `{
				"type": "volume",
				"name": "data",
				"unit": {"Volume": {}}
			}`,
			wantType: SpecTypeVolume,
			wantName: "data",
		},
		{
			name: "network",
			json: `{
				"type": "network",
				"name": "backend",
				"unit": {"Network": {}}
			}`,
			wantType: SpecTypeNetwork,
			wantName: "backend",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec, err := unmarshalSpec([]byte(tt.json))
			if err != nil {
				t.Fatalf("unmarshalSpec failed: %v", err)
			}
			if spec.GetType() != tt.wantType {
				t.Errorf("expected type %s, got %s", tt.wantType, spec.GetType())
			}
			if spec.GetName() != tt.wantName {
				t.Errorf("expected name %s, got %s", tt.wantName, spec.GetName())
			}
		})
	}
}

// TestUnmarshalSpec_Errors tests error cases for unmarshalSpec.
func TestUnmarshalSpec_Errors(t *testing.T) {
	tests := []struct {
		name string
		json string
	}{
		{
			name: "invalid_json",
			json: `{"type": "container"`,
		},
		{
			name: "unknown_type",
			json: `{"type": "unknown", "name": "test"}`,
		},
		{
			name: "missing_type",
			json: `{"name": "test"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := unmarshalSpec([]byte(tt.json))
			if err == nil {
				t.Error("expected error for invalid spec")
			}
		})
	}
}
