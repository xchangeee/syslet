// Package api loads syslet spec files (a directory of JSON files or a JSON
// stream of spec objects) and deserializes them into domain-level LoadResult
// values consumed by the loader package.
package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/spf13/afero"
)

// RawContainerSpec is the JSON deserialization target for a container spec file.
type RawContainerSpec struct {
	Type           string                    `json:"type"`
	Name           string                    `json:"name"`
	Unit           map[string]map[string]any `json:"unit"`
	DesiredState   string                    `json:"desiredState,omitempty"`
	RemovalAllowed bool                      `json:"removalAllowed,omitempty"`
	Configs        []RawConfigEntry          `json:"configs,omitempty"`
	ConfigDirs     []RawConfigDirEntry       `json:"configDirs,omitempty"`
}

// RawVolumeSpec is the JSON deserialization target for a volume spec file.
type RawVolumeSpec struct {
	Type           string                    `json:"type"`
	Name           string                    `json:"name"`
	Unit           map[string]map[string]any `json:"unit"`
	RemovalAllowed bool                      `json:"removalAllowed,omitempty"`
	ReclaimPolicy  string                    `json:"reclaimPolicy,omitempty"`
}

// RawNetworkSpec is the JSON deserialization target for a network spec file.
type RawNetworkSpec struct {
	Type           string                    `json:"type"`
	Name           string                    `json:"name"`
	Unit           map[string]map[string]any `json:"unit"`
	RemovalAllowed bool                      `json:"removalAllowed,omitempty"`
	ReclaimPolicy  string                    `json:"reclaimPolicy,omitempty"`
}

// RawBuildSpec is the JSON deserialization target for a build spec file.
type RawBuildSpec struct {
	Type          string                    `json:"type"`
	Name          string                    `json:"name"`
	Unit          map[string]map[string]any `json:"unit"`
	ReclaimPolicy string                    `json:"reclaimPolicy,omitempty"`
	Containerfile string                    `json:"containerfile"`
	Configs       []RawBuildFileEntry       `json:"configs,omitempty"`
}

// RawConfigEntry is the JSON deserialization target for a container config entry.
type RawConfigEntry struct {
	MountPath string `json:"mountPath"`
	Mode      string `json:"mode,omitempty"`
	Content   string `json:"content"`
}

// RawConfigDirEntry is the JSON deserialization target for a container configDir entry.
// A configDir is a directory bind-mounted into the container; its files are updated
// atomically via versioned symlinks, enabling in-place reload without restarting.
type RawConfigDirEntry struct {
	MountPath string             `json:"mountPath"`
	Files     []RawConfigDirFile `json:"files,omitempty"`
}

// RawConfigDirFile is a single file within a RawConfigDirEntry.
type RawConfigDirFile struct {
	Name    string `json:"name"`
	Mode    string `json:"mode,omitempty"`
	Content string `json:"content"`
}

// RawBuildFileEntry is the JSON deserialization target for a build context file entry.
type RawBuildFileEntry struct {
	Filename string `json:"filename"`
	Mode     string `json:"mode,omitempty"`
	Content  string `json:"content"`
}

// RawSecretSpec is the JSON deserialization target for a secret spec file.
// Ciphertext holds the raw SOPS-encrypted YAML file content as a plain string
// (not base64) so spec authors can embed the SOPS YAML text directly. Conversion
// to model.Ciphertext ([]byte) happens in the loader.
type RawSecretSpec struct {
	Type       string `json:"type"`
	Name       string `json:"name"`
	Ciphertext string `json:"ciphertext"`
}

// LoadResult groups the deserialized raw specs by type.
type LoadResult struct {
	Containers []RawContainerSpec
	Volumes    []RawVolumeSpec
	Networks   []RawNetworkSpec
	Builds     []RawBuildSpec
	Secrets    []RawSecretSpec
}

// LoadSpecsFS reads specs from a path, dispatching on whether it is a directory
// (each .json file is one spec) or a regular file containing a JSON stream of
// specs (an array, NDJSON, or concatenated objects — see LoadSpecsReader).
func LoadSpecsFS(fs afero.Fs, path string) (LoadResult, error) {
	info, err := fs.Stat(path)
	if err != nil {
		return LoadResult{}, fmt.Errorf("reading %s: %w", path, err)
	}
	if info.IsDir() {
		return LoadSpecsDir(fs, path)
	}
	f, err := fs.Open(path)
	if err != nil {
		return LoadResult{}, fmt.Errorf("opening %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	result, err := LoadSpecsReader(f)
	if err != nil {
		return LoadResult{}, fmt.Errorf("loading specs from %s: %w", path, err)
	}
	return result, nil
}

// LoadSpecsStream reads a JSON spec stream from r and decodes it via
// LoadSpecsReader. When persistPath is non-empty the raw stream is first written
// there (creating parent directories) so callers such as the daemon can keep a
// re-applyable on-disk record of what was applied; pass "" to decode without
// persisting (e.g. a dry-run). The persist path layout is the caller's policy —
// this function only honors the path it is given.
func LoadSpecsStream(fs afero.Fs, r io.Reader, persistPath string) (LoadResult, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return LoadResult{}, fmt.Errorf("reading spec stream: %w", err)
	}
	if persistPath != "" {
		dir := filepath.Dir(persistPath)
		if err := fs.MkdirAll(dir, 0755); err != nil {
			return LoadResult{}, fmt.Errorf("creating %s: %w", dir, err)
		}
		if err := afero.WriteFile(fs, persistPath, data, 0600); err != nil {
			return LoadResult{}, fmt.Errorf("persisting %s: %w", persistPath, err)
		}
	}
	return LoadSpecsReader(bytes.NewReader(data))
}

// LoadSpecsDir reads all .json files from a directory.
func LoadSpecsDir(fs afero.Fs, dirPath string) (LoadResult, error) {
	entries, err := afero.ReadDir(fs, dirPath)
	if err != nil {
		return LoadResult{}, fmt.Errorf("reading directory %s: %w", dirPath, err)
	}

	var result LoadResult
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dirPath, entry.Name())
		data, err := afero.ReadFile(fs, path)
		if err != nil {
			return LoadResult{}, fmt.Errorf("reading %s: %w", path, err)
		}
		if err := unmarshalInto(data, entry.Name(), &result); err != nil {
			return LoadResult{}, fmt.Errorf("parsing %s: %w", entry.Name(), err)
		}
	}

	if result.empty() {
		return LoadResult{}, fmt.Errorf("no .json spec files found in directory %s", dirPath)
	}
	return result, nil
}

// LoadSpecsReader reads a JSON stream of specs from r. It accepts the shapes a
// deploy can produce over stdin or from a persisted config.json: a top-level
// JSON array of spec objects, newline-delimited JSON, or whitespace/`cat`-
// concatenated objects (e.g. `cat dir/*.json`). Each object is dispatched
// through unmarshalInto by its "type" field.
func LoadSpecsReader(r io.Reader) (LoadResult, error) {
	var result LoadResult
	dec := json.NewDecoder(r)
	for i := 0; ; i++ {
		var msg json.RawMessage
		if err := dec.Decode(&msg); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return LoadResult{}, fmt.Errorf("decoding spec stream: %w", err)
		}
		if err := addStreamValue(msg, i, &result); err != nil {
			return LoadResult{}, err
		}
	}

	if result.empty() {
		return LoadResult{}, fmt.Errorf("no specs found in input")
	}
	return result, nil
}

// addStreamValue handles a single decoded JSON value from a spec stream. A value
// may itself be an array (the top-level-array form) whose elements are specs, or
// a single spec object (the NDJSON / concatenated-object form).
func addStreamValue(msg json.RawMessage, index int, result *LoadResult) error {
	trimmed := bytesTrimLeadingSpace(msg)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var arr []json.RawMessage
		if err := json.Unmarshal(msg, &arr); err != nil {
			return fmt.Errorf("parsing spec array: %w", err)
		}
		for j, elem := range arr {
			if err := unmarshalInto(elem, fmt.Sprintf("spec[%d]", j), result); err != nil {
				return fmt.Errorf("parsing spec[%d]: %w", j, err)
			}
		}
		return nil
	}
	if err := unmarshalInto(msg, fmt.Sprintf("spec %d", index), result); err != nil {
		return fmt.Errorf("parsing spec %d: %w", index, err)
	}
	return nil
}

// bytesTrimLeadingSpace returns b without leading JSON whitespace, used to peek
// at the first significant byte of a decoded value.
func bytesTrimLeadingSpace(b []byte) []byte {
	for len(b) > 0 {
		switch b[0] {
		case ' ', '\t', '\r', '\n':
			b = b[1:]
		default:
			return b
		}
	}
	return b
}

func (r LoadResult) empty() bool {
	return len(r.Containers) == 0 && len(r.Volumes) == 0 && len(r.Networks) == 0 && len(r.Builds) == 0 && len(r.Secrets) == 0
}

// unmarshalInto peeks at the type field and appends the deserialized spec to the correct slice.
func unmarshalInto(data []byte, filename string, result *LoadResult) error {
	var peek struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &peek); err != nil {
		return err
	}

	switch peek.Type {
	case "container", "Container":
		var s RawContainerSpec
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		result.Containers = append(result.Containers, s)
	case "volume", "Volume":
		var s RawVolumeSpec
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		result.Volumes = append(result.Volumes, s)
	case "network", "Network":
		var s RawNetworkSpec
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		result.Networks = append(result.Networks, s)
	case "build", "Build":
		var s RawBuildSpec
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		result.Builds = append(result.Builds, s)
	case "secret", "Secret":
		var s RawSecretSpec
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		result.Secrets = append(result.Secrets, s)
	default:
		return fmt.Errorf("unknown spec type %q in %s", peek.Type, filename)
	}
	return nil
}
