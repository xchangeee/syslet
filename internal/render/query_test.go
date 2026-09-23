package render

import (
	"testing"

	gounit "github.com/coreos/go-systemd/v22/unit"

	"github.com/xchangeee/syslet/internal/model"
)

// --- IsReclaimPolicyDelete pure function tests ---

func TestIsReclaimPolicyDelete_WithDelete(t *testing.T) {
	opts := []gounit.UnitOption{
		NewUnitOption(SectionXSyslet, KeyXSysletReclaimPolicy, string(model.ReclaimPolicyDelete)),
	}
	if !IsReclaimPolicyDelete(opts) {
		t.Error("expected IsReclaimPolicyDelete to return true for Delete policy")
	}
}

func TestIsReclaimPolicyDelete_WithRetain(t *testing.T) {
	opts := []gounit.UnitOption{
		NewUnitOption(SectionXSyslet, KeyXSysletReclaimPolicy, string(model.ReclaimPolicyRetain)),
	}
	if IsReclaimPolicyDelete(opts) {
		t.Error("expected IsReclaimPolicyDelete to return false for Retain policy")
	}
}

func TestIsReclaimPolicyDelete_NoPolicy(t *testing.T) {
	opts := []gounit.UnitOption{
		NewUnitOption(SectionBuild, KeyBuildFile, "/some/path"),
	}
	if IsReclaimPolicyDelete(opts) {
		t.Error("expected IsReclaimPolicyDelete to return false when no policy is set")
	}
}

func TestIsReclaimPolicyDelete_Nil(t *testing.T) {
	if IsReclaimPolicyDelete(nil) {
		t.Error("expected IsReclaimPolicyDelete to return false for nil input")
	}
}

// --- IsUnitRemovalAllowed pure function tests ---

func TestIsUnitRemovalAllowed_WithMarker(t *testing.T) {
	opts := []gounit.UnitOption{
		NewUnitOption(SectionXSyslet, KeyXSysletRemovalAllowed, SystemdTrue),
	}
	if !IsUnitRemovalAllowed(opts) {
		t.Error("expected IsUnitRemovalAllowed to return true when marker is present")
	}
}

func TestIsUnitRemovalAllowed_WithoutMarker(t *testing.T) {
	opts := []gounit.UnitOption{
		NewUnitOption(SectionContainer, KeyContainerName, "webapp"),
	}
	if IsUnitRemovalAllowed(opts) {
		t.Error("expected IsUnitRemovalAllowed to return false when marker is not present")
	}
}

func TestIsUnitRemovalAllowed_Nil(t *testing.T) {
	if IsUnitRemovalAllowed(nil) {
		t.Error("expected IsUnitRemovalAllowed to return false for nil input")
	}
}

func TestIsUnitRemovalAllowed_CaseInsensitive(t *testing.T) {
	opts := []gounit.UnitOption{
		NewUnitOption(SectionXSyslet, KeyXSysletRemovalAllowed, "TRUE"),
	}
	if !IsUnitRemovalAllowed(opts) {
		t.Error("expected IsUnitRemovalAllowed to be case-insensitive")
	}
}

// --- Mixin test helpers ---

// testReclaimableMixin verifies ReclaimableMixin rendering behavior for any unit type.
//
//   - withDelete: renders a unit with ReclaimPolicy=Delete in the struct field
//   - withOverride: renders a unit with ReclaimPolicy=Delete in the struct field but "Retain" in the unit map
//   - withRetain: renders a unit with ReclaimPolicy=Retain (the default)
func testReclaimableMixin(t *testing.T, withDelete, withOverride, withRetain func() (RenderedUnit, error)) {
	t.Helper()

	t.Run("WithDeleteReclaimPolicy", func(t *testing.T) {
		opts := unitOptions(t, withDelete)
		assertHasOption(t, opts, SectionXSyslet, KeyXSysletReclaimPolicy, string(model.ReclaimPolicyDelete))
	})

	t.Run("ReclaimPolicyOverridesUnitMap", func(t *testing.T) {
		opts := unitOptions(t, withOverride)
		assertUniqueOption(t, opts, SectionXSyslet, KeyXSysletReclaimPolicy, string(model.ReclaimPolicyDelete))
	})

	t.Run("WithRetainReclaimPolicy", func(t *testing.T) {
		opts := unitOptions(t, withRetain)
		assertHasOption(t, opts, SectionXSyslet, KeyXSysletReclaimPolicy, string(model.ReclaimPolicyRetain))
	})
}

// testPrunableMixin verifies PrunableMixin rendering behavior for any unit type.
//
//   - withAllowed: renders a unit with RemovalAllowed=true in the struct field
//   - withOverride: renders a unit with RemovalAllowed=true in the struct field but "false" in the unit map
//   - withoutAllowed: renders a unit with RemovalAllowed=false
func testPrunableMixin(t *testing.T, withAllowed, withOverride, withoutAllowed func() (RenderedUnit, error)) {
	t.Helper()

	t.Run("WithRemovalAllowed", func(t *testing.T) {
		assertHasOption(t, unitOptions(t, withAllowed), SectionXSyslet, KeyXSysletRemovalAllowed, SystemdTrue)
	})

	t.Run("RemovalAllowedOverridesUnitMap", func(t *testing.T) {
		assertUniqueOption(t, unitOptions(t, withOverride), SectionXSyslet, KeyXSysletRemovalAllowed, SystemdTrue)
	})

	t.Run("WithoutRemovalAllowed", func(t *testing.T) {
		assertHasOption(t, unitOptions(t, withoutAllowed), SectionXSyslet, KeyXSysletRemovalAllowed, SystemdFalse)
	})
}
