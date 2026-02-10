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

// Spec is the parsed form of a JSON spec file. It describes a single
// container, volume, or network and maps directly to a Podman quadlet file.
type Spec struct {
	Name         string                    `json:"name"`
	Type         string                    `json:"type"`
	DesiredState string                    `json:"desiredState,omitempty"`
	Unit         map[string]map[string]any `json:"unit"`
	Configs      []ConfigEntry             `json:"configs,omitempty"`
}

// ConfigEntry defines a config file to mount into a container.
type ConfigEntry struct {
	Content          string `json:"content"`
	TargetVolumePath string `json:"targetVolumePath"`
}

// UnitOption is a single key=value in a systemd unit file section.
type UnitOption struct {
	Section string
	Name    string
	Value   string
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

		var s Spec
		if err := json.Unmarshal(data, &s); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", f.Name, err)
		}
		specs = append(specs, s)
	}

	if len(specs) == 0 {
		return nil, fmt.Errorf("no .json spec files found in %s", zipPath)
	}
	return specs, nil
}

// ValidateSpecs checks that a set of specs is internally consistent:
//   - No duplicate unit names (across all types)
//   - All volumes referenced by containers exist as volume specs
//   - All networks referenced by containers exist as network specs
//   - No duplicate targetVolumePath within a single container's configs
//   - Every spec has a valid type
func ValidateSpecs(specs []Spec) error {
	// Collect names per type and globally.
	allNames := make(map[string]string) // name → type (for duplicate check)
	volumes := make(map[string]bool)
	networks := make(map[string]bool)

	for _, s := range specs {
		if s.Name == "" {
			return fmt.Errorf("spec has empty name")
		}
		switch strings.ToLower(s.Type) {
		case "container", "volume", "network":
		default:
			return fmt.Errorf("spec %q: unknown type %q", s.Name, s.Type)
		}

		if existing, ok := allNames[s.Name]; ok {
			return fmt.Errorf("duplicate unit name %q (type %s and %s)", s.Name, existing, s.Type)
		}
		allNames[s.Name] = s.Type

		switch strings.ToLower(s.Type) {
		case "volume":
			volumes[s.Name] = true
		case "network":
			networks[s.Name] = true
		}
	}

	// Validate container references and config paths.
	for _, s := range specs {
		if strings.ToLower(s.Type) != "container" {
			continue
		}

		// Check for duplicate config target paths.
		configPaths := make(map[string]bool)
		for _, cfg := range s.Configs {
			if configPaths[cfg.TargetVolumePath] {
				return fmt.Errorf("container %q: duplicate config targetVolumePath %q", s.Name, cfg.TargetVolumePath)
			}
			configPaths[cfg.TargetVolumePath] = true
		}

		// Check volume and network references in unit options.
		opts, err := flattenUnitOptions(s.Unit)
		if err != nil {
			return fmt.Errorf("container %q: %w", s.Name, err)
		}
		for _, o := range opts {
			if o.Section == "Container" && o.Name == "Volume" {
				volRef := parseVolumeReference(o.Value)
				if volRef != "" && !volumes[volRef] {
					return fmt.Errorf("container %q: references undefined volume %q", s.Name, volRef)
				}
			}
			if o.Section == "Container" && o.Name == "Network" {
				netRef := parseNetworkReference(o.Value)
				if netRef != "" && !networks[netRef] {
					return fmt.Errorf("container %q: references undefined network %q", s.Name, netRef)
				}
			}
		}
	}

	return nil
}

// parseVolumeReference extracts the volume name from a Volume= entry.
// Returns the volume name (without extension) if the source references a
// named volume (e.g. "webapp-data.volume:/data" → "webapp-data").
// Returns "" for host-path bind mounts (e.g. "/host/path:/container/path").
func parseVolumeReference(value string) string {
	parts := strings.SplitN(value, ":", 2)
	if len(parts) < 2 {
		return ""
	}
	source := parts[0]
	if strings.HasSuffix(source, ".volume") {
		return strings.TrimSuffix(source, ".volume")
	}
	return ""
}

// parseNetworkReference extracts the network name from a Network= entry.
// Returns the network name (without extension) if it references a named
// network (e.g. "webapp-net.network" → "webapp-net").
// Returns "" otherwise.
func parseNetworkReference(value string) string {
	if strings.HasSuffix(value, ".network") {
		return strings.TrimSuffix(value, ".network")
	}
	return ""
}

// flattenUnitOptions converts the JSON "unit" map to sorted UnitOptions.
// Sections and keys within each section are sorted alphabetically for
// deterministic output.
func flattenUnitOptions(unitMap map[string]map[string]any) ([]UnitOption, error) {
	sections := make([]string, 0, len(unitMap))
	for s := range unitMap {
		sections = append(sections, s)
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
					s, ok := item.(string)
					if !ok {
						return nil, fmt.Errorf("option %s.%s: expected string value, got %T", section, key, item)
					}
					opts = append(opts, UnitOption{Section: section, Name: key, Value: s})
				}
			default:
				opts = append(opts, UnitOption{Section: section, Name: key, Value: fmt.Sprintf("%v", v)})
			}
		}
	}
	return opts, nil
}

// renderUnitContent converts a Spec into systemd unit file content as a string.
// For containers, it also generates Volume= entries for config file bind mounts
// and adds the [Install] section.
func renderUnitContent(s Spec, containerConfigDir string) (string, error) {
	opts, err := flattenUnitOptions(s.Unit)
	if err != nil {
		return "", err
	}

	var goOpts []*gounit.UnitOption
	for _, o := range opts {
		goOpts = append(goOpts, &gounit.UnitOption{
			Section: o.Section,
			Name:    o.Name,
			Value:   o.Value,
		})
	}

	// Container-specific: add config bind-mount volumes and [Install] section.
	if strings.ToLower(s.Type) == "container" {
		for _, cfg := range s.Configs {
			basename := filepath.Base(cfg.TargetVolumePath)
			hostPath := filepath.Join(containerConfigDir, s.Name, basename)
			goOpts = append(goOpts, &gounit.UnitOption{
				Section: "Container",
				Name:    "Volume",
				Value:   fmt.Sprintf("%s:%s:ro", hostPath, cfg.TargetVolumePath),
			})
		}
		goOpts = append(goOpts, &gounit.UnitOption{
			Section: "Install",
			Name:    "WantedBy",
			Value:   "multi-user.target default.target",
		})
	}

	data, err := io.ReadAll(gounit.Serialize(goOpts))
	if err != nil {
		return "", fmt.Errorf("serializing unit: %w", err)
	}
	return string(data), nil
}
