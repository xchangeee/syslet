package api

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
)

const (
	containerJSON = `{"apiVersion":"v1","type":"container","name":"web","unit":{"Container":{"Image":"nginx:latest"}}}`
	volumeJSON    = `{"apiVersion":"v1","type":"volume","name":"data","unit":{"Volume":{}}}`
	networkJSON   = `{"apiVersion":"v1","type":"network","name":"backend","unit":{"Network":{}}}`
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
	if len(result.V1.Containers) != 1 {
		t.Errorf("expected 1 container, got %d", len(result.V1.Containers))
	}
	if len(result.V1.Volumes) != 1 {
		t.Errorf("expected 1 volume, got %d", len(result.V1.Volumes))
	}
	if len(result.V1.Networks) != 1 {
		t.Errorf("expected 1 network, got %d", len(result.V1.Networks))
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
	if len(result.V1.Containers) != 1 {
		t.Errorf("expected 1 container, got %d", len(result.V1.Containers))
	}
	if len(result.V1.Volumes) != 0 {
		t.Errorf("expected 0 volumes (subdir ignored), got %d", len(result.V1.Volumes))
	}
	if result.V1.Containers[0].Name != "web" {
		t.Errorf("expected name 'web', got %s", result.V1.Containers[0].Name)
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
				t.Helper()
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
				t.Helper()
				if err := fs.Mkdir("/invalid", 0755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				writeSpecFile(t, fs, "/invalid/bad.json", `{"apiVersion":"v1","type":"container","name":"web"`)
				return "/invalid"
			},
			wantErr: true,
		},
		{
			name: "unknown spec type",
			setup: func(t *testing.T, fs afero.Fs) string {
				t.Helper()
				if err := fs.Mkdir("/unknown", 0755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				writeSpecFile(t, fs, "/unknown/x.json", `{"apiVersion":"v1","type":"unknown","name":"test"}`)
				return "/unknown"
			},
			wantErr: true,
		},
		{
			name: "nonexistent directory",
			setup: func(_ *testing.T, _ afero.Fs) string {
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
			if len(result.V1.Containers) != 1 {
				t.Errorf("expected 1 container, got %d", len(result.V1.Containers))
			}
			if len(result.V1.Volumes) != 1 {
				t.Errorf("expected 1 volume, got %d", len(result.V1.Volumes))
			}
			if len(result.V1.Networks) != 1 {
				t.Errorf("expected 1 network, got %d", len(result.V1.Networks))
			}
		})
	}
}

// TestLoadSpecsReader_EmptyArray_ReturnsEmpty asserts that an explicit empty
// array is a valid desired state: nothing is declared, so everything whose
// removal is allowed gets pruned. Input with no JSON value at all stays an
// error (see TestLoadSpecsReader_ErrorCases), since that is what a broken
// pipe or failed export produces.
func TestLoadSpecsReader_EmptyArray_ReturnsEmpty(t *testing.T) {
	result, err := LoadSpecsReader(strings.NewReader("[]\n"))
	if err != nil {
		t.Fatalf("LoadSpecsReader failed: %v", err)
	}
	if !result.empty() {
		t.Errorf("expected no specs, got %+v", result.V1)
	}
}

func TestLoadSpecsReader_ErrorCases(t *testing.T) {
	tests := map[string]string{
		"empty":          "",
		"whitespace":     "   \n\t ",
		"invalidJSON":    `{"apiVersion":"v1","type":"container"`,
		"unknownType":    `{"apiVersion":"v1","type":"unknown","name":"x"}`,
		"unknownInArray": "[" + containerJSON + `,{"apiVersion":"v1","type":"unknown","name":"x"}]`,
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
	if len(result.V1.Containers) != 1 || len(result.V1.Volumes) != 1 {
		t.Errorf("decoded %d containers and %d volumes, want 1 and 1", len(result.V1.Containers), len(result.V1.Volumes))
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
	if len(result.V1.Containers) != 1 {
		t.Errorf("expected 1 container, got %d", len(result.V1.Containers))
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
		if len(result.V1.Containers) != 1 {
			t.Errorf("expected 1 container from directory, got %d", len(result.V1.Containers))
		}
	})

	t.Run("json stream file", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		writeSpecFile(t, fs, "/config.json", containerJSON+volumeJSON)

		result, err := LoadSpecsFS(fs, "/config.json")
		if err != nil {
			t.Fatalf("LoadSpecsFS (file) failed: %v", err)
		}
		if len(result.V1.Containers) != 1 || len(result.V1.Volumes) != 1 {
			t.Errorf("expected 1 container and 1 volume from file, got %d and %d", len(result.V1.Containers), len(result.V1.Volumes))
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
			json: `{"apiVersion":"v1","type":"container","name":"web","unit":{"Container":{"Image":"nginx"}}}`,
			checkFunc: func(t *testing.T, result LoadResult) {
				t.Helper()
				if len(result.V1.Containers) != 1 {
					t.Fatalf("expected 1 container, got %d", len(result.V1.Containers))
				}
				if result.V1.Containers[0].Name != "web" {
					t.Errorf("expected name 'web', got %q", result.V1.Containers[0].Name)
				}
			},
		},
		{
			name: "volume",
			json: `{"apiVersion":"v1","type":"volume","name":"data","unit":{"Volume":{}}}`,
			checkFunc: func(t *testing.T, result LoadResult) {
				t.Helper()
				if len(result.V1.Volumes) != 1 || result.V1.Volumes[0].Name != "data" {
					t.Errorf("expected volume 'data', got %+v", result.V1.Volumes)
				}
			},
		},
		{
			name: "network",
			json: `{"apiVersion":"v1","type":"network","name":"backend","unit":{"Network":{}}}`,
			checkFunc: func(t *testing.T, result LoadResult) {
				t.Helper()
				if len(result.V1.Networks) != 1 || result.V1.Networks[0].Name != "backend" {
					t.Errorf("expected network 'backend', got %+v", result.V1.Networks)
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
		{"invalid_json", `{"apiVersion":"v1","type":"container"`},
		{"unknown_type", `{"apiVersion":"v1","type":"unknown","name":"test"}`},
		{"missing_type", `{"apiVersion":"v1","name":"test"}`},
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

// --- apiVersion tests ---

// TestAPIVersion_V1_Accepted verifies that a spec with apiVersion "v1" loads as v1.
func TestAPIVersion_V1_Accepted(t *testing.T) {
	result, err := LoadSpecsReader(strings.NewReader(containerJSON))
	if err != nil {
		t.Fatalf("LoadSpecsReader failed: %v", err)
	}
	if len(result.V1.Containers) != 1 {
		t.Errorf("expected 1 v1 container, got %d", len(result.V1.Containers))
	}
}

// TestAPIVersion_Missing_ReturnsError verifies that apiVersion is required: a
// spec without it is rejected in every input shape instead of being guessed as
// v1, so the version a spec was written for is always explicit.
func TestAPIVersion_Missing_ReturnsError(t *testing.T) {
	missingJSON := `{"type":"container","name":"web","unit":{"Container":{"Image":"nginx"}}}`

	t.Run("directory", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		if err := fs.Mkdir("/specs", 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		writeSpecFile(t, fs, "/specs/web.json", missingJSON)
		_, err := LoadSpecsDir(fs, "/specs")
		if err == nil || !strings.Contains(err.Error(), "apiVersion is required") {
			t.Errorf("expected missing apiVersion error, got %v", err)
		}
	})

	streams := map[string]string{
		"single": missingJSON,
		"ndjson": containerJSON + "\n" + missingJSON + "\n",
		"array":  "[" + containerJSON + "," + missingJSON + "]",
	}
	for name, input := range streams {
		t.Run(name, func(t *testing.T) {
			_, err := LoadSpecsReader(strings.NewReader(input))
			if err == nil || !strings.Contains(err.Error(), "apiVersion is required") {
				t.Errorf("expected missing apiVersion error, got %v", err)
			}
		})
	}
}

// TestAPIVersion_Unsupported verifies that an unknown apiVersion is rejected in
// every input shape, so a spec written for a newer syslet is never half-understood.
func TestAPIVersion_Unsupported(t *testing.T) {
	futureJSON := `{"apiVersion":"v2","type":"container","name":"web","unit":{}}`
	garbageJSON := `{"apiVersion":"foo","type":"container","name":"web","unit":{}}`

	t.Run("directory", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		if err := fs.Mkdir("/specs", 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		writeSpecFile(t, fs, "/specs/web.json", futureJSON)
		_, err := LoadSpecsDir(fs, "/specs")
		if err == nil || !strings.Contains(err.Error(), `apiVersion "v2" is not supported`) {
			t.Errorf("expected unsupported apiVersion error, got %v", err)
		}
	})

	streams := map[string]string{
		"ndjson":  containerJSON + "\n" + futureJSON + "\n",
		"array":   "[" + containerJSON + "," + futureJSON + "]",
		"garbage": garbageJSON,
	}
	for name, input := range streams {
		t.Run(name, func(t *testing.T) {
			_, err := LoadSpecsReader(strings.NewReader(input))
			if err == nil || !strings.Contains(err.Error(), "is not supported") {
				t.Errorf("expected unsupported apiVersion error, got %v", err)
			}
		})
	}
}
