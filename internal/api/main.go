package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/afero"
)

// SpecType identifies the kind of systemd quadlet unit.
type SpecType string

const (
	SpecTypeContainer SpecType = "container"
	SpecTypeVolume    SpecType = "volume"
	SpecTypeNetwork   SpecType = "network"
	SpecTypeBuild     SpecType = "build"
)

// Spec is the interface implemented by all spec types.
// Each spec maps to a Podman quadlet file (.container, .volume, or .network).
type Spec interface {
	GetName() string
	GetType() SpecType
	GetUnit() map[string]map[string]UnitValue
	FullUnitName() string
}

// LoadSpecsFS reads specs from either a directory or a zip file using the provided filesystem.
// It auto-detects the type based on whether the path is a directory or file.
func LoadSpecsFS(fs afero.Fs, path string) ([]Spec, error) {
	// Try to open as a zip first
	specs, err := LoadSpecsFromZipFS(fs, path)
	if err == nil {
		return specs, nil
	}

	// If zip open failed, try as directory
	specs, dirErr := LoadSpecsFromDirectoryFS(fs, path)
	if dirErr == nil {
		return specs, nil
	}

	// Both failed - return most informative error
	return nil, fmt.Errorf("failed to load specs from %s: not a valid zip file (%v) or directory (%v)", path, err, dirErr)
}

// LoadSpecsFromDirectoryFS reads all .json files from a directory using the provided filesystem.
// This is useful for webhookd scenarios where specs are in a git repository.
func LoadSpecsFromDirectoryFS(fs afero.Fs, dirPath string) ([]Spec, error) {
	// Read directory entries
	entries, err := afero.ReadDir(fs, dirPath)
	if err != nil {
		return nil, fmt.Errorf("reading directory %s: %w", dirPath, err)
	}

	var specs []Spec
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		path := filepath.Join(dirPath, entry.Name())
		data, err := afero.ReadFile(fs, path)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}

		spec, err := unmarshalSpec(data)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", entry.Name(), err)
		}
		specs = append(specs, spec)
	}

	if len(specs) == 0 {
		return nil, fmt.Errorf("no .json spec files found in directory %s", dirPath)
	}

	return specs, nil
}

// LoadSpecsFromZipFS opens a zip file from the provided filesystem and reads all .json entries as specs.
// Files are read entirely in memory; the zip is not extracted to disk.
func LoadSpecsFromZipFS(fs afero.Fs, zipPath string) ([]Spec, error) {
	// Read the entire zip file into memory
	data, err := afero.ReadFile(fs, zipPath)
	if err != nil {
		return nil, fmt.Errorf("reading zip file %s: %w", zipPath, err)
	}

	// Create a reader from the byte slice
	readerAt := bytes.NewReader(data)
	r, err := zip.NewReader(readerAt, int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("opening zip %s: %w", zipPath, err)
	}

	var specs []Spec
	for _, f := range r.File {
		if f.FileInfo().IsDir() || filepath.Ext(f.Name) != ".json" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("opening %s in zip: %w", f.Name, err)
		}
		fileData, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return nil, fmt.Errorf("reading %s from zip: %w", f.Name, err)
		}

		spec, err := unmarshalSpec(fileData)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", f.Name, err)
		}
		specs = append(specs, spec)
	}

	if len(specs) == 0 {
		return nil, fmt.Errorf("no .json spec files found in %s", zipPath)
	}
	return specs, nil
}

// unmarshalSpec unmarshals JSON data into the appropriate Spec type based on
// the "type" field. This helper handles the type discrimination for the JSON.
func unmarshalSpec(data []byte) (Spec, error) {
	// First, peek at the type field to determine which struct to unmarshal into
	var peek struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &peek); err != nil {
		return nil, err
	}

	switch strings.ToLower(peek.Type) {
	case "container":
		var s ContainerSpec
		if err := json.Unmarshal(data, &s); err != nil {
			return nil, err
		}
		return &s, nil
	case "volume":
		var s VolumeSpec
		if err := json.Unmarshal(data, &s); err != nil {
			return nil, err
		}
		return &s, nil
	case "network":
		var s NetworkSpec
		if err := json.Unmarshal(data, &s); err != nil {
			return nil, err
		}
		return &s, nil
	case "build":
		var s BuildSpec
		if err := json.Unmarshal(data, &s); err != nil {
			return nil, err
		}
		return &s, nil
	default:
		return nil, fmt.Errorf("unknown spec type: %q", peek.Type)
	}
}

// ensureUnitOption adds a UnitOption if it doesn't already exist in the slice.
// This is used to set default values that the user can override in their specs.
func ensureUnitOption(opts *[]UnitOption, section, name, value string) {
	// Check if the option already exists
	for _, opt := range *opts {
		if opt.Section == section && opt.Name == name {
			// Option already exists, don't add it
			return
		}
	}
	// Option doesn't exist, prepend it to the beginning
	*opts = append([]UnitOption{{Section: section, Name: name, Value: value}}, *opts...)
}

// flattenUnitMap converts a unit map into flattened UnitOptions.
// This is a helper function used by the type-specific render functions.
// Sections and keys are sorted alphabetically for deterministic output.
func flattenUnitMap(unitMap map[string]map[string]UnitValue) ([]UnitOption, error) {
	sections := make([]string, 0, len(unitMap))
	for section := range unitMap {
		sections = append(sections, section)
	}
	sort.Strings(sections)

	var opts []UnitOption
	for _, section := range sections {
		keys := make([]string, 0, len(unitMap[section]))
		for k := range unitMap[section] {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, key := range keys {
			unitValue := unitMap[section][key]
			// Expand each value in the UnitValue into a separate UnitOption.
			// This allows array values to produce multiple options with the same key.
			for _, v := range unitValue.Values() {
				opts = append(opts, UnitOption{Section: section, Name: key, Value: v})
			}
		}
	}
	return opts, nil
}
