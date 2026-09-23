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
