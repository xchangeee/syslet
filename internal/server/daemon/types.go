package daemon

import (
	"crypto/sha256"
	"fmt"
	"io"

	pb "codeberg.org/xchangeee/syslet/proto"
	"github.com/coreos/go-systemd/v22/unit"
)

// ChangePlan is the result of diffing all units.
type ChangePlan struct {
	Changes    []*UnitChange
	NeedReload bool // at least one unit file changed
}

// UnitChange captures the diff for a single unit and, when changes are
// detected, carries the rendered systemd options and config files needed
// to apply those changes.
type UnitChange struct {
	// The spec
	Spec *pb.UnitSpec

	// rendered systemd unit options
	Options []*unit.UnitOption

	// config files to deploy
	Configs []ConfigFile

	// unit doesn't exist yet
	IsNew bool
	// immutable unit changed
	Rejected     bool
	RejectReason string
	//
	UnitChanged bool
	//
	ConfigChanged bool

	// stop before update (unit or config changed)
	NeedsStop bool
	// start after update
	NeedsStart bool
}

// ConfigFile represents a config file to deploy.
type ConfigFile struct {
	// e.g. "myapp" (without extension)
	UnitName string
	// e.g. "config.yaml"
	Filename string
	Content  string
}

// FullName returns the full unit name (e.g. "webapp.container").
func (c *UnitChange) FullName() string {
	return pb.FullUnitName(c.Spec.Name, c.Spec.Type)
}

// SystemdContent returns the unit file content serialized as INI,
// suitable for installing to systemd.
func (c *UnitChange) SystemdContent() io.Reader {
	return unit.Serialize(c.Options)
}

// UnitResult is the outcome of reconciling one unit.
type UnitResult struct {
	FullName string
	Type     pb.UnitType
	Changed  bool
	Message  string
	Error    bool
}

func sha256sum(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h[:])
}
