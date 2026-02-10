package daemon

import (
	"crypto/sha256"
	"fmt"
	"io"

	"github.com/coreos/go-systemd/v22/unit"

	pb "codeberg.org/xchangeee/syslet/proto"
)

const (
	DefaultContainerConfigDirectory = "/var/syslet/containers/config"
)

func sha256sum(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h[:])
}

// unitContent serializes go-systemd options into INI-style unit file content.
func unitContent(opts []*unit.UnitOption) string {
	data, _ := io.ReadAll(unit.Serialize(opts))
	return string(data)
}

func pbActiveState(s string) pb.ActiveState {
	switch s {
	case "active":
		return pb.ActiveState_ACTIVE_STATE_ACTIVE
	case "inactive":
		return pb.ActiveState_ACTIVE_STATE_INACTIVE
	case "failed":
		return pb.ActiveState_ACTIVE_STATE_FAILED
	case "activating":
		return pb.ActiveState_ACTIVE_STATE_ACTIVATING
	case "deactivating":
		return pb.ActiveState_ACTIVE_STATE_DEACTIVATING
	default:
		return pb.ActiveState_ACTIVE_STATE_UNSPECIFIED
	}
}
