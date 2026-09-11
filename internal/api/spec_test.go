package api

import (
	"path/filepath"
	"strings"
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

// --- JSON-stream loading tests ---

// TestLoadSpecsReader_InputShapes verifies that the three stream shapes a deploy
// can produce — a top-level array, newline-delimited JSON, and `cat`-style
// concatenated objects — all parse to the same set of specs.
func TestLoadSpecsReader_InputShapes(t *testing.T) {
	cases := map[string]string{
		"array":         "[" + containerJSON + "," + volumeJSON + "," + networkJSON + "]",
		"ndjson":        containerJSON + "\n" + volumeJSON + "\n" + networkJSON + "\n",
		"concatenated":  containerJSON + volumeJSON + networkJSON,
		"prettyCatLike": "  " + containerJSON + "\n\n  " + volumeJSON + "\n" + networkJSON + "\n",
	}

	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			result, err := LoadSpecsReader(strings.NewReader(input))
			if err != nil {
				t.Fatalf("LoadSpecsReader failed: %v", err)
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
		})
	}
}

func TestLoadSpecsReader_ErrorCases(t *testing.T) {
	tests := map[string]string{
		"empty":          "",
		"whitespace":     "   \n\t ",
		"invalidJSON":    `{"type":"container"`,
		"unknownType":    `{"type":"unknown","name":"x"}`,
		"unknownInArray": "[" + containerJSON + `,{"type":"unknown","name":"x"}]`,
	}

	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadSpecsReader(strings.NewReader(input)); err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

// --- LoadSpecsStream tests ---

// TestLoadSpecsStream_WithPersistPath asserts the stream is decoded and the raw
// bytes are persisted (parent dirs created) so a caller keeps a re-applyable record.
func TestLoadSpecsStream_WithPersistPath(t *testing.T) {
	fs := afero.NewMemMapFs()
	input := containerJSON + "\n" + volumeJSON + "\n"

	result, err := LoadSpecsStream(fs, strings.NewReader(input), "/etc/syslet/config.json")
	if err != nil {
		t.Fatalf("LoadSpecsStream failed: %v", err)
	}
	if len(result.Containers) != 1 || len(result.Volumes) != 1 {
		t.Errorf("decoded %d containers and %d volumes, want 1 and 1", len(result.Containers), len(result.Volumes))
	}

	persisted, err := afero.ReadFile(fs, "/etc/syslet/config.json")
	if err != nil {
		t.Fatalf("expected stream to be persisted: %v", err)
	}
	if string(persisted) != input {
		t.Errorf("persisted content = %q, want %q", persisted, input)
	}
}

// TestLoadSpecsStream_WithoutPersistPath asserts an empty persist path decodes
// without writing anything (the dry-run case).
func TestLoadSpecsStream_WithoutPersistPath(t *testing.T) {
	fs := afero.NewMemMapFs()

	result, err := LoadSpecsStream(fs, strings.NewReader(containerJSON), "")
	if err != nil {
		t.Fatalf("LoadSpecsStream failed: %v", err)
	}
	if len(result.Containers) != 1 {
		t.Errorf("expected 1 container, got %d", len(result.Containers))
	}

	files, _ := afero.ReadDir(fs, "/")
	if len(files) != 0 {
		t.Errorf("expected no files written, got %d", len(files))
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

	t.Run("json stream file", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		writeSpecFile(t, fs, "/config.json", containerJSON+volumeJSON)

		result, err := LoadSpecsFS(fs, "/config.json")
		if err != nil {
			t.Fatalf("LoadSpecsFS (file) failed: %v", err)
		}
		if len(result.Containers) != 1 || len(result.Volumes) != 1 {
			t.Errorf("expected 1 container and 1 volume from file, got %d and %d", len(result.Containers), len(result.Volumes))
		}
	})

	t.Run("invalid file", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		writeSpecFile(t, fs, "/notafile.txt", "not json")

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
