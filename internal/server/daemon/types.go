package daemon

import (
	"time"

	"codeberg.org/xchangeee/syslet/internal/server/parser"
)

// UnitStatus represents the status of a managed unit.
type UnitStatus struct {
	Name           string
	Type           parser.UnitType
	DesiredState   string // "running", "stopped", ""
	ActiveState    string // "active", "inactive", "failed", etc.
	Enabled        bool
	ConfigFiles    []string
	LastReconciled time.Time
	Error          string
}
