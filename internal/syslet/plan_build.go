package syslet

import (
	"fmt"

	gounit "github.com/coreos/go-systemd/v22/unit"

	"github.com/xchangeee/syslet/internal/filestore"
	"github.com/xchangeee/syslet/internal/model"
	"github.com/xchangeee/syslet/internal/render"
	"github.com/xchangeee/syslet/internal/systemd"
)

type buildChanges struct {
	unitChanges
	contextFileChanged bool
}

func (c *buildChanges) markContextFileChanged() {
	c.contextFileChanged = true
}

func (c buildChanges) recordOutcome(planner *planner) {
	var status UnitStatus
	var message string
	if c.isNew {
		status, message = StatusCreated, "created"
	} else if c.isChanged || c.contextFileChanged {
		status = StatusUpdated
		if c.isChanged && c.contextFileChanged {
			message = "unit and build context updated"
		} else if c.isChanged {
			message = "unit updated"
		} else {
			message = "build context updated"
		}
	} else {
		status, message = StatusUnchanged, "up to date"
	}
	planner.RecordOutcome(c.fullUnitName, status, message)
}

func buildPlanUnitBuild(sd *systemd.Client, buildMgr *filestore.BuildContextFileStore, planner *planner, r render.RenderedUnit, changedBuilds map[model.BuildUnitRef]bool) {
	build, ok := r.Unit.(*model.BuildUnit)
	if !ok {
		panic("buildPlanUnitBuild called with non-build spec")
	}

	uc, ok := computeUnitChanges(sd, planner, r)
	if !ok {
		return
	}

	changes := buildChanges{unitChanges: uc}
	changes.applyToPlan(planner)

	unitRef := build.TypedUnitRef()

	if !buildPlanUnitBuildContextFiles(buildMgr, planner, build, unitRef, &changes) {
		return
	}

	if !uc.isNew && (uc.meaningfullyChanged || changes.contextFileChanged) {
		// Recreation path: stop the build service so it re-runs with the updated
		// spec on next start. No image deletion needed — podman build overwrites
		// the existing image in place.
		// Containers referencing this build will be restarted by buildPlanUnitContainer.
		planner.StopSystemdService(unitRef)
		changedBuilds[unitRef] = true
	}

	changes.recordOutcome(planner)
}

func buildPlanUnitBuildContextFiles(buildMgr *filestore.BuildContextFileStore, planner *planner, buildUnit *model.BuildUnit, unitRef model.BuildUnitRef, changes *buildChanges) bool {
	// Treat Containerfile + context files uniformly as a flat list of (filename, mode, content).
	allFiles := make([]model.BuildContextFile, 0, 1+len(buildUnit.ContextFiles))
	allFiles = append(allFiles, model.BuildContextFile{Filename: "Containerfile", Mode: 0644, Content: buildUnit.Containerfile})
	allFiles = append(allFiles, buildUnit.ContextFiles...)

	desiredFilenames := make(map[string]bool, len(allFiles))
	for _, bf := range allFiles {
		name := string(bf.Filename)
		desiredFilenames[name] = true
		changed, oldContent, oldMode, err := buildMgr.IsFileChanged(buildUnit.Ref().Name(), name, bf.Content, bf.Mode)
		if err != nil {
			planner.recordUnitError(changes.fullUnitName, fmt.Sprintf("checking build file %s: %v", name, err))
			return false
		}
		if changed {
			planner.WriteFsBuildContextFile(unitRef, name, bf.Filename, bf.Content, oldContent, bf.Mode, oldMode)
			changes.markContextFileChanged()
		}
	}

	_, stale, err := buildMgr.ListStaleFiles(buildUnit.Ref().Name(), desiredFilenames)
	if err != nil {
		planner.recordUnitError(changes.fullUnitName, fmt.Sprintf("listing stale build files: %v", err))
		return false
	}
	for _, f := range stale {
		planner.DeleteFsBuildContextFile(unitRef, f)
		changes.markContextFileChanged()
	}
	return true
}

func buildPlanUnitStaleBuild(planner *planner, build model.BuildUnitRef, options []gounit.UnitOption) {
	fullName := build.FullName()
	planner.RecordOutcome(fullName, StatusRemoved, "removed")
	planner.DeleteFsQuadletUnitFile(fullName)
	planner.DeleteFsBuildContext(build)
	if render.IsReclaimPolicyDelete(options) {
		if imageTag := render.ImageTagFromUnitOpts(options); imageTag != "" {
			planner.DeletePodmanImage(imageTag)
		}
	}
}
