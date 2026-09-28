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
	"slices"
	"strings"

	gounit "github.com/coreos/go-systemd/v22/unit"
	"github.com/spf13/afero"

	"github.com/xchangeee/syslet/internal/api"
	"github.com/xchangeee/syslet/internal/filestore"
	"github.com/xchangeee/syslet/internal/loader"
	"github.com/xchangeee/syslet/internal/model"
	"github.com/xchangeee/syslet/internal/podman"
	"github.com/xchangeee/syslet/internal/render"
	"github.com/xchangeee/syslet/internal/sops"
	"github.com/xchangeee/syslet/internal/systemd"
	"github.com/xchangeee/syslet/internal/util"
	"github.com/xchangeee/syslet/internal/validate"
)

// UnitStatus classifies a UnitOutcome: what the plan does to the unit, or
// StatusError once an operation on it failed during the apply.
type UnitStatus string

const (
	StatusCreated   UnitStatus = "created"
	StatusUpdated   UnitStatus = "updated"
	StatusUnchanged UnitStatus = "unchanged"
	StatusRemoved   UnitStatus = "removed"
	StatusSkipped   UnitStatus = "skipped"
	StatusError     UnitStatus = "error"
)

// ApplyPlan contains all operations to execute, organized by phase, and the
// planned outcome of every unit. BuildPlan only returns a plan it could build
// without errors, so holding an ApplyPlan means it is safe to display and
// apply; errors come back as a *PlanError instead.
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

	Outcomes []UnitOutcome
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

// DeletePodmanNetworkOp removes a podman network. recreate marks a delete that
// is part of converging a changed network unit (its service recreates the
// network afterwards) rather than reclaiming a removed one: Apply fails the unit
// when a recreate delete fails, but keeps reclamation best-effort.
type DeletePodmanNetworkOp struct {
	network  model.NetworkUnitRef
	recreate bool
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

// UnitOutcome is the planned effect of an apply on one unit, one row of the
// summary: its status and a description of what changes, such as "unit
// updated, restarted (desired: running)". BuildPlan records one for every unit
// in the input or on the host. The plan keeps it as planned; when an operation
// on the unit fails, the ApplyReport shows a StatusError row in its place.
type UnitOutcome struct {
	unit    model.FullUnitName
	status  UnitStatus
	message string
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

// DeletePodmanNetwork schedules reclaiming the podman network of a removed network unit.
func (p *ApplyPlan) DeletePodmanNetwork(ref model.NetworkUnitRef) {
	p.DeletePodmanNetworks = append(p.DeletePodmanNetworks, DeletePodmanNetworkOp{network: ref})
}

// RecreatePodmanNetwork schedules deleting the podman network of a changed
// network unit, so its service creates it again with the new settings.
func (p *ApplyPlan) RecreatePodmanNetwork(ref model.NetworkUnitRef) {
	p.DeletePodmanNetworks = append(p.DeletePodmanNetworks, DeletePodmanNetworkOp{network: ref, recreate: true})
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

func (p *ApplyPlan) RecordOutcome(fn model.FullUnitName, status UnitStatus, message string) {
	p.Outcomes = append(p.Outcomes, UnitOutcome{unit: fn, status: status, message: message})
}

func (p *ApplyPlan) NeedsReload() bool {
	return len(p.WriteFsQuadletUnitFiles) > 0 || len(p.DeleteFsQuadletUnitFiles) > 0
}

// HasChanges reports whether the plan holds any operation, which is what an
// apply would do to the host. DisplayPlan and DisplayReport use it to decide
// whether to print "No changes detected"; units that are skipped or unchanged
// record outcomes but no operations, so they don't count.
func (p *ApplyPlan) HasChanges() bool {
	return len(p.StopSystemdServices) > 0 ||
		len(p.DeleteFsQuadletUnitFiles) > 0 ||
		len(p.DeleteFsBuildContextFiles) > 0 ||
		len(p.DeleteFsBuildContexts) > 0 ||
		len(p.DeleteFsContainerConfigFiles) > 0 ||
		len(p.DeleteFsContainerConfigDirs) > 0 ||
		len(p.DeleteFsContainerConfigs) > 0 ||
		len(p.DeletePodmanSecrets) > 0 ||
		len(p.DeletePodmanVolumes) > 0 ||
		len(p.DeletePodmanNetworks) > 0 ||
		len(p.DeletePodmanImages) > 0 ||
		len(p.WriteFsQuadletUnitFiles) > 0 ||
		len(p.WriteFsBuildContextFiles) > 0 ||
		len(p.WriteFsContainerConfigFiles) > 0 ||
		len(p.WriteFsContainerConfigDirs) > 0 ||
		len(p.UpsertPodmanSecrets) > 0 ||
		len(p.ReloadSystemdServices) > 0 ||
		len(p.StartSystemdServices) > 0
}

// PlanError is the error BuildPlan returns when the specs or the host state
// don't allow a plan: validation failures and per-unit problems found while
// planning. It replaces the plan rather than accompanying it, so a plan that
// must not be applied can't reach DisplayPlan or Apply. The CLI prints it with
// DisplayPlanErrors.
type PlanError struct {
	// Errors are failures not tied to one unit, such as a validation stage or
	// a secret that can't be decrypted.
	Errors []string
	// UnitErrors are failures of single units, one per unit.
	UnitErrors []UnitError
}

// UnitError is a planning failure of one unit.
type UnitError struct {
	Unit    model.FullUnitName
	Message string
}

func (e *PlanError) Error() string { return "plan has errors" }

// planner holds the ApplyPlan under construction. The buildPlan* functions
// add operations and outcomes to it through the embedded plan, and record
// errors on the planner, which BuildPlan turns into a *PlanError instead of
// returning the plan. Keeping errors off ApplyPlan is what makes an ApplyPlan
// with errors unrepresentable.
type planner struct {
	*ApplyPlan
	errors     []string
	unitErrors []UnitError
}

func newPlanner() *planner {
	return &planner{ApplyPlan: &ApplyPlan{}}
}

func (p *planner) recordGenericError(message string) {
	p.errors = append(p.errors, message)
}

// recordUnitError records a planning failure of one unit. A later failure of
// the same unit replaces the earlier one, since planning stops working on a
// unit once it failed and the last message is the most specific.
func (p *planner) recordUnitError(name model.FullUnitName, message string) {
	for i := range p.unitErrors {
		if p.unitErrors[i].Unit == name {
			p.unitErrors[i].Message = message
			return
		}
	}
	p.unitErrors = append(p.unitErrors, UnitError{Unit: name, Message: message})
}

func (p *planner) hasErrors() bool {
	return len(p.errors) > 0 || len(p.unitErrors) > 0
}

// build returns either the finished plan or, if anything was recorded as an
// error, a *PlanError; never both.
func (p *planner) build() (*ApplyPlan, error) {
	if p.hasErrors() {
		return nil, &PlanError{Errors: p.errors, UnitErrors: p.unitErrors}
	}
	return p.ApplyPlan, nil
}

// BuildPlan validates already-loaded specs, builds an execution plan by diffing
// against the installed state, and runs pre-flight staging validation on any unit
// files that would be written. Callers load raw themselves (from a path via
// api.LoadSpecsFS or a stream via api.LoadSpecsReader), keeping BuildPlan free of
// any I/O concerning the spec source. pc and decryptor are required for secret
// support: pass nil for both when the caller does not manage secrets.
//
// It returns a *PlanError when the specs or the host state don't allow a plan,
// and any other error when it couldn't finish looking, such as an unreadable
// unit directory.
func BuildPlan(ctx context.Context, fs afero.Fs, mgrs filestore.FileManagers, sd *systemd.Client, jr systemd.JournalReader, gen systemd.QuadletGeneratorRunner, az systemd.AnalyzeRunner, pc podman.Interface, decryptor *sops.Decryptor, raw api.LoadResult) (*ApplyPlan, error) {
	units, err := loader.Parse(raw)
	if err != nil {
		return nil, err
	}
	secrets, err := loader.ParseSecrets(raw)
	if err != nil {
		return nil, err
	}

	planner := newPlanner()

	if err := validate.PreRender(units, secrets); err != nil {
		planner.recordGenericError(fmt.Sprintf("pre-render validation: %v", err))
		return planner.build()
	}

	result, err := render.NewResult(units, mgrs.Config.Resolve, mgrs.Build.Resolve)
	if err != nil {
		return nil, err
	}

	if err := validate.PostRender(result); err != nil {
		planner.recordGenericError(fmt.Sprintf("post-render validation: %v", err))
		return planner.build()
	}

	changedNetworks := make(map[model.NetworkUnitRef]bool)
	changedBuilds := make(map[model.BuildUnitRef]bool)
	changedVolumes := make(map[model.VolumeUnitRef]bool)
	for _, r := range result.Volumes {
		buildPlanUnitVolume(sd, planner, r, changedVolumes)
	}
	for _, r := range result.Networks {
		buildPlanUnitNetwork(sd, planner, r, changedNetworks)
	}
	for _, r := range result.Builds {
		buildPlanUnitBuild(sd, mgrs.Build, planner, r, changedBuilds)
	}
	// Secrets must be diffed before containers so that containers referencing
	// a changed secret can be scheduled for restart in the same pass.
	changedSecrets := buildPlanSecrets(ctx, pc, decryptor, planner, secrets)
	if planner.hasErrors() {
		return planner.build()
	}
	for _, r := range result.Containers {
		buildPlanUnitContainer(ctx, sd, mgrs.Config, planner, r, changedNetworks, changedBuilds, changedVolumes, changedSecrets)
	}

	stale, err := loadStaleUnits(sd, result.UnitNames)
	if err != nil {
		return nil, fmt.Errorf("finding stale units: %w", err)
	}
	heldBy := skippedContainerReferences(stale)
	for _, u := range stale {
		if holders := heldBy[u.ref.FullName()]; len(holders) > 0 {
			planner.RecordOutcome(u.ref.FullName(), StatusSkipped, "still referenced by "+strings.Join(holders, ", "))
			continue
		}
		switch u.ref.UnitType() {
		case model.UnitTypeContainer:
			buildPlanUnitStaleContainer(ctx, sd, planner, u.ref.(model.ContainerUnitRef), u.options)
		case model.UnitTypeVolume:
			buildPlanUnitStaleVolume(planner, u.ref.(model.VolumeUnitRef), u.options)
		case model.UnitTypeNetwork:
			buildPlanUnitStaleNetwork(planner, u.ref.(model.NetworkUnitRef), u.options)
		case model.UnitTypeBuild:
			buildPlanUnitStaleBuild(planner, u.ref.(model.BuildUnitRef), u.options)
		}
	}

	if len(planner.WriteFsQuadletUnitFiles) > 0 {
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
			planner.recordGenericError(msg)
		}
	}

	return planner.build()
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

// skippedContainerReferences maps each volume, network, and build unit to the
// stale containers that reference it but are kept because their removal is not
// allowed. It is the host-side counterpart of validate.PostRender, which only
// checks references within the desired set: a skipped container keeps running
// on the host, so BuildPlan must skip the units it depends on as a whole rather
// than delete their unit file (which breaks the container's quadlet generation)
// or reclaim their podman resource. Stale containers that are removed are
// stopped before reclamation runs, so they hold nothing back.
func skippedContainerReferences(stale []staleUnit) map[model.FullUnitName][]string {
	heldBy := make(map[model.FullUnitName][]string)
	for _, u := range stale {
		if u.ref.UnitType() != model.UnitTypeContainer || render.IsUnitRemovalAllowed(u.options) {
			continue
		}
		for _, ref := range containerUnitReferences(u.options) {
			heldBy[ref] = append(heldBy[ref], string(u.ref.FullName()))
		}
	}
	for _, holders := range heldBy {
		slices.Sort(holders)
	}
	return heldBy
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

func computeUnitChanges(sd *systemd.Client, planner *planner, r render.RenderedUnit) (uc unitChanges, ok bool) {
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
		planner.recordUnitError(fn, fmt.Sprintf("reading installed unit: %v", err))
		return unitChanges{}, false
	}
	oldContent := string(existing)
	ptrOpts, err := gounit.DeserializeOptions(bytes.NewReader(existing))
	if err != nil {
		planner.recordUnitError(fn, fmt.Sprintf("parsing installed unit: %v", err))
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
			planner.recordUnitError(fn, fmt.Sprintf("processing installed unit: %v", err))
			return unitChanges{}, false
		}
		newStripped, err := render.StripMetadataSections(newContent)
		if err != nil {
			planner.recordUnitError(fn, fmt.Sprintf("processing new unit: %v", err))
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

func (uc unitChanges) applyToPlan(planner *planner) {
	if uc.isChanged {
		planner.WriteFsQuadletUnitFile(uc.fullUnitName, uc.newContent, uc.oldContent)
	}
}

func (uc unitChanges) recordOutcome(planner *planner) {
	if uc.isNew {
		planner.RecordOutcome(uc.fullUnitName, StatusCreated, "created")
	} else if uc.isChanged {
		planner.RecordOutcome(uc.fullUnitName, StatusUpdated, "unit updated")
	} else {
		planner.RecordOutcome(uc.fullUnitName, StatusUnchanged, "up to date")
	}
}

// buildPlanUnitStaleResource records removal or skip for a prune-eligible resource unit.
// deleteFn is called only when IsReclaimPolicyDelete is true; it performs the actual
// Podman-side deletion (volume rm, network rm, etc.).
func buildPlanUnitStaleResource(planner *planner, fullName model.FullUnitName, options []gounit.UnitOption, deleteFn func()) {
	if render.IsUnitRemovalAllowed(options) {
		planner.RecordOutcome(fullName, StatusRemoved, "removed")
		planner.DeleteFsQuadletUnitFile(fullName)
		if render.IsReclaimPolicyDelete(options) {
			deleteFn()
		}
	} else {
		planner.RecordOutcome(fullName, StatusSkipped, render.RemovalSkipReason(options))
	}
}
