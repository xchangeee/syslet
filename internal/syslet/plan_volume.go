package syslet

import (
	gounit "github.com/coreos/go-systemd/v22/unit"

	"github.com/xchangeee/syslet/internal/model"
	"github.com/xchangeee/syslet/internal/render"
	"github.com/xchangeee/syslet/internal/systemd"
)

// buildPlanUnitVolume diffs an existing volume unit against the desired spec.
// When the change is meaningful and the new spec opts in to destructive recreation
// (removalAllowed=true AND reclaimPolicy=delete), the volume is torn down and
// recreated. Without opt-in, a meaningful change is a hard error — silently writing
// a unit file that diverges from the actual running volume would misrepresent system
// state. The operator must either set both permissions or revert the change.
func buildPlanUnitVolume(sd *systemd.Client, planner *planner, r render.RenderedUnit, changedVolumes map[model.VolumeUnitRef]bool) {
	uc, ok := computeUnitChanges(sd, planner, r)
	if !ok {
		return
	}
	volumeRef := r.Unit.(*model.VolumeUnit).TypedUnitRef()
	if !uc.isNew && uc.meaningfullyChanged {
		if render.IsUnitRemovalAllowed(uc.existingOptions) && render.IsReclaimPolicyDelete(uc.existingOptions) {
			planner.StopSystemdService(volumeRef)
			planner.DeletePodmanVolume(volumeRef)
			changedVolumes[volumeRef] = true
		} else {
			planner.recordUnitError(uc.fullUnitName, "volume has meaningful changes but recreation is not permitted: set removalAllowed=true and reclaimPolicy=delete to allow")
			return
		}
	}
	uc.applyToPlan(planner)
	uc.recordOutcome(planner)
}

func buildPlanUnitStaleVolume(planner *planner, volume model.VolumeUnitRef, options []gounit.UnitOption) {
	buildPlanUnitStaleResource(planner, volume.FullName(), options, func() { planner.DeletePodmanVolume(volume) })
}
