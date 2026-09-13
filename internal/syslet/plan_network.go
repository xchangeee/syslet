package syslet

import (
	gounit "github.com/coreos/go-systemd/v22/unit"

	"codeberg.org/xchangeee/syslet/internal/model"
	"codeberg.org/xchangeee/syslet/internal/render"
	"codeberg.org/xchangeee/syslet/internal/systemd"
)

// buildPlanUnitNetwork diffs an existing network unit against the desired spec.
// When the change is meaningful (outside [X-Syslet] / [Unit].Description), the network
// must be torn down and recreated because podman networks are immutable after creation.
// It records the network in changedNetworks so buildPlanUnitContainer can arrange
// container restarts on behalf of the affected containers.
func buildPlanUnitNetwork(sd *systemd.Client, plan *ApplyPlan, r render.RenderedUnit, changedNetworks map[model.NetworkUnitRef]bool) {
	uc, ok := computeUnitChanges(sd, plan, r)
	if !ok {
		return
	}
	networkRef := r.Unit.(*model.NetworkUnit).TypedUnitRef()
	if !uc.isNew && uc.meaningfullyChanged {
		plan.StopSystemdService(networkRef)
		plan.DeletePodmanNetwork(networkRef)
		changedNetworks[networkRef] = true
	}
	uc.applyToPlan(plan)
	uc.recordResult(plan)
}

func buildPlanUnitStaleNetwork(plan *ApplyPlan, network model.NetworkUnitRef, options []gounit.UnitOption) {
	buildPlanUnitStaleResource(plan, network.FullName(), options, func() { plan.DeletePodmanNetwork(network) })
}
