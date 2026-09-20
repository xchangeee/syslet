package render

import (
	"testing"

	"codeberg.org/xchangeee/syslet/internal/model"
)

func TestVolumeSpec_Render_BasicVolume(t *testing.T) {
	spec := model.NewVolumeUnit(model.VolumeUnitRef("data"), makeUnitOptions(SectionVolume, "Device", "tmpfs"), false, model.ReclaimPolicyRetain)
	ru, err := NewRenderedUnitFromVolume(spec)
	rendered := mustRender(t, ru, err)
	assertHasOption(t, rendered.UnitOptions, SectionVolume, KeyVolumeName, "data")
}

func TestVolumeSpec_Render_VolumeNameIsEnforced(t *testing.T) {
	spec := model.NewVolumeUnit(model.VolumeUnitRef("data"), makeUnitOptions(SectionVolume, "VolumeName", "custom-volume"), false, model.ReclaimPolicyRetain)
	ru, err := NewRenderedUnitFromVolume(spec)
	rendered := mustRender(t, ru, err)
	assertUniqueOption(t, rendered.UnitOptions, SectionVolume, KeyVolumeName, "data")
}

func TestVolumeSpec_ReclaimableMixin(t *testing.T) {
	testReclaimableMixin(t,
		func() (RenderedUnit, error) {
			return NewRenderedUnitFromVolume(model.NewVolumeUnit(
				model.VolumeUnitRef("data"),
				makeUnitOptions(SectionVolume, "Device", "tmpfs"),
				false,
				model.ReclaimPolicyDelete,
			))
		},
		func() (RenderedUnit, error) {
			return NewRenderedUnitFromVolume(model.NewVolumeUnit(
				model.VolumeUnitRef("data"),
				makeUnitOptions(SectionXSyslet, "ReclaimPolicy", "Retain"),
				false,
				model.ReclaimPolicyDelete,
			))
		},
		func() (RenderedUnit, error) {
			return NewRenderedUnitFromVolume(model.NewVolumeUnit(
				model.VolumeUnitRef("data"),
				makeUnitOptions(SectionVolume, "Device", "tmpfs"),
				false,
				model.ReclaimPolicyRetain,
			))
		},
	)
}

func TestVolumeSpec_PrunableMixin(t *testing.T) {
	testPrunableMixin(t,
		func() (RenderedUnit, error) {
			return NewRenderedUnitFromVolume(model.NewVolumeUnit(
				model.VolumeUnitRef("data"),
				makeUnitOptions(SectionVolume, "Device", "tmpfs"),
				true,
				model.ReclaimPolicyRetain,
			))
		},
		func() (RenderedUnit, error) {
			return NewRenderedUnitFromVolume(model.NewVolumeUnit(
				model.VolumeUnitRef("data"),
				makeUnitOptions(SectionXSyslet, "RemovalAllowed", "false"),
				true,
				model.ReclaimPolicyRetain,
			))
		},
		func() (RenderedUnit, error) {
			return NewRenderedUnitFromVolume(model.NewVolumeUnit(
				model.VolumeUnitRef("data"),
				makeUnitOptions(SectionVolume, "Device", "tmpfs"),
				false,
				model.ReclaimPolicyRetain,
			))
		},
	)
}
