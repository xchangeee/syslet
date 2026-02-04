package sysletv1

import (
	"path/filepath"
	"strings"
)

// UnitName returns the unit name without the extension suffix.
// e.g. "webapp.container" -> "webapp"
func UnitName(fullUnitName string) string {
	return strings.TrimSuffix(fullUnitName, filepath.Ext(fullUnitName))
}
