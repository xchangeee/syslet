package daemon

import (
	"io"
	"time"

	pb "codeberg.org/xchangeee/syslet/proto"
	"github.com/coreos/go-systemd/v22/unit"
)

// ResolvedUnit holds a spec resolved into go-systemd options and config files,
// ready for reconciliation.
type ResolvedUnit struct {
	Spec    *pb.UnitSpec
	Options []*unit.UnitOption
	Configs []ConfigFile
}

// FullName returns the full unit name (e.g. "webapp.container").
func (u *ResolvedUnit) FullName() string {
	return pb.FullUnitName(u.Spec.Name, u.Spec.Type)
}

// SystemdContent returns the unit file content serialized as INI,
// suitable for installing to systemd.
func (u *ResolvedUnit) SystemdContent() io.Reader {
	return unit.Serialize(u.Options)
}

// ManagedUnitState represents the observed state of a managed unit.
type ManagedUnitState struct {
	Name           string
	Type           pb.UnitType
	DesiredState   pb.DesiredState
	ActiveState    pb.ActiveState
	Enabled        bool
	ConfigFiles    []string
	LastReconciled time.Time
	Error          string
}

// UnitAction describes what needs to happen to a single unit.
type UnitAction int

const (
	ActionNone        UnitAction = iota
	ActionStop                   // stop before updating
	ActionWriteConfig            // write config files
	ActionWriteUnit              // write unit file
	ActionStart                  // start the unit
	ActionReject                 // immutable unit changed, reject
)

// UnitChange captures the diff for a single unit.
type UnitChange struct {
	ResolvedUnit
	UnitChanged   bool
	ConfigChanged bool
	NeedsStop     bool // stop before update (unit or config changed)
	NeedsStart    bool // start after update
	IsNew         bool // unit doesn't exist yet
	Rejected      bool // immutable unit changed
	RejectReason  string
}

// ChangePlan is the result of diffing all units.
type ChangePlan struct {
	Changes    []*UnitChange
	NeedReload bool // at least one unit file changed
}

// UnitResult is the outcome of reconciling one unit.
type UnitResult struct {
	FullName string
	Type     pb.UnitType
	Changed  bool
	Message  string
	Error    bool
}

// ConfigFile represents a config file to deploy.
type ConfigFile struct {
	UnitName string // e.g. "myapp" (without extension)
	Filename string // e.g. "config.yaml"
	Content  string
}
