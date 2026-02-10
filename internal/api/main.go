package api

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SpecType identifies the kind of systemd quadlet unit.
type SpecType string

const (
	SpecTypeContainer SpecType = "container"
	SpecTypeVolume    SpecType = "volume"
	SpecTypeNetwork   SpecType = "network"
)

// Spec is the interface implemented by all spec types.
// Each spec maps to a Podman quadlet file (.container, .volume, or .network).
type Spec interface {
	GetName() string
	GetType() SpecType
	GetUnit() map[string]map[string]any
	FullUnitName() string
}

// LoadSpecs reads specs from either a directory or a zip file.
// It auto-detects the type based on whether the path is a directory or file.
func LoadSpecs(path string) ([]Spec, error) {
	// Try to open as a zip first
	specs, err := LoadSpecsFromZip(path)
	if err == nil {
		return specs, nil
	}

	// If zip open failed, try as directory
	specs, dirErr := LoadSpecsFromDirectory(path)
	if dirErr == nil {
		return specs, nil
	}

	// Both failed - return most informative error
	return nil, fmt.Errorf("failed to load specs from %s: not a valid zip file (%v) or directory (%v)", path, err, dirErr)
}

// LoadSpecsFromDirectory reads all .json files from a directory.
// This is useful for webhookd scenarios where specs are in a git repository.
func LoadSpecsFromDirectory(dirPath string) ([]Spec, error) {
	pattern := filepath.Join(dirPath, "*.json")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("globbing %s: %w", pattern, err)
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("no .json spec files found in directory %s", dirPath)
	}

	var specs []Spec
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}

		spec, err := unmarshalSpec(data)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", filepath.Base(path), err)
		}
		specs = append(specs, spec)
	}

	return specs, nil
}

// LoadSpecsFromZip opens a zip file and reads all .json entries as specs.
// Files are read entirely in memory; the zip is not extracted to disk.
func LoadSpecsFromZip(zipPath string) ([]Spec, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("opening zip %s: %w", zipPath, err)
	}
	defer r.Close()

	var specs []Spec
	for _, f := range r.File {
		if f.FileInfo().IsDir() || filepath.Ext(f.Name) != ".json" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("opening %s in zip: %w", f.Name, err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("reading %s from zip: %w", f.Name, err)
		}

		spec, err := unmarshalSpec(data)
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
func flattenUnitMap(unitMap map[string]map[string]any) ([]UnitOption, error) {
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
			val := unitMap[section][key]
			switch v := val.(type) {
			case string:
				opts = append(opts, UnitOption{Section: section, Name: key, Value: v})
			case []any:
				for _, item := range v {
					str, ok := item.(string)
					if !ok {
						return nil, fmt.Errorf("option %s.%s: expected string value, got %T", section, key, item)
					}
					opts = append(opts, UnitOption{Section: section, Name: key, Value: str})
				}
			default:
				opts = append(opts, UnitOption{Section: section, Name: key, Value: fmt.Sprintf("%v", v)})
			}
		}
	}
	return opts, nil
}
