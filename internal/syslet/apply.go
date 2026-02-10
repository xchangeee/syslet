package syslet

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"codeberg.org/xchangeee/syslet/internal/systemd"

	gounit "github.com/coreos/go-systemd/v22/unit"
	"github.com/spf13/afero"
)

// Per-phase operation structs for the apply plan.

// StopOp represents a container that needs to be stopped.
type StopOp struct {
	fullName string
}

// ConfigFileWrite represents a config file to write.
type ConfigFileWrite struct {
	containerName string
	filename      string
	content       string
}

// ConfigFileDelete represents a config file to delete.
type ConfigFileDelete struct {
	containerName string
	filename      string
}

// ConfigDirDelete represents a container config directory to delete entirely.
type ConfigDirDelete struct {
	containerName string
}

// UnitFileWrite represents a unit file to write.
type UnitFileWrite struct {
	fullName string
	content  string
}

// UnitFileDelete represents a unit file to delete (prune).
type UnitFileDelete struct {
	fullName string
}

// StartOp represents a container that needs to be started.
type StartOp struct {
	fullName string
}

// VolumeDeleteOp represents a volume that needs to be deleted via podman.
type VolumeDeleteOp struct {
	name string
}

// NetworkDeleteOp represents a network that needs to be deleted via podman.
type NetworkDeleteOp struct {
	name string
}

// ApplyResult tracks the outcome for a single unit (for summary reporting).
type ApplyResult struct {
	fullName string
	status   string // "created", "updated", "unchanged", "removed", "error"
	message  string
	errored  bool
}

// ApplyPlan contains all operations to execute, organized by phase.
// This is the global struct passed to diff functions to accumulate operations.
type ApplyPlan struct {
	StopContainers   []StopOp
	WriteConfigs     []ConfigFileWrite
	DeleteConfigs    []ConfigFileDelete
	DeleteConfigDirs []ConfigDirDelete
	WriteUnits       []UnitFileWrite
	DeleteUnits      []UnitFileDelete
	DeleteVolumes    []VolumeDeleteOp
	DeleteNetworks   []NetworkDeleteOp
	StartContainers  []StartOp

	NeedsReload bool          // set during diff if daemon-reload is needed
	Results     []ApplyResult // final summary for reporting
}

// Apply reads specs from a zip file, validates them, diffs against the
// installed state, and executes changes in coordinated order:
//  1. Stop containers that changed or are being pruned
//  2. Write config files
//  3. Write unit files
//  4. Remove pruned unit files and config dirs
//  5. Delete podman volumes and networks (if ReclaimPolicy is "Delete")
//  6. Single daemon-reload
//  7. Start containers that should be running
func Apply(ctx context.Context, logger *slog.Logger, fs afero.Fs, sd *systemd.Client, zipPath string) error {
	specs, err := LoadSpecsFromZip(zipPath)
	if err != nil {
		return err
	}

	// Pre-render validation: check raw input data.
	if err := ValidateSpecs(specs); err != nil {
		return fmt.Errorf("pre-render validation: %w", err)
	}

	cfg := NewConfigFileManager(fs)
	containerConfigDir := DefaultContainerConfigDir

	// Render all specs to intermediate unit options.
	var containers, volumes, networks []RenderedUnit
	specNames := make(map[string]bool)

	for _, s := range specs {
		var r RenderedUnit
		var err error

		switch s.GetType() {
		case SpecTypeContainer:
			r, err = renderContainer(s.(*ContainerSpec), containerConfigDir)
			if err != nil {
				return fmt.Errorf("rendering %s: %w", s.GetName(), err)
			}
			containers = append(containers, r)
		case SpecTypeVolume:
			r, err = renderVolume(s.(*VolumeSpec))
			if err != nil {
				return fmt.Errorf("rendering %s: %w", s.GetName(), err)
			}
			volumes = append(volumes, r)
		case SpecTypeNetwork:
			r, err = renderNetwork(s.(*NetworkSpec))
			if err != nil {
				return fmt.Errorf("rendering %s: %w", s.GetName(), err)
			}
			networks = append(networks, r)
		}
		specNames[fullUnitName(s)] = true
	}

	// Post-render validation: check rendered units and cross-references.
	if err := ValidateRenderedUnits(containers, volumes, networks); err != nil {
		return fmt.Errorf("post-render validation: %w", err)
	}

	// Serialize unit options to strings after validation passes.
	for i := range containers {
		content, err := serializeUnitOptions(containers[i].UnitOptions)
		if err != nil {
			return fmt.Errorf("serializing %s: %w", containers[i].Spec.GetName(), err)
		}
		containers[i].Content = content
	}
	for i := range volumes {
		content, err := serializeUnitOptions(volumes[i].UnitOptions)
		if err != nil {
			return fmt.Errorf("serializing %s: %w", volumes[i].Spec.GetName(), err)
		}
		volumes[i].Content = content
	}
	for i := range networks {
		content, err := serializeUnitOptions(networks[i].UnitOptions)
		if err != nil {
			return fmt.Errorf("serializing %s: %w", networks[i].Spec.GetName(), err)
		}
		networks[i].Content = content
	}

	// Build the apply plan by diffing all specs.
	plan := &ApplyPlan{}

	// Diff containers (handles config files, unit files, start/stop).
	for _, r := range containers {
		if err := diffContainer(ctx, sd, cfg, plan, r.Spec, r.Content); err != nil {
			return fmt.Errorf("diffing container %s: %w", r.Spec.GetName(), err)
		}
	}

	// Diff volumes and networks (unit files only).
	for _, r := range volumes {
		diffSimple(sd, plan, r.Spec, r.Content)
	}
	for _, r := range networks {
		diffSimple(sd, plan, r.Spec, r.Content)
	}

	// Find installed files that are NOT in the current spec set (stale → prune).
	if err := findStaleUnits(ctx, sd, plan, specNames); err != nil {
		return fmt.Errorf("finding stale units: %w", err)
	}

	// Execute in strict global order (4 phases).

	// Phase 1: Stop containers that need stopping.
	for _, op := range plan.StopContainers {
		logger.Info("stopping", "unit", op.fullName)
		if err := sd.StopContainer(ctx, op.fullName); err != nil {
			logger.Error("failed to stop", "unit", op.fullName, "error", err)
			recordError(plan, op.fullName, fmt.Sprintf("failed to stop: %v", err))
		}
	}

	// Phase 2: Write and delete config files.
	for _, op := range plan.WriteConfigs {
		logger.Info("writing config", "container", op.containerName, "file", op.filename)
		if err := cfg.Write(op.containerName, op.filename, op.content); err != nil {
			logger.Error("failed to write config", "container", op.containerName, "file", op.filename, "error", err)
			recordError(plan, op.containerName+".container", fmt.Sprintf("failed to write config %s: %v", op.filename, err))
		}
	}
	for _, op := range plan.DeleteConfigs {
		logger.Info("removing stale config", "container", op.containerName, "file", op.filename)
		if err := cfg.RemoveFile(op.containerName, op.filename); err != nil {
			logger.Error("failed to remove stale config", "container", op.containerName, "file", op.filename, "error", err)
		}
	}
	for _, op := range plan.DeleteConfigDirs {
		logger.Info("removing config directory", "container", op.containerName)
		if err := cfg.RemoveAll(op.containerName); err != nil {
			logger.Error("failed to remove config dir", "container", op.containerName, "error", err)
		}
	}

	// Phase 3: Write and delete unit files.
	for _, op := range plan.WriteUnits {
		logger.Info("writing unit file", "unit", op.fullName)
		if err := sd.WriteUnitFile(op.fullName, []byte(op.content)); err != nil {
			logger.Error("failed to write unit file", "unit", op.fullName, "error", err)
			recordError(plan, op.fullName, fmt.Sprintf("failed to write unit file: %v", err))
		}
	}
	for _, op := range plan.DeleteUnits {
		logger.Info("removing stale unit", "unit", op.fullName)
		if err := sd.RemoveUnitFile(op.fullName); err != nil {
			logger.Error("failed to remove unit file", "unit", op.fullName, "error", err)
		}
	}

	// Delete podman volumes and networks if requested.
	for _, op := range plan.DeleteVolumes {
		logger.Info("deleting podman volume", "name", op.name)
		if err := deletePodmanVolume(ctx, op.name); err != nil {
			logger.Error("failed to delete volume", "name", op.name, "error", err)
		}
	}
	for _, op := range plan.DeleteNetworks {
		logger.Info("deleting podman network", "name", op.name)
		if err := deletePodmanNetwork(ctx, op.name); err != nil {
			logger.Error("failed to delete network", "name", op.name, "error", err)
		}
	}

	// Single daemon-reload if needed.
	if plan.NeedsReload {
		logger.Info("daemon-reload")
		if err := sd.DaemonReload(ctx); err != nil {
			logger.Error("daemon-reload failed", "error", err)
		}
	}

	// Phase 4: Start containers that need starting.
	for _, op := range plan.StartContainers {
		logger.Info("starting", "unit", op.fullName)
		if err := sd.StartContainer(ctx, op.fullName); err != nil {
			logger.Error("failed to start", "unit", op.fullName, "error", err)
			recordError(plan, op.fullName, fmt.Sprintf("failed to start: %v", err))
		}
	}

	// Print summary.
	for _, result := range plan.Results {
		fmt.Printf("%-40s %-10s %s\n", result.fullName, result.status, result.message)
	}

	// Return error if any operations failed.
	for _, result := range plan.Results {
		if result.errored {
			return fmt.Errorf("one or more units failed to apply")
		}
	}
	return nil
}

// diffSimple computes the diff for a volume or network spec.
// These are write-only: install the unit file if new or changed.
func diffSimple(sd *systemd.Client, plan *ApplyPlan, s Spec, newContent string) {
	fn := fullUnitName(s)
	isNew := false
	changed := false

	if !sd.UnitFileExists(fn) {
		isNew = true
		changed = true
	} else {
		existing, err := sd.ReadUnitFile(fn)
		if err != nil {
			recordError(plan, fn, fmt.Sprintf("reading installed unit: %v", err))
			return
		}
		if sha256hex([]byte(newContent)) != sha256hex(existing) {
			changed = true
		}
	}

	// Add write operation if unit changed.
	if changed {
		plan.WriteUnits = append(plan.WriteUnits, UnitFileWrite{
			fullName: fn,
			content:  newContent,
		})
		plan.NeedsReload = true
	}

	// Record result for summary.
	status := "unchanged"
	message := "up to date"
	if isNew {
		status = "created"
		message = "created"
	} else if changed {
		status = "updated"
		message = "unit updated"
	}
	plan.Results = append(plan.Results, ApplyResult{
		fullName: fn,
		status:   status,
		message:  message,
	})
}

// diffContainer computes the diff for a container spec, including config
// changes and start/stop decisions based on desired state and runtime state.
func diffContainer(ctx context.Context, sd *systemd.Client, cfg *ConfigFileManager, plan *ApplyPlan, s Spec, newContent string) error {
	container, ok := s.(*ContainerSpec)
	if !ok {
		return fmt.Errorf("diffContainer called with non-container spec")
	}

	fn := fullUnitName(s)
	isNew := false
	unitChanged := false
	configChanged := false

	if !sd.UnitFileExists(fn) {
		isNew = true
		unitChanged = true
		configChanged = len(container.Configs) > 0
	} else {
		// Check unit file changes.
		existing, err := sd.ReadUnitFile(fn)
		if os.IsNotExist(err) {
			isNew = true
			unitChanged = true
		} else if err != nil {
			return err
		} else if sha256hex([]byte(newContent)) != sha256hex(existing) {
			unitChanged = true
		}

		// Check config changes.
		configChanged = configsChanged(cfg, container)
	}

	// Query runtime state.
	var isRunning bool
	if !isNew {
		state, err := sd.ContainerState(ctx, fn)
		if err != nil {
			return fmt.Errorf("querying state of %s: %w", fn, err)
		}
		isRunning = state.ActiveState == "active" || state.ActiveState == "activating"
	}

	// Build operations based on changes and desired state.
	needsStop := false
	needsStart := false

	// If unit or config changed and currently running, need to stop first.
	if (unitChanged || configChanged) && isRunning {
		needsStop = true
	}

	switch strings.ToLower(container.DesiredState) {
	case "running":
		if !isRunning || needsStop {
			needsStart = true
		}
	case "stopped":
		if isRunning {
			needsStop = true
		}
	}

	// Add operations to plan.
	if needsStop {
		plan.StopContainers = append(plan.StopContainers, StopOp{fullName: fn})
	}

	if configChanged {
		addConfigOperations(plan, cfg, container)
	}

	if unitChanged {
		plan.WriteUnits = append(plan.WriteUnits, UnitFileWrite{
			fullName: fn,
			content:  newContent,
		})
		plan.NeedsReload = true
	}

	if needsStart {
		plan.StartContainers = append(plan.StartContainers, StartOp{fullName: fn})
	}

	// Record result for summary.
	status, message := summarizeContainer(isNew, unitChanged, configChanged, needsStop, needsStart)
	plan.Results = append(plan.Results, ApplyResult{
		fullName: fn,
		status:   status,
		message:  message,
	})

	return nil
}

// findStaleUnits scans /etc/containers/systemd/ for unit files that are not
// in the current spec set. Adds prune operations to the plan.
func findStaleUnits(ctx context.Context, sd *systemd.Client, plan *ApplyPlan, specNames map[string]bool) error {
	for _, ext := range []string{".container", ".volume", ".network"} {
		files, err := sd.ListUnitFiles(ext)
		if err != nil {
			return err
		}
		for _, fn := range files {
			if specNames[fn] {
				continue
			}

			// If it's a container, check if it's running so we stop it first.
			if ext == ".container" {
				state, err := sd.ContainerState(ctx, fn)
				if err == nil && (state.ActiveState == "active" || state.ActiveState == "activating") {
					plan.StopContainers = append(plan.StopContainers, StopOp{fullName: fn})
				}

				// Add operation to delete config directory.
				containerName := strings.TrimSuffix(fn, ".container")
				plan.DeleteConfigDirs = append(plan.DeleteConfigDirs, ConfigDirDelete{
					containerName: containerName,
				})
			}

			// For volumes and networks, check if reclaim policy is Delete.
			if ext == ".volume" || ext == ".network" {
				shouldDelete := checkReclaimPolicy(sd, fn)
				if shouldDelete {
					name := strings.TrimSuffix(fn, ext)
					if ext == ".volume" {
						plan.DeleteVolumes = append(plan.DeleteVolumes, VolumeDeleteOp{name: name})
					} else {
						plan.DeleteNetworks = append(plan.DeleteNetworks, NetworkDeleteOp{name: name})
					}
				}
			}

			// Add delete operation for unit file.
			plan.DeleteUnits = append(plan.DeleteUnits, UnitFileDelete{
				fullName: fn,
			})
			plan.NeedsReload = true

			// Record result for summary.
			plan.Results = append(plan.Results, ApplyResult{
				fullName: fn,
				status:   "removed",
				message:  "removed",
			})
		}
	}
	return nil
}

// recordError adds an error result to the plan.
func recordError(plan *ApplyPlan, fullName, message string) {
	// Check if result already exists, update it.
	for i := range plan.Results {
		if plan.Results[i].fullName == fullName {
			plan.Results[i].errored = true
			plan.Results[i].status = "error"
			plan.Results[i].message = message
			return
		}
	}
	// Otherwise add a new error result.
	plan.Results = append(plan.Results, ApplyResult{
		fullName: fullName,
		status:   "error",
		message:  message,
		errored:  true,
	})
}

// addConfigOperations adds config write and delete operations to the plan.
func addConfigOperations(plan *ApplyPlan, cfg *ConfigFileManager, container *ContainerSpec) {
	// Add write operations for all configs in the spec.
	for _, cfgEntry := range container.Configs {
		basename := filepath.Base(cfgEntry.TargetVolumePath)
		plan.WriteConfigs = append(plan.WriteConfigs, ConfigFileWrite{
			containerName: container.Name,
			filename:      basename,
			content:       cfgEntry.Content,
		})
	}

	// Add delete operations for stale configs (on disk but not in spec).
	deployed, err := cfg.ListFiles(container.Name)
	if err != nil {
		return
	}
	specFiles := make(map[string]bool)
	for _, ce := range container.Configs {
		specFiles[filepath.Base(ce.TargetVolumePath)] = true
	}
	for _, f := range deployed {
		if !specFiles[f] {
			plan.DeleteConfigs = append(plan.DeleteConfigs, ConfigFileDelete{
				containerName: container.Name,
				filename:      f,
			})
		}
	}

	// If container has no configs, schedule the entire config directory for deletion.
	if len(container.Configs) == 0 && len(deployed) > 0 {
		plan.DeleteConfigDirs = append(plan.DeleteConfigDirs, ConfigDirDelete{
			containerName: container.Name,
		})
	}
}

// summarizeContainer builds a status and message for a container result.
func summarizeContainer(isNew, unitChanged, configChanged, needsStop, needsStart bool) (status, message string) {
	var parts []string
	if isNew {
		parts = append(parts, "created")
		status = "created"
	} else if unitChanged || configChanged || needsStop || needsStart {
		status = "updated"
	} else {
		status = "unchanged"
	}

	if unitChanged && !isNew {
		parts = append(parts, "unit updated")
	}
	if configChanged {
		parts = append(parts, "config updated")
	}
	if needsStop && needsStart {
		parts = append(parts, "restarted")
	} else if needsStart {
		parts = append(parts, "started")
	} else if needsStop {
		parts = append(parts, "stopped")
	}

	if len(parts) == 0 {
		message = "up to date"
	} else {
		message = strings.Join(parts, ", ")
	}
	return status, message
}

// configsChanged checks if any config file in the spec differs from what's deployed.
func configsChanged(cfg *ConfigFileManager, container *ContainerSpec) bool {
	// Check if any spec configs differ from disk.
	for _, ce := range container.Configs {
		basename := filepath.Base(ce.TargetVolumePath)
		changed, err := cfg.IsChanged(container.Name, basename, ce.Content)
		if err != nil || changed {
			return true
		}
	}
	// Check if there are config files on disk that are no longer in the spec.
	deployed, _ := cfg.ListFiles(container.Name)
	specFiles := make(map[string]bool)
	for _, ce := range container.Configs {
		specFiles[filepath.Base(ce.TargetVolumePath)] = true
	}
	for _, f := range deployed {
		if !specFiles[f] {
			return true
		}
	}
	return false
}

// fullUnitName returns the quadlet file name for a spec (e.g. "webapp.container").
func fullUnitName(s Spec) string {
	return s.GetName() + "." + string(s.GetType())
}

// checkReclaimPolicy reads a unit file and checks if the ReclaimPolicy is set to "Delete".
// Returns true if the resource should be deleted when the unit is removed.
func checkReclaimPolicy(sd *systemd.Client, fullName string) bool {
	content, err := sd.ReadUnitFile(fullName)
	if err != nil {
		return false
	}

	// Parse unit file using systemd library.
	opts, err := gounit.Deserialize(bytes.NewReader(content))
	if err != nil {
		return false
	}

	// Look for ReclaimPolicy=Delete in X-Syslet section.
	for _, opt := range opts {
		if opt.Section == "X-Syslet" && opt.Name == "ReclaimPolicy" {
			return strings.EqualFold(opt.Value, "Delete")
		}
	}
	return false
}

// deletePodmanVolume executes 'podman volume rm <name>' to delete a volume.
func deletePodmanVolume(ctx context.Context, name string) error {
	cmd := exec.CommandContext(ctx, "podman", "volume", "rm", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("podman volume rm %s failed: %w (output: %s)", name, err, string(output))
	}
	return nil
}

// deletePodmanNetwork executes 'podman network rm <name>' to delete a network.
func deletePodmanNetwork(ctx context.Context, name string) error {
	cmd := exec.CommandContext(ctx, "podman", "network", "rm", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("podman network rm %s failed: %w (output: %s)", name, err, string(output))
	}
	return nil
}
