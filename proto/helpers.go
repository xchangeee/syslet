package sysletv1

import (
	"path/filepath"
	"strings"
)

// IsStartable returns true for unit types that can be started/stopped.
func (t UnitType) IsStartable() bool {
	return t == UnitType_UNIT_TYPE_CONTAINER
}

// IsImmutable returns true for unit types that cannot be modified after creation.
func (t UnitType) IsImmutable() bool {
	return t == UnitType_UNIT_TYPE_VOLUME || t == UnitType_UNIT_TYPE_NETWORK
}

// Extension returns the file extension for a unit type (e.g. ".container").
func (t UnitType) Extension() string {
	switch t {
	case UnitType_UNIT_TYPE_CONTAINER:
		return ".container"
	case UnitType_UNIT_TYPE_VOLUME:
		return ".volume"
	case UnitType_UNIT_TYPE_NETWORK:
		return ".network"
	default:
		return ""
	}
}

// ShortName returns a short human-readable name for the unit type (e.g. "container").
func (t UnitType) ShortName() string {
	switch t {
	case UnitType_UNIT_TYPE_CONTAINER:
		return "container"
	case UnitType_UNIT_TYPE_VOLUME:
		return "volume"
	case UnitType_UNIT_TYPE_NETWORK:
		return "network"
	default:
		return "unknown"
	}
}

// UnitTypeFromExtension determines the unit type from a filename extension.
func UnitTypeFromExtension(filename string) UnitType {
	switch filepath.Ext(filename) {
	case ".container":
		return UnitType_UNIT_TYPE_CONTAINER
	case ".volume":
		return UnitType_UNIT_TYPE_VOLUME
	case ".network":
		return UnitType_UNIT_TYPE_NETWORK
	default:
		return UnitType_UNIT_TYPE_UNSPECIFIED
	}
}

// FullUnitName constructs "name.type" from a name and unit type.
// e.g. FullUnitName("webapp", UNIT_TYPE_CONTAINER) → "webapp.container"
func FullUnitName(name string, t UnitType) string {
	return name + t.Extension()
}

// UnitName returns the unit name without the type suffix.
// e.g. "webapp.container" → "webapp"
func UnitName(fullUnitName string) string {
	return strings.TrimSuffix(fullUnitName, filepath.Ext(fullUnitName))
}
