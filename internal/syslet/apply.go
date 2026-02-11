package syslet

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"codeberg.org/xchangeee/syslet/internal/api"
	"codeberg.org/xchangeee/syslet/internal/containerconfig"
	"codeberg.org/xchangeee/syslet/internal/podman"
	"codeberg.org/xchangeee/syslet/internal/systemd"

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
	oldContent    string // empty if new file
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
	fullName   string
	content    string
	oldContent string // empty if new file
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

	// set during diff if daemon-reload is needed
	NeedsReload bool

	// final summary for reporting
	Results []ApplyResult
}

// BuildPlan reads specs from a zip file or directory, validates them, and builds
// an execution plan by diffing against the installed state.
// Returns the plan without executing it.
func BuildPlan(ctx context.Context, fs afero.Fs, sd *systemd.Client, path string) (*ApplyPlan, error) {
	specs, err := api.LoadSpecsFS(fs, path)
	if err != nil {
		return nil, err
	}

	// Pre-render validation: check raw input data.
	if err := api.ValidateSpecs(specs); err != nil {
		return nil, fmt.Errorf("pre-render validation: %w", err)
	}

	cfg := containerconfig.NewConfigFileManager(fs)

	// Render all specs to intermediate unit options.
	var containers, volumes, networks []api.RenderedUnit
	specNames := make(map[string]bool)

	for _, s := range specs {
		var r api.RenderedUnit
		var err error

		switch s.GetType() {
		case api.SpecTypeContainer:
			r, err = s.(*api.ContainerSpec).Render(cfg.BaseDirectory())
			if err != nil {
				return nil, fmt.Errorf("rendering %s: %w", s.GetName(), err)
			}
			containers = append(containers, r)
		case api.SpecTypeVolume:
			r, err = s.(*api.VolumeSpec).Render()
			if err != nil {
				return nil, fmt.Errorf("rendering %s: %w", s.GetName(), err)
			}
			volumes = append(volumes, r)
		case api.SpecTypeNetwork:
			r, err = s.(*api.NetworkSpec).Render()
			if err != nil {
				return nil, fmt.Errorf("rendering %s: %w", s.GetName(), err)
			}
			networks = append(networks, r)
		}
		specNames[s.FullUnitName()] = true
	}

	// Post-render validation: check rendered units and cross-references.
	if err := api.ValidateRenderedUnits(containers, volumes, networks); err != nil {
		return nil, fmt.Errorf("post-render validation: %w", err)
	}

	// Serialize unit options to strings after validation passes.
	for i := range containers {
		content, err := containers[i].SerializeUnitOptions()
		if err != nil {
			return nil, fmt.Errorf("serializing %s: %w", containers[i].Spec.GetName(), err)
		}
		containers[i].Content = content
	}
	for i := range volumes {
		content, err := volumes[i].SerializeUnitOptions()
		if err != nil {
			return nil, fmt.Errorf("serializing %s: %w", volumes[i].Spec.GetName(), err)
		}
		volumes[i].Content = content
	}
	for i := range networks {
		content, err := networks[i].SerializeUnitOptions()
		if err != nil {
			return nil, fmt.Errorf("serializing %s: %w", networks[i].Spec.GetName(), err)
		}
		networks[i].Content = content
	}

	// Build the apply plan by diffing all specs.
	plan := &ApplyPlan{}

	// Diff containers (handles config files, unit files, start/stop).
	for _, r := range containers {
		if err := diffContainer(ctx, sd, cfg, plan, r); err != nil {
			return nil, fmt.Errorf("diffing container %s: %w", r.Spec.GetName(), err)
		}
	}

	// Diff volumes and networks (unit files only).
	for _, r := range volumes {
		diffSimple(sd, plan, r)
	}
	for _, r := range networks {
		diffSimple(sd, plan, r)
	}

	// Find installed files that are NOT in the current spec set (stale → prune).
	if err := findStaleUnits(ctx, sd, plan, specNames); err != nil {
		return nil, fmt.Errorf("finding stale units: %w", err)
	}

	return plan, nil
}

// Apply executes a pre-built plan in coordinated order:
//  1. Stop containers that changed or are being pruned
//  2. Write config files
//  3. Write unit files
//  4. Remove pruned unit files and config dirs
//  5. Delete podman volumes and networks (if ReclaimPolicy is "Delete")
//  6. Single daemon-reload
//  7. Start containers that should be running
func Apply(ctx context.Context, logger *slog.Logger, fs afero.Fs, sd *systemd.Client, pc podman.Interface, plan *ApplyPlan) error {
	cfg := containerconfig.NewConfigFileManager(fs)

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
		if err := pc.DeleteVolume(ctx, op.name); err != nil {
			logger.Error("failed to delete volume", "name", op.name, "error", err)
		}
	}
	for _, op := range plan.DeleteNetworks {
		logger.Info("deleting podman network", "name", op.name)
		if err := pc.DeleteNetwork(ctx, op.name); err != nil {
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

// Diff displays what would change based on a pre-built plan
// without actually applying the changes.
func Diff(plan *ApplyPlan) {
	DisplayDiff(plan)
}

// displayContentDiff prints a unified diff between old and new content.
func displayContentDiff(name, oldContent, newContent string) {
	oldLines := strings.Split(oldContent, "\n")
	newLines := strings.Split(newContent, "\n")

	fmt.Printf("\n--- %s (current)\n", name)
	fmt.Printf("+++ %s (new)\n", name)

	// Simple line-by-line comparison
	maxLen := len(oldLines)
	if len(newLines) > maxLen {
		maxLen = len(newLines)
	}

	for i := 0; i < maxLen; i++ {
		var oldLine, newLine string
		if i < len(oldLines) {
			oldLine = oldLines[i]
		}
		if i < len(newLines) {
			newLine = newLines[i]
		}

		if oldLine != newLine {
			if oldLine != "" {
				fmt.Printf("-%s\n", oldLine)
			}
			if newLine != "" {
				fmt.Printf("+%s\n", newLine)
			}
		}
	}
}

// DisplayDiff prints a human-readable diff showing what would change.
// It shows operations that would be performed and the final status of each unit.
func DisplayDiff(plan *ApplyPlan) {
	// Print operations that would be performed
	hasChanges := false

	if len(plan.StopContainers) > 0 {
		hasChanges = true
		fmt.Println("\nContainers to stop:")
		for _, op := range plan.StopContainers {
			fmt.Printf("  - %s\n", op.fullName)
		}
	}

	if len(plan.WriteConfigs) > 0 {
		hasChanges = true
		fmt.Println("\nConfig file changes:")
		for _, op := range plan.WriteConfigs {
			if op.oldContent == "" {
				fmt.Printf("  - %s/%s (new file)\n", op.containerName, op.filename)
			} else if op.oldContent != op.content {
				fmt.Printf("  - %s/%s (modified)\n", op.containerName, op.filename)
				displayContentDiff(fmt.Sprintf("%s/%s", op.containerName, op.filename), op.oldContent, op.content)
			}
		}
	}

	if len(plan.DeleteConfigs) > 0 {
		hasChanges = true
		fmt.Println("\nConfig files to delete:")
		for _, op := range plan.DeleteConfigs {
			fmt.Printf("  - %s/%s\n", op.containerName, op.filename)
		}
	}

	if len(plan.DeleteConfigDirs) > 0 {
		hasChanges = true
		fmt.Println("\nConfig directories to delete:")
		for _, op := range plan.DeleteConfigDirs {
			fmt.Printf("  - %s/\n", op.containerName)
		}
	}

	if len(plan.WriteUnits) > 0 {
		hasChanges = true
		fmt.Println("\nUnit file changes:")
		for _, op := range plan.WriteUnits {
			if op.oldContent == "" {
				fmt.Printf("  - %s (new file)\n", op.fullName)
			} else if op.oldContent != op.content {
				fmt.Printf("  - %s (modified)\n", op.fullName)
				displayContentDiff(op.fullName, op.oldContent, op.content)
			}
		}
	}

	if len(plan.DeleteUnits) > 0 {
		hasChanges = true
		fmt.Println("\nUnit files to delete:")
		for _, op := range plan.DeleteUnits {
			fmt.Printf("  - %s\n", op.fullName)
		}
	}

	if len(plan.DeleteVolumes) > 0 {
		hasChanges = true
		fmt.Println("\nPodman volumes to delete:")
		for _, op := range plan.DeleteVolumes {
			fmt.Printf("  - %s\n", op.name)
		}
	}

	if len(plan.DeleteNetworks) > 0 {
		hasChanges = true
		fmt.Println("\nPodman networks to delete:")
		for _, op := range plan.DeleteNetworks {
			fmt.Printf("  - %s\n", op.name)
		}
	}

	if plan.NeedsReload {
		hasChanges = true
		fmt.Println("\nSystemd daemon-reload: required")
	}

	if len(plan.StartContainers) > 0 {
		hasChanges = true
		fmt.Println("\nContainers to start:")
		for _, op := range plan.StartContainers {
			fmt.Printf("  - %s\n", op.fullName)
		}
	}

	// Print summary of all units
	fmt.Println("\nSummary:")
	fmt.Printf("%-40s %-10s %s\n", "UNIT", "STATUS", "CHANGES")
	for _, result := range plan.Results {
		fmt.Printf("%-40s %-10s %s\n", result.fullName, result.status, result.message)
	}

	if !hasChanges {
		fmt.Println("\nNo changes detected. All units are up to date.")
	}
}
