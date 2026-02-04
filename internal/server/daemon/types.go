package daemon

import (
	"crypto/sha256"
	"fmt"
	"io"

	pb "codeberg.org/xchangeee/syslet/proto"
	"github.com/coreos/go-systemd/v22/unit"
)

// UnitChange captures the diff for a single unit and, when changes are
// detected, carries the rendered systemd options and config files needed
// to apply those changes.
type UnitChange struct {
	Spec    *pb.UnitSpec
	Options []*unit.UnitOption // rendered systemd unit options
	Configs []ConfigFile       // config files to deploy

	UnitChanged   bool
	ConfigChanged bool
	NeedsStop     bool // stop before update (unit or config changed)
	NeedsStart    bool // start after update
	IsNew         bool // unit doesn't exist yet
	Rejected      bool // immutable unit changed
	RejectReason  string
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

func sha256sum(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h[:])
}
