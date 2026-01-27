package spec

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"codeberg.org/xchangeee/rsystemd/internal/config"
	"codeberg.org/xchangeee/rsystemd/internal/parser"

	"github.com/coreos/go-systemd/v22/unit"
)

// Convert transforms a ContainerSpec into a ParsedUnit and a list of ConfigFiles.
// configBase is the host directory where config files are stored (e.g. /etc/containers/config).
func Convert(s *ContainerSpec, configBase string) (*parser.ParsedUnit, []config.ConfigFile, error) {
	unitType, err := parseType(s.Type)
	if err != nil {
		return nil, nil, err
	}

	unitFilename := s.Name + "." + s.Type

	// Build unit options from the spec's unit map.
	var opts []*unit.UnitOption

	// Sort sections for deterministic output.
	sections := sortedKeys(s.Unit)
	for _, section := range sections {
		keys := sortedMapKeys(s.Unit[section])
		for _, key := range keys {
			values, err := toStringSlice(s.Unit[section][key])
			if err != nil {
				return nil, nil, fmt.Errorf("section %q key %q: %w", section, key, err)
			}
			for _, v := range values {
				opts = append(opts, &unit.UnitOption{
					Section: section,
					Name:    key,
					Value:   v,
				})
			}
		}
	}

	// Process configs: generate Volume= entries and ConfigFile list.
	var cfgFiles []config.ConfigFile
	seen := make(map[string]bool)

	for _, ce := range s.Configs {
		basename := filepath.Base(ce.TargetVolumePath)
		if seen[basename] {
			return nil, nil, fmt.Errorf("duplicate config basename %q (from targetVolumePath %q)", basename, ce.TargetVolumePath)
		}
		seen[basename] = true

		hostPath := filepath.Join(configBase, s.Name, basename)
		volumeEntry := fmt.Sprintf("%s:%s:ro", hostPath, ce.TargetVolumePath)

		opts = append(opts, &unit.UnitOption{
			Section: "Container",
			Name:    "Volume",
			Value:   volumeEntry,
		})

		cfgFiles = append(cfgFiles, config.ConfigFile{
			UnitName: s.Name,
			Filename: basename,
			Content:  ce.Content,
		})
	}

	// Auto-generate [Install] section for startable units.
	if unitType == parser.UnitTypeContainer {
		opts = append(opts, &unit.UnitOption{
			Section: "Install",
			Name:    "WantedBy",
			Value:   "multi-user.target default.target",
		})
	}

	// Inject [X-Rsystemd] DesiredState.
	if s.DesiredState != "" {
		opts = append(opts, &unit.UnitOption{
			Section: "X-Rsystemd",
			Name:    "DesiredState",
			Value:   s.DesiredState,
		})
	}

	// Serialize to INI for RawContent.
	rawBytes, err := io.ReadAll(unit.Serialize(opts))
	if err != nil {
		return nil, nil, fmt.Errorf("serializing unit: %w", err)
	}

	desiredState := parser.DesiredStateUnspecified
	switch strings.ToLower(s.DesiredState) {
	case "running":
		desiredState = parser.DesiredStateRunning
	case "stopped":
		desiredState = parser.DesiredStateStopped
	}

	pu := &parser.ParsedUnit{
		Name:         unitFilename,
		Type:         unitType,
		DesiredState: desiredState,
		RawContent:   string(rawBytes),
		Options:      opts,
	}

	return pu, cfgFiles, nil
}

func parseType(t string) (parser.UnitType, error) {
	switch strings.ToLower(t) {
	case "container":
		return parser.UnitTypeContainer, nil
	case "volume":
		return parser.UnitTypeVolume, nil
	case "network":
		return parser.UnitTypeNetwork, nil
	default:
		return parser.UnitTypeUnknown, fmt.Errorf("unknown unit type %q", t)
	}
}

func toStringSlice(v any) ([]string, error) {
	switch val := v.(type) {
	case string:
		return []string{val}, nil
	case []any:
		var out []string
		for _, elem := range val {
			s, ok := elem.(string)
			if !ok {
				return nil, fmt.Errorf("expected string, got %T", elem)
			}
			out = append(out, s)
		}
		return out, nil
	case []string:
		return val, nil
	default:
		return nil, fmt.Errorf("expected string or []string, got %T", v)
	}
}

func sortedKeys(m map[string]map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
