package syslet

import (
	"context"
	"fmt"
	"strings"

	gounit "github.com/coreos/go-systemd/v22/unit"

	"codeberg.org/xchangeee/syslet/internal/filestore"
	"codeberg.org/xchangeee/syslet/internal/model"
	"codeberg.org/xchangeee/syslet/internal/render"
	"codeberg.org/xchangeee/syslet/internal/systemd"
)

func buildPlanUnitContainer(ctx context.Context, sd *systemd.Client, store *filestore.ContainerConfigFileStore, plan *ApplyPlan, r render.RenderedUnit, changedNetworks map[model.NetworkUnitRef]bool, changedBuilds map[model.BuildUnitRef]bool, changedVolumes map[model.VolumeUnitRef]bool, changedSecrets map[string]bool) {
	container, ok := r.Unit.(*model.ContainerUnit)
	if !ok {
		panic("buildPlanUnitContainer called with non-container spec")
	}

	uc, ok := computeUnitChanges(sd, plan, r)
	if !ok {
		return
	}

	changes := containerChanges{unitChanges: uc, desiredState: container.DesiredState}
	changes.applyToPlan(plan)

	if containerReferencesChangedNetwork(r.UnitOptions, changedNetworks) {
		changes.markNetworkChanged()
	}
	if containerReferencesChangedBuild(r.UnitOptions, changedBuilds) {
		changes.markBuildChanged()
	}
	if containerReferencesChangedVolume(r.UnitOptions, changedVolumes) {
		changes.markVolumeChanged()
	}
	if containerReferencesChangedSecret(r.UnitOptions, changedSecrets) {
		changes.markSecretChanged()
	}

	unitRef := container.TypedUnitRef()

	if !buildPlanUnitContainerConfigFiles(store, plan, container, unitRef, &changes) {
		return
	}

	if !buildPlanUnitContainerConfigDirs(store, plan, container, unitRef, &changes) {
		return
	}

	action := actionNone
	if !render.IsOneshotUnit(r.UnitOptions) {
		var isRunning bool
		if !changes.isNew {
			state, err := sd.ContainerState(ctx, unitRef)
			if err != nil {
				plan.RecordError(unitRef.FullName(), fmt.Sprintf("querying state of %s: %v", unitRef.FullName(), err))
				return
			}
			isRunning = state.ActiveState.IsRunning()
		}
		action = changes.planLifecycle(isRunning)
		switch action {
		case actionRestart:
			plan.StopSystemdService(unitRef)
			plan.StartSystemdService(unitRef)
		case actionStop:
			plan.StopSystemdService(unitRef)
		case actionReload:
			plan.ReloadSystemdService(unitRef)
		case actionStart:
			plan.StartSystemdService(unitRef)
		}
	}

	changes.recordResult(plan, action)
}

func buildPlanUnitContainerConfigFiles(store *filestore.ContainerConfigFileStore, plan *ApplyPlan, container *model.ContainerUnit, unitRef model.ContainerUnitRef, changes *containerChanges) bool {
	desiredFilenames := make(map[string]bool, len(container.FileMounts))
	for _, fm := range container.FileMounts {
		internalName := store.InternalFilename(fm.FullPath())
		desiredFilenames[internalName] = true
		changed, oldContent, oldMode, err := store.IsFileChanged(unitRef, fm.FullPath(), fm.File.Content, fm.File.Mode)
		if err != nil {
			plan.RecordError(unitRef.FullName(), fmt.Sprintf("checking config %s: %v", internalName, err))
			return false
		}
		if changed {
			plan.WriteFsContainerConfigFile(unitRef, fm.FullPath(), fm.File.Content, oldContent, fm.File.Mode, oldMode)
			changes.markConfigFileChanged()
		}
	}

	// Determine stale config files
	deployed, stale, err := store.ListStaleFiles(unitRef, desiredFilenames)
	if err != nil {
		plan.RecordError(unitRef.FullName(), fmt.Sprintf("listing stale files: %v", err))
		return false
	}
	for _, f := range stale {
		plan.DeleteFsContainerConfigFile(unitRef, f)
		changes.markConfigFileChanged()
	}
	if len(desiredFilenames) == 0 && len(deployed) > 0 {
		plan.DeleteFsContainerConfig(unitRef)
	}
	return true
}

func buildPlanUnitContainerConfigDirs(store *filestore.ContainerConfigFileStore, plan *ApplyPlan, container *model.ContainerUnit, unitRef model.ContainerUnitRef, changes *containerChanges) bool {
	desiredDirnames := make(map[string]bool, len(container.DirMounts))
	for _, dm := range container.DirMounts {
		internalDirname := store.InternalDirname(dm.Directory)
		desiredDirnames[internalDirname] = true
		currentVersion, err := store.CurrentDirVersion(unitRef, dm.Directory)
		if err != nil {
			plan.RecordError(unitRef.FullName(), fmt.Sprintf("checking configDir %s version: %v", internalDirname, err))
			return false
		}
		dirChanged := false
		desiredFileSet := make(map[string]bool, len(dm.Files))
		for _, f := range dm.Files {
			desiredFileSet[f.Name] = true
			if dirChanged {
				continue
			}
			changed, err := store.IsVersionedFileChanged(unitRef, dm.Directory, currentVersion, f.Name, f.Mode, f.Content)
			if err != nil {
				plan.RecordError(unitRef.FullName(), fmt.Sprintf("checking configDir %s file %s: %v", internalDirname, f.Name, err))
				return false
			}
			if changed {
				dirChanged = true
			}
		}
		if !dirChanged && currentVersion > 0 {
			// Detect file removals: if the deployed version has files not in the desired set, the dir changed.
			deployed, err := store.ListVersionedDirFiles(unitRef, dm.Directory, currentVersion)
			if err != nil {
				plan.RecordError(unitRef.FullName(), fmt.Sprintf("listing configDir %s files: %v", internalDirname, err))
				return false
			}
			for _, name := range deployed {
				if !desiredFileSet[name] {
					dirChanged = true
					break
				}
			}
		}
		if dirChanged {
			oldFiles, err := store.ReadVersionedDirFiles(unitRef, dm.Directory, currentVersion)
			if err != nil {
				plan.RecordError(unitRef.FullName(), fmt.Sprintf("reading configDir %s old files: %v", internalDirname, err))
				return false
			}
			oldModes, err := store.ReadVersionedDirFileModes(unitRef, dm.Directory, currentVersion)
			if err != nil {
				plan.RecordError(unitRef.FullName(), fmt.Sprintf("reading configDir %s old modes: %v", internalDirname, err))
				return false
			}
			plan.WriteFsContainerConfigDir(unitRef, dm.Directory, currentVersion+1, dm.Files, oldFiles, oldModes)
			changes.markConfigDirChanged()
		}
	}

	// determine stale config files inside config dirs
	staleDirs, err := store.ListStaleDirs(unitRef, desiredDirnames)
	if err != nil {
		plan.RecordError(unitRef.FullName(), fmt.Sprintf("listing stale configDir groups: %v", err))
		return false
	}
	for _, internalDirname := range staleDirs {
		plan.DeleteFsContainerConfigDir(unitRef, internalDirname)
	}
	return true
}

type containerChanges struct {
	unitChanges
	desiredState      model.DesiredState
	configFileChanged bool
	configDirChanged  bool
	networkChanged    bool // a referenced network is being recreated this plan pass
	buildChanged      bool // a referenced build image is being rebuilt this plan pass
	volumeChanged     bool // a referenced volume is being recreated this plan pass
	secretChanged     bool // a referenced secret is being upserted this plan pass
}

type containerAction int

const (
	actionNone    containerAction = iota
	actionStart                   // not running, desired: running (includes new containers)
	actionStop                    // running, desired: stopped
	actionRestart                 // unit or config changed while running, desired: running
	actionReload                  // only configDir changed while running; no stop/start needed
)

func (c *containerChanges) markConfigFileChanged() { c.configFileChanged = true }
func (c *containerChanges) markConfigDirChanged()  { c.configDirChanged = true }
func (c *containerChanges) markNetworkChanged()    { c.networkChanged = true }
func (c *containerChanges) markBuildChanged()      { c.buildChanged = true }
func (c *containerChanges) markVolumeChanged()     { c.volumeChanged = true }
func (c *containerChanges) markSecretChanged()     { c.secretChanged = true }

// planLifecycle converts detected changes and current runtime state into a single action.
// Exactly one action is returned; reload and stop/start are mutually exclusive by construction.
func (c containerChanges) planLifecycle(isRunning bool) containerAction {
	needsRestart := (c.meaningfullyChanged || c.configFileChanged || c.networkChanged || c.buildChanged || c.volumeChanged || c.secretChanged) && isRunning
	switch {
	case needsRestart && c.desiredState == model.DesiredStateRunning:
		return actionRestart
	case needsRestart, isRunning && c.desiredState == model.DesiredStateStopped:
		return actionStop
	case c.configDirChanged && isRunning:
		return actionReload
	case !isRunning && c.desiredState == model.DesiredStateRunning:
		return actionStart
	default:
		return actionNone
	}
}

func (c containerChanges) recordResult(plan *ApplyPlan, action containerAction) {
	anyChange := c.isChanged || c.configFileChanged || c.configDirChanged || c.networkChanged || c.buildChanged || c.volumeChanged || c.secretChanged || action != actionNone
	var status OperationStatus
	var parts []string
	if c.isNew {
		parts = append(parts, "created")
		status = StatusCreated
	} else if anyChange {
		status = StatusUpdated
	} else {
		status = StatusUnchanged
	}

	if c.isChanged && !c.isNew {
		parts = append(parts, "unit updated")
	}
	if c.configFileChanged {
		parts = append(parts, "config updated")
	}
	if c.configDirChanged {
		parts = append(parts, "configDir updated")
	}
	if c.networkChanged {
		parts = append(parts, "network recreated")
	}
	if c.buildChanged {
		parts = append(parts, "build recreated")
	}
	if c.volumeChanged {
		parts = append(parts, "volume recreated")
	}
	if c.secretChanged {
		parts = append(parts, "secret updated")
	}
	switch action {
	case actionRestart:
		parts = append(parts, "restarted")
	case actionStart:
		parts = append(parts, "started")
	case actionStop:
		parts = append(parts, "stopped")
	case actionReload:
		parts = append(parts, "reloaded")
	}

	var message string
	if len(parts) == 0 {
		message = fmt.Sprintf("up to date (desired: %s)", string(c.desiredState))
	} else {
		message = fmt.Sprintf("%s (desired: %s)", strings.Join(parts, ", "), string(c.desiredState))
	}
	plan.RecordResult(c.fullUnitName, status, message)
}

// containerReferencesChangedBuild reports whether the Image= entry in opts
// references a build unit that is being rebuilt in this plan pass.
// A container has exactly one image, so FindOptValue (singular) is used.
func containerReferencesChangedBuild(opts []gounit.UnitOption, changedBuilds map[model.BuildUnitRef]bool) bool {
	val := render.FindOptValue(opts, render.SectionContainer, render.KeyContainerImage)
	if before, ok := strings.CutSuffix(val, ".build"); ok {
		return changedBuilds[model.BuildUnitRef(before)]
	}
	return false
}

// containerReferencesChangedVolume reports whether any Volume= entry in opts
// references a volume that is being recreated in this plan pass.
// Volume= format: "<source>:<target>[:<options>]"; the source is before the first colon.
func containerReferencesChangedVolume(opts []gounit.UnitOption, changedVolumes map[model.VolumeUnitRef]bool) bool {
	for _, val := range render.FindOptValues(opts, render.SectionContainer, render.KeyContainerVolume) {
		source, _, _ := strings.Cut(val, ":")
		if before, ok := strings.CutSuffix(source, ".volume"); ok {
			if changedVolumes[model.VolumeUnitRef(before)] {
				return true
			}
		}
	}
	return false
}

// containerReferencesChangedNetwork reports whether any Network= entry in opts
// references a network that is being recreated in this plan pass.
func containerReferencesChangedNetwork(opts []gounit.UnitOption, changedNetworks map[model.NetworkUnitRef]bool) bool {
	for _, val := range render.FindOptValues(opts, render.SectionContainer, render.KeyContainerNetwork) {
		if before, ok := strings.CutSuffix(val, ".network"); ok {
			if changedNetworks[model.NetworkUnitRef(before)] {
				return true
			}
		}
	}
	return false
}

// containerReferencesChangedSecret reports whether any Secret= entry in opts
// references a podman secret that is being upserted in this plan pass.
// Secret= format: "<specname>-<key>[,opt=val...]" — the secret name is before the first comma.
func containerReferencesChangedSecret(opts []gounit.UnitOption, changedSecrets map[string]bool) bool {
	for _, val := range render.FindOptValues(opts, render.SectionContainer, render.KeyContainerSecret) {
		secretName, _, _ := strings.Cut(val, ",")
		if changedSecrets[secretName] {
			return true
		}
	}
	return false
}

func buildPlanUnitStaleContainer(ctx context.Context, sd *systemd.Client, plan *ApplyPlan, containerName model.ContainerUnitRef, options []gounit.UnitOption) {
	fullName := containerName.FullName()
	if render.IsUnitRemovalAllowed(options) {
		plan.RecordResult(fullName, StatusRemoved, "removed")
		plan.DeleteFsQuadletUnitFile(fullName)
		plan.DeleteFsContainerConfig(containerName)
		if !render.IsOneshotUnit(options) {
			state, err := sd.ContainerState(ctx, containerName)
			if err == nil && state.ActiveState.IsRunning() {
				plan.StopSystemdService(containerName)
			}
		}
	} else {
		plan.RecordResult(fullName, StatusSkipped, "not marked for removal (removalAllowed not set)")
	}
}
