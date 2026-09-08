// Package api loads syslet spec files (JSON archives) and deserializes them into
// domain-level LoadResult values consumed by the loader package.
package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
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

// LoadSpecsFS reads specs from either a zip file or a directory, auto-detecting by trying
// LoadSpecsZip first and falling back to LoadSpecsDir.
func LoadSpecsFS(fs afero.Fs, path string) (LoadResult, error) {
	result, err := LoadSpecsZip(fs, path)
	if err == nil {
		return result, nil
	}
	result, dirErr := LoadSpecsDir(fs, path)
	if dirErr == nil {
		return result, nil
	}
	return LoadResult{}, fmt.Errorf("failed to load specs from %s: not a valid zip file (%v) or directory (%v)", path, err, dirErr)
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

// LoadSpecsZip opens a zip file and reads all .json entries as specs.
func LoadSpecsZip(fs afero.Fs, zipPath string) (LoadResult, error) {
	data, err := afero.ReadFile(fs, zipPath)
	if err != nil {
		return LoadResult{}, fmt.Errorf("reading zip file %s: %w", zipPath, err)
	}
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return LoadResult{}, fmt.Errorf("opening zip %s: %w", zipPath, err)
	}

	var result LoadResult
	for _, f := range r.File {
		if f.FileInfo().IsDir() || filepath.Ext(f.Name) != ".json" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return LoadResult{}, fmt.Errorf("opening %s in zip: %w", f.Name, err)
		}
		fileData, err := io.ReadAll(rc)
		if closeErr := rc.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
		if err != nil {
			return LoadResult{}, fmt.Errorf("reading %s from zip: %w", f.Name, err)
		}
		if err := unmarshalInto(fileData, f.Name, &result); err != nil {
			return LoadResult{}, fmt.Errorf("parsing %s: %w", f.Name, err)
		}
	}

	if result.empty() {
		return LoadResult{}, fmt.Errorf("no .json spec files found in %s", zipPath)
	}
	return result, nil
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
