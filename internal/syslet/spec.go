// Package syslet implements the core logic for reading, validating, and
// applying declarative container specs. It reads JSON specs from a zip file,
// validates cross-references, and orchestrates the generation of quadlet
// unit files and container config files on the host.
package syslet

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	gounit "github.com/coreos/go-systemd/v22/unit"
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
}

// ContainerSpec describes a container and maps to a .container quadlet file.
// It includes container-specific configuration like desired state and config files.
type ContainerSpec struct {
	Name         string                    `json:"name"`
	Unit         map[string]map[string]any `json:"unit"`
	DesiredState string                    `json:"desiredState,omitempty"`
	Configs      []ConfigEntry             `json:"configs,omitempty"`
}

// ConfigEntry defines a config file to mount into a container.
type ConfigEntry struct {
	Content          string `json:"content"`
	TargetVolumePath string `json:"targetVolumePath"`
}

// VolumeSpec describes a volume and maps to a .volume quadlet file.
type VolumeSpec struct {
	Name string                    `json:"name"`
	Unit map[string]map[string]any `json:"unit"`
}

// NetworkSpec describes a network and maps to a .network quadlet file.
type NetworkSpec struct {
	Name string                    `json:"name"`
	Unit map[string]map[string]any `json:"unit"`
}

// RenderedUnit holds a spec and its rendered unit options before serialization.
// The Content field is populated after serialization for use during apply.
type RenderedUnit struct {
	Spec        Spec
	UnitOptions []UnitOption
	Content     string // serialized form (computed after validation)
}

// UnitOption is a single key=value in a systemd unit file section.
type UnitOption struct {
	Section string
	Name    string
	Value   string
}

// Interface implementations
func (s *ContainerSpec) GetName() string                    { return s.Name }
func (s *ContainerSpec) GetType() SpecType                  { return SpecTypeContainer }
func (s *ContainerSpec) GetUnit() map[string]map[string]any { return s.Unit }

func (s *VolumeSpec) GetName() string                    { return s.Name }
func (s *VolumeSpec) GetType() SpecType                  { return SpecTypeVolume }
func (s *VolumeSpec) GetUnit() map[string]map[string]any { return s.Unit }

func (s *NetworkSpec) GetName() string                    { return s.Name }
func (s *NetworkSpec) GetType() SpecType                  { return SpecTypeNetwork }
func (s *NetworkSpec) GetUnit() map[string]map[string]any { return s.Unit }

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

// renderContainer converts a ContainerSpec into a RenderedUnit with flattened UnitOptions.
// It processes the JSON "unit" map, adds config bind-mounts, and the [Install] section.
// Sections and keys are sorted alphabetically for deterministic output.
func renderContainer(s *ContainerSpec, containerConfigDir string) (RenderedUnit, error) {
	opts, err := flattenUnitMap(s.GetUnit())
	if err != nil {
		return RenderedUnit{}, err
	}

	// Add config bind-mount volumes.
	for _, cfg := range s.Configs {
		basename := filepath.Base(cfg.TargetVolumePath)
		hostPath := filepath.Join(containerConfigDir, s.Name, basename)
		opts = append(opts, UnitOption{
			Section: "Container",
			Name:    "Volume",
			Value:   fmt.Sprintf("%s:%s:ro", hostPath, cfg.TargetVolumePath),
		})
	}

	// Add [Install] section.
	opts = append(opts, UnitOption{
		Section: "Install",
		Name:    "WantedBy",
		Value:   "multi-user.target default.target",
	})

	return RenderedUnit{Spec: s, UnitOptions: opts}, nil
}

// renderVolume converts a VolumeSpec into a RenderedUnit with flattened UnitOptions.
// Sections and keys are sorted alphabetically for deterministic output.
func renderVolume(s *VolumeSpec) (RenderedUnit, error) {
	opts, err := flattenUnitMap(s.GetUnit())
	if err != nil {
		return RenderedUnit{}, err
	}
	return RenderedUnit{Spec: s, UnitOptions: opts}, nil
}

// renderNetwork converts a NetworkSpec into a RenderedUnit with flattened UnitOptions.
// Sections and keys are sorted alphabetically for deterministic output.
func renderNetwork(s *NetworkSpec) (RenderedUnit, error) {
	opts, err := flattenUnitMap(s.GetUnit())
	if err != nil {
		return RenderedUnit{}, err
	}
	return RenderedUnit{Spec: s, UnitOptions: opts}, nil
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

// serializeUnitOptions converts unit options to systemd unit file content.
func serializeUnitOptions(opts []UnitOption) (string, error) {
	var goOpts []*gounit.UnitOption
	for _, o := range opts {
		goOpts = append(goOpts, &gounit.UnitOption{
			Section: o.Section,
			Name:    o.Name,
			Value:   o.Value,
		})
	}

	data, err := io.ReadAll(gounit.Serialize(goOpts))
	if err != nil {
		return "", fmt.Errorf("serializing unit: %w", err)
	}
	return string(data), nil
}
