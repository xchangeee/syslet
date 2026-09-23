package render

import (
	"strings"

	gounit "github.com/coreos/go-systemd/v22/unit"

	"github.com/xchangeee/syslet/internal/model"
)

// IsUnitRemovalAllowed returns true when RemovalAllowed=true is present in the [X-Syslet] section.
func IsUnitRemovalAllowed(opts []gounit.UnitOption) bool {
	return strings.EqualFold(FindOptValue(opts, SectionXSyslet, KeyXSysletRemovalAllowed), SystemdTrue)
}

// Skip reasons reported in the plan for a stale unit that IsUnitRemovalAllowed keeps.
const (
	// SkipReasonProtected: syslet wrote the unit with RemovalAllowed=false.
	SkipReasonProtected = "protected (removalAllowed: false)"
	// SkipReasonNotManaged: the unit has no RemovalAllowed marker, so syslet
	// didn't write it (e.g. a hand-placed quadlet).
	SkipReasonNotManaged = "not managed by syslet (no [X-Syslet] marker)"
)

// RemovalSkipReason explains why IsUnitRemovalAllowed refuses a stale unit, for
// the planner's skip result. It tells a unit protected in its spec apart from
// one syslet never wrote; only meaningful when IsUnitRemovalAllowed is false.
func RemovalSkipReason(opts []gounit.UnitOption) string {
	if FindOptValue(opts, SectionXSyslet, KeyXSysletRemovalAllowed) == "" {
		return SkipReasonNotManaged
	}
	return SkipReasonProtected
}

// IsReclaimPolicyDelete returns true when ReclaimPolicy=Delete is present in the [X-Syslet] section.
func IsReclaimPolicyDelete(opts []gounit.UnitOption) bool {
	return strings.EqualFold(FindOptValue(opts, SectionXSyslet, KeyXSysletReclaimPolicy), string(model.ReclaimPolicyDelete))
}

// ImageTagFromUnitOpts extracts the ImageTag value from a deserialized .build unit file.
func ImageTagFromUnitOpts(opts []gounit.UnitOption) model.ImageTag {
	return model.ImageTag(FindOptValue(opts, SectionBuild, KeyBuildImageTag))
}

// IsOneshotUnit reports whether the parsed unit options describe a oneshot service.
func IsOneshotUnit(opts []gounit.UnitOption) bool {
	for _, opt := range opts {
		if MatchSectionKey(opt, SectionService, KeyServiceType) &&
			strings.EqualFold(opt.Value, ServiceTypeOneshot) {
			return true
		}
	}
	return false
}
