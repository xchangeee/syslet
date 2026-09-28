package syslet

import (
	gounit "github.com/coreos/go-systemd/v22/unit"

	"github.com/xchangeee/syslet/internal/model"
	"github.com/xchangeee/syslet/internal/render"
	"github.com/xchangeee/syslet/internal/systemd"
)

// buildPlanUnitNetwork diffs an existing network unit against the desired spec.
// When the change is meaningful (outside [X-Syslet] / [Unit].Description), the network
// must be torn down and recreated because podman networks are immutable after creation.
// It records the network in changedNetworks so buildPlanUnitContainer can arrange
// container restarts on behalf of the affected containers.
func buildPlanUnitNetwork(sd *systemd.Client, planner *planner, r render.RenderedUnit, changedNetworks map[model.NetworkUnitRef]bool) {
	uc, ok := computeUnitChanges(sd, planner, r)
	if !ok {
		return
	}
	networkRef := r.Unit.(*model.NetworkUnit).TypedUnitRef()
	if !uc.isNew && uc.meaningfullyChanged {
		planner.StopSystemdService(networkRef)
		planner.RecreatePodmanNetwork(networkRef)
		changedNetworks[networkRef] = true
	}
	uc.applyToPlan(planner)
	uc.recordOutcome(planner)
}

func buildPlanUnitStaleNetwork(planner *planner, network model.NetworkUnitRef, options []gounit.UnitOption) {
	buildPlanUnitStaleResource(planner, network.FullName(), options, func() { planner.DeletePodmanNetwork(network) })
}
