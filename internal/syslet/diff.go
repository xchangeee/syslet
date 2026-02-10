package syslet

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"codeberg.org/xchangeee/syslet/internal/api"
	"codeberg.org/xchangeee/syslet/internal/containerconfig"
	"codeberg.org/xchangeee/syslet/internal/systemd"
	"codeberg.org/xchangeee/syslet/internal/util"
)

// checkUnitFileChanged reads an existing unit file and determines if it's new or changed.
// Returns (isNew, contentChanged, error).
func checkUnitFileChanged(sd *systemd.Client, fullUnitName, newContent string) (bool, bool, error) {
	existing, err := sd.ReadUnitFile(fullUnitName)
	if os.IsNotExist(err) {
		// Unit doesn't exist, mark as new.
		return true, true, nil
	} else if err != nil {
		// Other read error.
		return false, false, err
	}
	// Unit exists, check if content changed.
	contentChanged := util.Sha256hex([]byte(newContent)) != util.Sha256hex(existing)
	return false, contentChanged, nil
}

// diffContainer computes the diff for a container spec, including config
// changes and start/stop decisions based on desired state and runtime state.
func diffContainer(ctx context.Context, sd *systemd.Client, cfg *containerconfig.ConfigFileManager, plan *ApplyPlan, r api.RenderedUnit) error {
	container, ok := r.Spec.(*api.ContainerSpec)
	if !ok {
		return fmt.Errorf("diffContainer called with non-container spec")
	}

	fn := r.Spec.FullUnitName()

	isNew, unitChanged, err := checkUnitFileChanged(sd, fn, r.Content)
	if err != nil {
		return err
	}

	configChanged := false
	if isNew {
		configChanged = len(container.Configs) > 0
	} else {
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
			content:  r.Content,
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

// diffSimple computes the diff for a volume or network api.
// These are write-only: install the unit file if new or changed.
func diffSimple(sd *systemd.Client, plan *ApplyPlan, r api.RenderedUnit) {
	fn := r.Spec.FullUnitName()

	isNew, changed, err := checkUnitFileChanged(sd, fn, r.Content)
	if err != nil {
		recordError(plan, fn, fmt.Sprintf("reading installed unit: %v", err))
		return
	}

	// Add write operation if unit changed.
	if changed {
		plan.WriteUnits = append(plan.WriteUnits, UnitFileWrite{
			fullName: fn,
			content:  r.Content,
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
				name := strings.TrimSuffix(fn, ext)
				if ext == ".volume" {
					spec := &api.VolumeSpec{Name: name}
					if spec.ShouldDeleteOnRemoval(sd) {
						plan.DeleteVolumes = append(plan.DeleteVolumes, VolumeDeleteOp{name: name})
					}
				} else {
					spec := &api.NetworkSpec{Name: name}
					if spec.ShouldDeleteOnRemoval(sd) {
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

// addConfigOperations adds config write and delete operations to the plan.
func addConfigOperations(plan *ApplyPlan, cfg *containerconfig.ConfigFileManager, container *api.ContainerSpec) {
	// Add write operations for all configs in the api.
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
func configsChanged(cfg *containerconfig.ConfigFileManager, container *api.ContainerSpec) bool {
	// Check if any spec configs differ from disk.
	for _, ce := range container.Configs {
		basename := filepath.Base(ce.TargetVolumePath)
		changed, err := cfg.IsChanged(container.Name, basename, ce.Content)
		if err != nil || changed {
			return true
		}
	}
	// Check if there are config files on disk that are no longer in the api.
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
