package render

import (
	"testing"

	"codeberg.org/xchangeee/syslet/internal/model"
)

func TestNetworkSpec_Render_BasicNetwork(t *testing.T) {
	spec := model.NewNetworkUnit(model.NetworkUnitRef("frontend"), makeUnitOptions(SectionNetwork, "Driver", "bridge"), false, model.ReclaimPolicyRetain)
	ru, err := RenderNetwork(spec)
	rendered := mustRender(t, ru, err)
	assertHasOption(t, rendered.UnitOptions, SectionNetwork, KeyNetworkName, "frontend")
}

func TestNetworkSpec_Render_NetworkNameIsEnforced(t *testing.T) {
	spec := model.NewNetworkUnit(model.NetworkUnitRef("frontend"), makeUnitOptions(SectionNetwork, "NetworkName", "custom-network"), false, model.ReclaimPolicyRetain)
	ru, err := RenderNetwork(spec)
	rendered := mustRender(t, ru, err)
	assertUniqueOption(t, rendered.UnitOptions, SectionNetwork, KeyNetworkName, "frontend")
}

func TestNetworkSpec_ReclaimableMixin(t *testing.T) {
	testReclaimableMixin(t,
		func() (RenderedUnit, error) {
			return RenderNetwork(model.NewNetworkUnit(
				model.NetworkUnitRef("frontend"),
				makeUnitOptions(SectionNetwork, "Driver", "bridge"),
				false,
				model.ReclaimPolicyDelete,
			))
		},
		func() (RenderedUnit, error) {
			return RenderNetwork(model.NewNetworkUnit(
				model.NetworkUnitRef("frontend"),
				makeUnitOptions(SectionXSyslet, "ReclaimPolicy", "Retain"),
				false,
				model.ReclaimPolicyDelete,
			))
		},
		func() (RenderedUnit, error) {
			return RenderNetwork(model.NewNetworkUnit(
				model.NetworkUnitRef("frontend"),
				makeUnitOptions(SectionNetwork, "Driver", "bridge"),
				false,
				model.ReclaimPolicyRetain,
			))
		},
	)
}

func TestNetworkSpec_PrunableMixin(t *testing.T) {
	testPrunableMixin(t,
		func() (RenderedUnit, error) {
			return RenderNetwork(model.NewNetworkUnit(
				model.NetworkUnitRef("frontend"),
				makeUnitOptions(SectionNetwork, "Driver", "bridge"),
				true,
				model.ReclaimPolicyRetain,
			))
		},
		func() (RenderedUnit, error) {
			return RenderNetwork(model.NewNetworkUnit(
				model.NetworkUnitRef("frontend"),
				makeUnitOptions(SectionXSyslet, "RemovalAllowed", "false"),
				true,
				model.ReclaimPolicyRetain,
			))
		},
		func() (RenderedUnit, error) {
			return RenderNetwork(model.NewNetworkUnit(
				model.NetworkUnitRef("frontend"),
				makeUnitOptions(SectionNetwork, "Driver", "bridge"),
				false,
				model.ReclaimPolicyRetain,
			))
		},
	)
}
