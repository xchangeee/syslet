// Package syslet orchestrates the full lifecycle of quadlet units: computing
// a change plan from the desired spec and the current on-disk state, then
// applying that plan (writing/removing unit files, updating symlinks, reloading
// systemd, and pruning stale container resources via Podman).
package syslet

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"

	gounit "github.com/coreos/go-systemd/v22/unit"
	"github.com/spf13/afero"

	"codeberg.org/xchangeee/syslet/internal/api"
	"codeberg.org/xchangeee/syslet/internal/filestore"
	"codeberg.org/xchangeee/syslet/internal/loader"
	"codeberg.org/xchangeee/syslet/internal/model"
	"codeberg.org/xchangeee/syslet/internal/podman"
	"codeberg.org/xchangeee/syslet/internal/render"
	"codeberg.org/xchangeee/syslet/internal/sops"
	"codeberg.org/xchangeee/syslet/internal/systemd"
	"codeberg.org/xchangeee/syslet/internal/util"
	"codeberg.org/xchangeee/syslet/internal/validate"
)

// OperationStatus is the outcome of a single unit operation.
type OperationStatus string

const (
	StatusCreated   OperationStatus = "created"
	StatusUpdated   OperationStatus = "updated"
	StatusUnchanged OperationStatus = "unchanged"
	StatusRemoved   OperationStatus = "removed"
	StatusSkipped   OperationStatus = "skipped"
	StatusError     OperationStatus = "error"
)

// ApplyPlan contains all operations to execute, organized by phase.
type ApplyPlan struct {
	StopSystemdServices          []StopSystemdServiceOp
	DeleteFsQuadletUnitFiles     []DeleteFsQuadletUnitFileOp
	DeleteFsBuildContextFiles    []DeleteFsBuildContextFileOp
	DeleteFsBuildContexts        []DeleteFsBuildContextOp
	DeleteFsContainerConfigFiles []DeleteFsContainerConfigFileOp
	DeleteFsContainerConfigDirs  []DeleteFsContainerConfigDirsOp
	DeleteFsContainerConfigs     []DeleteFsContainerConfigOp
	DeletePodmanSecrets          []DeletePodmanSecretOp
	DeletePodmanVolumes          []DeletePodmanVolumeOp
	DeletePodmanNetworks         []DeletePodmanNetworkOp
	DeletePodmanImages           []DeletePodmanImageOp
	WriteFsQuadletUnitFiles      []WriteFsQuadletUnitFileOp
	WriteFsBuildContextFiles     []WriteFsBuildContextFileOp
	WriteFsContainerConfigFiles  []WriteFsContainerConfigFileOp
	WriteFsContainerConfigDirs   []WriteFsContainerConfigDirOp
	UpsertPodmanSecrets          []UpsertPodmanSecretOp
	ReloadSystemdServices        []ReloadSystemdServiceOp
	StartSystemdServices         []StartSystemdServiceOp

	Errors  []string
	Results []ApplyResult
}

type StopSystemdServiceOp struct {
	ref model.UnitRef
}

// Ref exposes the unit this op stops. Op fields stay unexported so only the
// plan builders can construct ops; this accessor lets out-of-package callers
// (notably the integration tests in test/integration) inspect a built plan.
func (op StopSystemdServiceOp) Ref() model.UnitRef { return op.ref }

type DeleteFsQuadletUnitFileOp struct {
	fullUnitName model.FullUnitName
}

type DeleteFsBuildContextOp struct {
	build model.BuildUnitRef
}

type DeleteFsBuildContextFileOp struct {
	build    model.BuildUnitRef
	filename string
}

type DeleteFsContainerConfigOp struct {
	container model.ContainerUnitRef
}

// DeleteFsContainerConfigDirsOp removes a stale configDir host subdirectory.
// Named to avoid collision with DeleteConfigDirOp (whole-unit removal).
type DeleteFsContainerConfigDirsOp struct {
	container     model.ContainerUnitRef
	mountPathHash string
}

type DeleteFsContainerConfigFileOp struct {
	container        model.ContainerUnitRef
	internalFilename string
}

// DeletePodmanSecretOp removes a podman secret that is no longer in the desired spec.
// Name is the full podman secret name (<specname>-<key>).
type DeletePodmanSecretOp struct {
	SpecName string
	Name     string
}

type DeletePodmanVolumeOp struct {
	volume model.VolumeUnitRef
}

type DeletePodmanNetworkOp struct {
	network model.NetworkUnitRef
}

type DeletePodmanImageOp struct {
	tag model.ImageTag
}

type WriteFsQuadletUnitFileOp struct {
	fullUnitName model.FullUnitName
	content      string
	oldContent   string
}

type WriteFsBuildContextFileOp struct {
	build       model.BuildUnitRef
	filename    string
	destination model.BuildFilePath
	content     string
	oldContent  string
	mode        os.FileMode
	oldMode     os.FileMode // 0 when the file did not previously exist
}

type WriteFsContainerConfigFileOp struct {
	container  model.ContainerUnitRef
	mountPath  model.ContainerMountPath
	content    string
	oldContent string
	mode       os.FileMode
	oldMode    os.FileMode // 0 when the file did not previously exist
}

// WriteFsContainerConfigDirOp writes (or updates) all files for one configDir.
// version is the target version number to write into; the caller increments from CurrentDirVersion.
// oldFiles holds the content of each file in the previous version (keyed by filename),
// oldModes holds the permission bits of each file in the previous version (keyed by filename),
// both used to render per-file diffs in the plan display.
type WriteFsContainerConfigDirOp struct {
	container model.ContainerUnitRef
	mountPath model.ContainerMountPath
	version   int
	files     []model.ContainerConfigFile
	oldFiles  map[string]string
	oldModes  map[string]os.FileMode
}

// UpsertPodmanSecretOp creates or replaces a single podman secret entry.
// Name is the full podman secret name (<specname>-<key>); SpecName is used for
// grouping in the diff display. Value is the decrypted plaintext, populated during
// the plan phase and must never be logged or serialized. Labels are passed through
// to podman secret create verbatim (e.g. {"syslet/hash": "<sha>"}).
type UpsertPodmanSecretOp struct {
	SpecName string
	Name     string
	Value    model.Plaintext
	Labels   map[string]string
}

// ReloadSystemdServiceOp triggers systemctl reload on a running container's service.
type ReloadSystemdServiceOp struct {
	container model.ContainerUnitRef
}

type StartSystemdServiceOp struct {
	ref model.UnitRef
}

// Ref exposes the unit this op starts. See [StopSystemdServiceOp.Ref] for why
// the accessor exists.
func (op StartSystemdServiceOp) Ref() model.UnitRef { return op.ref }

// ApplyResult tracks the outcome for a single unit.
type ApplyResult struct {
	fullUnitName model.FullUnitName
	status       OperationStatus
	message      string
	errored      bool
}

func (p *ApplyPlan) StopSystemdService(ref model.UnitRef) {
	p.StopSystemdServices = append(p.StopSystemdServices, StopSystemdServiceOp{ref: ref})
}

func (p *ApplyPlan) StartSystemdService(ref model.UnitRef) {
	p.StartSystemdServices = append(p.StartSystemdServices, StartSystemdServiceOp{ref: ref})
}

func (p *ApplyPlan) WriteFsQuadletUnitFile(fn model.FullUnitName, content, oldContent string) {
	p.WriteFsQuadletUnitFiles = append(p.WriteFsQuadletUnitFiles, WriteFsQuadletUnitFileOp{fullUnitName: fn, content: content, oldContent: oldContent})
}

func (p *ApplyPlan) WriteFsBuildContextFile(ref model.BuildUnitRef, filename string, destinationPath model.BuildFilePath, content, oldContent string, mode, oldMode os.FileMode) {
	p.WriteFsBuildContextFiles = append(p.WriteFsBuildContextFiles, WriteFsBuildContextFileOp{build: ref, filename: filename, destination: destinationPath, content: content, oldContent: oldContent, mode: mode, oldMode: oldMode})
}

func (p *ApplyPlan) WriteFsContainerConfigFile(ref model.ContainerUnitRef, mountPath model.ContainerMountPath, content, oldContent string, mode, oldMode os.FileMode) {
	p.WriteFsContainerConfigFiles = append(p.WriteFsContainerConfigFiles, WriteFsContainerConfigFileOp{container: ref, mountPath: mountPath, content: content, oldContent: oldContent, mode: mode, oldMode: oldMode})
}

func (p *ApplyPlan) WriteFsContainerConfigDir(ref model.ContainerUnitRef, mountPath model.ContainerMountPath, version int, files []model.ContainerConfigFile, oldFiles map[string]string, oldModes map[string]os.FileMode) {
	p.WriteFsContainerConfigDirs = append(p.WriteFsContainerConfigDirs, WriteFsContainerConfigDirOp{container: ref, mountPath: mountPath, version: version, files: files, oldFiles: oldFiles, oldModes: oldModes})
}

func (p *ApplyPlan) DeleteFsContainerConfigDir(ref model.ContainerUnitRef, internalDirname string) {
	p.DeleteFsContainerConfigDirs = append(p.DeleteFsContainerConfigDirs, DeleteFsContainerConfigDirsOp{container: ref, mountPathHash: internalDirname})
}

func (p *ApplyPlan) ReloadSystemdService(ref model.ContainerUnitRef) {
	p.ReloadSystemdServices = append(p.ReloadSystemdServices, ReloadSystemdServiceOp{container: ref})
}

func (p *ApplyPlan) DeleteFsQuadletUnitFile(fn model.FullUnitName) {
	p.DeleteFsQuadletUnitFiles = append(p.DeleteFsQuadletUnitFiles, DeleteFsQuadletUnitFileOp{fullUnitName: fn})
}

func (p *ApplyPlan) DeleteFsBuildContextFile(ref model.BuildUnitRef, filename string) {
	p.DeleteFsBuildContextFiles = append(p.DeleteFsBuildContextFiles, DeleteFsBuildContextFileOp{build: ref, filename: filename})
}

func (p *ApplyPlan) DeleteFsBuildContext(ref model.BuildUnitRef) {
	p.DeleteFsBuildContexts = append(p.DeleteFsBuildContexts, DeleteFsBuildContextOp{build: ref})
}

func (p *ApplyPlan) DeleteFsContainerConfigFile(ref model.ContainerUnitRef, internalFilename string) {
	p.DeleteFsContainerConfigFiles = append(p.DeleteFsContainerConfigFiles, DeleteFsContainerConfigFileOp{container: ref, internalFilename: internalFilename})
}

func (p *ApplyPlan) DeleteFsContainerConfig(ref model.ContainerUnitRef) {
	p.DeleteFsContainerConfigs = append(p.DeleteFsContainerConfigs, DeleteFsContainerConfigOp{container: ref})
}

func (p *ApplyPlan) DeletePodmanVolume(ref model.VolumeUnitRef) {
	p.DeletePodmanVolumes = append(p.DeletePodmanVolumes, DeletePodmanVolumeOp{volume: ref})
}

func (p *ApplyPlan) DeletePodmanNetwork(ref model.NetworkUnitRef) {
	p.DeletePodmanNetworks = append(p.DeletePodmanNetworks, DeletePodmanNetworkOp{network: ref})
}

func (p *ApplyPlan) DeletePodmanImage(tag model.ImageTag) {
	p.DeletePodmanImages = append(p.DeletePodmanImages, DeletePodmanImageOp{tag: tag})
}

func (p *ApplyPlan) UpsertPodmanSecret(specName, name string, value model.Plaintext, labels map[string]string) {
	p.UpsertPodmanSecrets = append(p.UpsertPodmanSecrets, UpsertPodmanSecretOp{SpecName: specName, Name: name, Value: value, Labels: labels})
}

func (p *ApplyPlan) DeletePodmanSecret(specName, name string) {
	p.DeletePodmanSecrets = append(p.DeletePodmanSecrets, DeletePodmanSecretOp{SpecName: specName, Name: name})
}

func (p *ApplyPlan) RecordResult(fn model.FullUnitName, status OperationStatus, message string) {
	p.Results = append(p.Results, ApplyResult{fullUnitName: fn, status: status, message: message})
}

func (p *ApplyPlan) NeedsReload() bool {
	return len(p.WriteFsQuadletUnitFiles) > 0 || len(p.DeleteFsQuadletUnitFiles) > 0
}

func (p *ApplyPlan) HasErrors() bool {
	if len(p.Errors) > 0 {
		return true
	}
	for _, r := range p.Results {
		if r.errored {
			return true
		}
	}
	return false
}

func (p *ApplyPlan) RecordGenericError(message string) {
	p.Errors = append(p.Errors, message)
}

func (p *ApplyPlan) RecordError(name model.FullUnitName, message string) {
	for i := range p.Results {
		if p.Results[i].fullUnitName == name {
			p.Results[i].errored = true
			p.Results[i].status = StatusError
			p.Results[i].message = message
			return
		}
	}
	p.Results = append(p.Results, ApplyResult{fullUnitName: name, status: StatusError, message: message, errored: true})
}

// BuildPlan validates already-loaded specs, builds an execution plan by diffing
// against the installed state, and runs pre-flight staging validation on any unit
// files that would be written. Callers load raw themselves (from a path via
// api.LoadSpecsFS or a stream via api.LoadSpecsReader), keeping BuildPlan free of
// any I/O concerning the spec source. pc and decryptor are required for secret
// support: pass nil for both when the caller does not manage secrets.
func BuildPlan(ctx context.Context, fs afero.Fs, mgrs filestore.FileManagers, sd *systemd.Client, jr systemd.JournalReader, gen systemd.QuadletGeneratorRunner, az systemd.AnalyzeRunner, pc podman.Interface, decryptor *sops.Decryptor, raw api.LoadResult) (*ApplyPlan, error) {
	units, err := loader.Parse(raw)
	if err != nil {
		return nil, err
	}
	secrets, err := loader.ParseSecrets(raw)
	if err != nil {
		return nil, err
	}

	plan := &ApplyPlan{}

	if err := validate.PreRender(units, secrets); err != nil {
		plan.RecordGenericError(fmt.Sprintf("pre-render validation: %v", err))
		return plan, nil
	}

	result, err := render.NewResult(units, mgrs.Config.Resolve, mgrs.Build.Resolve)
	if err != nil {
		return nil, err
	}

	if err := validate.PostRender(result); err != nil {
		plan.RecordGenericError(fmt.Sprintf("post-render validation: %v", err))
		return plan, nil
	}

	changedNetworks := make(map[model.NetworkUnitRef]bool)
	changedBuilds := make(map[model.BuildUnitRef]bool)
	changedVolumes := make(map[model.VolumeUnitRef]bool)
	for _, r := range result.Volumes {
		buildPlanUnitVolume(sd, plan, r, changedVolumes)
	}
	for _, r := range result.Networks {
		buildPlanUnitNetwork(sd, plan, r, changedNetworks)
	}
	for _, r := range result.Builds {
		buildPlanUnitBuild(sd, mgrs.Build, plan, r, changedBuilds)
	}
	// Secrets must be diffed before containers so that containers referencing
	// a changed secret can be scheduled for restart in the same pass.
	changedSecrets := buildPlanSecrets(ctx, pc, decryptor, plan, secrets)
	if plan.HasErrors() {
		return plan, nil
	}
	for _, r := range result.Containers {
		buildPlanUnitContainer(ctx, sd, mgrs.Config, plan, r, changedNetworks, changedBuilds, changedVolumes, changedSecrets)
	}

	stale, err := loadStaleUnits(sd, result.UnitNames)
	if err != nil {
		return nil, fmt.Errorf("finding stale units: %w", err)
	}
	for _, u := range stale {
		switch u.ref.UnitType() {
		case model.UnitTypeContainer:
			buildPlanUnitStaleContainer(ctx, sd, plan, u.ref.(model.ContainerUnitRef), u.options)
		case model.UnitTypeVolume:
			buildPlanUnitStaleVolume(plan, u.ref.(model.VolumeUnitRef), u.options)
		case model.UnitTypeNetwork:
			buildPlanUnitStaleNetwork(plan, u.ref.(model.NetworkUnitRef), u.options)
		case model.UnitTypeBuild:
			buildPlanUnitStaleBuild(plan, u.ref.(model.BuildUnitRef), u.options)
		}
	}

	if len(plan.WriteFsQuadletUnitFiles) > 0 {
		stage, err := validate.NewStaging(fs, gen, az)
		if err != nil {
			return nil, fmt.Errorf("setting up staging: %w", err)
		}
		for _, r := range result.Containers {
			stage.AddRenderedUnit(r)
		}
		for _, r := range result.Volumes {
			stage.AddRenderedUnit(r)
		}
		for _, r := range result.Networks {
			stage.AddRenderedUnit(r)
		}
		for _, r := range result.Builds {
			stage.AddRenderedUnit(r)
		}
		for _, msg := range stage.Validate(ctx, slog.Default(), jr) {
			plan.RecordGenericError(msg)
		}
	}

	return plan, nil
}

// staleUnit holds data about an installed unit not in the desired set.
type staleUnit struct {
	ref     model.UnitRef
	options []gounit.UnitOption
}

func loadStaleUnits(sd *systemd.Client, desiredNames map[model.FullUnitName]bool) ([]staleUnit, error) {
	var stale []staleUnit
	for _, t := range model.AllUnitTypes {
		files, err := sd.ListUnitFiles(t.FileExtension())
		if err != nil {
			return nil, err
		}
		for _, fn := range files {
			if desiredNames[fn] {
				continue
			}
			ref, err := model.ParseFullUnitName(fn)
			if err != nil {
				return nil, fmt.Errorf("unrecognized unit file %s: %w", fn, err)
			}
			unitContent, err := sd.ReadUnitFile(fn)
			if err != nil {
				return nil, fmt.Errorf("reading installed unit %s: %w", fn, err)
			}
			ptrOpts, err := gounit.DeserializeOptions(bytes.NewReader(unitContent))
			if err != nil {
				return nil, fmt.Errorf("parsing installed unit %s: %w", fn, err)
			}
			unitOpts := make([]gounit.UnitOption, len(ptrOpts))
			for i, o := range ptrOpts {
				unitOpts[i] = *o
			}
			stale = append(stale, staleUnit{ref: ref, options: unitOpts})
		}
	}
	return stale, nil
}

type unitChanges struct {
	fullUnitName        model.FullUnitName
	isNew               bool
	isChanged           bool
	meaningfullyChanged bool // isChanged, ignoring metadata-only sections
	oldContent          string
	newContent          string
	existingOptions     []gounit.UnitOption // parsed options of the installed unit; nil for new units
}

func computeUnitChanges(sd *systemd.Client, plan *ApplyPlan, r render.RenderedUnit) (uc unitChanges, ok bool) {
	fn := r.Unit.Ref().FullName()
	newContent := r.Content
	existing, err := sd.ReadUnitFile(fn)
	if os.IsNotExist(err) {
		return unitChanges{
			fullUnitName: fn,
			isNew:        true,
			isChanged:    true,
			newContent:   newContent,
		}, true
	} else if err != nil {
		plan.RecordError(fn, fmt.Sprintf("reading installed unit: %v", err))
		return unitChanges{}, false
	}
	oldContent := string(existing)
	ptrOpts, err := gounit.DeserializeOptions(bytes.NewReader(existing))
	if err != nil {
		plan.RecordError(fn, fmt.Sprintf("parsing installed unit: %v", err))
		return unitChanges{}, false
	}
	existingOptions := make([]gounit.UnitOption, len(ptrOpts))
	for i, o := range ptrOpts {
		existingOptions[i] = *o
	}
	contentChanged := util.SHA256Hex([]byte(newContent)) != util.SHA256Hex(existing)
	meaningfullyChanged := false
	if contentChanged {
		oldStripped, err := render.StripMetadataSections(oldContent)
		if err != nil {
			plan.RecordError(fn, fmt.Sprintf("processing installed unit: %v", err))
			return unitChanges{}, false
		}
		newStripped, err := render.StripMetadataSections(newContent)
		if err != nil {
			plan.RecordError(fn, fmt.Sprintf("processing new unit: %v", err))
			return unitChanges{}, false
		}
		meaningfullyChanged = util.SHA256Hex([]byte(newStripped)) != util.SHA256Hex([]byte(oldStripped))
	}
	return unitChanges{
		fullUnitName:        fn,
		isChanged:           contentChanged,
		meaningfullyChanged: meaningfullyChanged,
		oldContent:          oldContent,
		newContent:          newContent,
		existingOptions:     existingOptions,
	}, true
}

func (uc unitChanges) applyToPlan(plan *ApplyPlan) {
	if uc.isChanged {
		plan.WriteFsQuadletUnitFile(uc.fullUnitName, uc.newContent, uc.oldContent)
	}
}

func (uc unitChanges) recordResult(plan *ApplyPlan) {
	if uc.isNew {
		plan.RecordResult(uc.fullUnitName, StatusCreated, "created")
	} else if uc.isChanged {
		plan.RecordResult(uc.fullUnitName, StatusUpdated, "unit updated")
	} else {
		plan.RecordResult(uc.fullUnitName, StatusUnchanged, "up to date")
	}
}

// buildPlanUnitStaleResource records removal or skip for a prune-eligible resource unit.
// deleteFn is called only when IsReclaimPolicyDelete is true; it performs the actual
// Podman-side deletion (volume rm, network rm, etc.).
func buildPlanUnitStaleResource(plan *ApplyPlan, fullName model.FullUnitName, options []gounit.UnitOption, deleteFn func()) {
	if render.IsUnitRemovalAllowed(options) {
		plan.RecordResult(fullName, StatusRemoved, "removed")
		plan.DeleteFsQuadletUnitFile(fullName)
		if render.IsReclaimPolicyDelete(options) {
			deleteFn()
		}
	} else {
		plan.RecordResult(fullName, StatusSkipped, "not marked for removal (removalAllowed not set)")
	}
}
