// Package parser extracts syslet metadata from container unit files.
// It uses github.com/coreos/go-systemd/v22/unit for actual unit file parsing.
package parser

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/coreos/go-systemd/v22/unit"
)

// UnitType represents the type of a managed unit.
type UnitType int

const (
	UnitTypeUnknown   UnitType = iota
	UnitTypeContainer          // .container (quadlet)
	UnitTypeVolume             // .volume (quadlet)
	UnitTypeNetwork            // .network (quadlet)
)

// DesiredState represents the desired runtime state of a unit.
type DesiredState int

const (
	DesiredStateUnspecified DesiredState = iota
	DesiredStateRunning
	DesiredStateStopped
)

const sysletSection = "X-Syslet"

// ParsedUnit represents a parsed unit file with its syslet metadata.
type ParsedUnit struct {
	Name         string
	Type         UnitType
	DesiredState DesiredState
	RawContent   string             // original file content
	Options      []*unit.UnitOption // parsed options from go-systemd
}

// UnitTypeFromExtension determines the unit type from a filename.
func UnitTypeFromExtension(filename string) UnitType {
	switch filepath.Ext(filename) {
	case ".container":
		return UnitTypeContainer
	case ".volume":
		return UnitTypeVolume
	case ".network":
		return UnitTypeNetwork
	default:
		return UnitTypeUnknown
	}
}

// UnitBaseName returns the filename without extension.
func UnitBaseName(filename string) string {
	return strings.TrimSuffix(filename, filepath.Ext(filename))
}

func (t UnitType) IsStartable() bool {
	return t == UnitTypeContainer
}

func (t UnitType) IsImmutable() bool {
	return t == UnitTypeVolume || t == UnitTypeNetwork
}

func (t UnitType) String() string {
	switch t {
	case UnitTypeContainer:
		return "container"
	case UnitTypeVolume:
		return "volume"
	case UnitTypeNetwork:
		return "network"
	default:
		return "unknown"
	}
}

// Parse parses a unit file and extracts syslet metadata.
func Parse(filename, content string) (*ParsedUnit, error) {
	unitType := UnitTypeFromExtension(filename)
	if unitType == UnitTypeUnknown {
		return nil, fmt.Errorf("unsupported unit type for file %q", filename)
	}

	opts, err := unit.DeserializeOptions(strings.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("parsing %q: %w", filename, err)
	}

	u := &ParsedUnit{
		Name:       filename,
		Type:       unitType,
		RawContent: content,
		Options:    opts,
	}

	for _, opt := range opts {
		if opt.Section != sysletSection {
			continue
		}
		switch opt.Name {
		case "DesiredState":
			switch strings.ToLower(opt.Value) {
			case "running":
				u.DesiredState = DesiredStateRunning
			case "stopped":
				u.DesiredState = DesiredStateStopped
			default:
				return nil, fmt.Errorf("invalid DesiredState %q in %q", opt.Value, filename)
			}
		default:
			return nil, fmt.Errorf("unknown key %q in [X-Syslet] in %q", opt.Name, filename)
		}
	}

	return u, nil
}

// SystemdContent returns the unit file content without the [X-Syslet] section,
// suitable for installing to systemd.
func (u *ParsedUnit) SystemdContent() io.Reader {
	var filtered []*unit.UnitOption
	for _, opt := range u.Options {
		if opt.Section == sysletSection {
			continue
		}
		filtered = append(filtered, opt)
	}
	return unit.Serialize(filtered)
}
