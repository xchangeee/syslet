package syslet

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"codeberg.org/xchangeee/syslet/internal/systemd"
	"github.com/spf13/afero"
)

// change captures the diff for a single spec against the installed state.
type change struct {
	spec       Spec
	fullName   string // e.g. "webapp.container"
	newContent string // rendered unit file content

	isNew         bool
	unitChanged   bool
	configChanged bool
	needsStop     bool
	needsStart    bool
	prune         bool // installed on disk but not in specs

	errored bool
	message string
}

// Apply reads specs from a zip file, validates them, diffs against the
// installed state, and executes changes in coordinated order:
//  1. Stop containers that changed or are being pruned
//  2. Write config files
//  3. Write unit files
//  4. Remove pruned unit files and config dirs
//  5. Single daemon-reload
//  6. Start containers that should be running
func Apply(ctx context.Context, logger *slog.Logger, fs afero.Fs, sd *systemd.Client, zipPath string) error {
	specs, err := LoadSpecsFromZip(zipPath)
	if err != nil {
		return err
	}
	if err := ValidateSpecs(specs); err != nil {
		return fmt.Errorf("validation: %w", err)
	}

	cfg := NewConfigFileManager(fs)
	containerConfigDir := DefaultContainerConfigDir

	// Render all specs to unit file content.
	type rendered struct {
		spec    Spec
		content string
	}
	var containers, volumes, networks []rendered
	specNames := make(map[string]bool)

	for _, s := range specs {
		content, err := renderUnitContent(s, containerConfigDir)
		if err != nil {
			return fmt.Errorf("rendering %s: %w", s.Name, err)
		}
		r := rendered{spec: s, content: content}
		switch strings.ToLower(s.Type) {
		case "container":
			containers = append(containers, r)
		case "volume":
			volumes = append(volumes, r)
		case "network":
			networks = append(networks, r)
		}
		specNames[fullUnitName(s)] = true
	}

	// Compute changes for all spec types.
	var changes []*change

	// Volumes and networks: simple unit file diff (no configs, no start/stop).
	for _, r := range volumes {
		c := diffSimple(sd, r.spec, r.content)
		changes = append(changes, c)
	}
	for _, r := range networks {
		c := diffSimple(sd, r.spec, r.content)
		changes = append(changes, c)
	}

	// Containers: unit file diff + config diff + start/stop logic.
	for _, r := range containers {
		c, err := diffContainer(ctx, sd, cfg, r.spec, r.content)
		if err != nil {
			return fmt.Errorf("diffing container %s: %w", r.spec.Name, err)
		}
		changes = append(changes, c)
	}

	// Find installed files that are NOT in the current spec set (stale → prune).
	pruneChanges, err := findStaleUnits(ctx, sd, specNames)
	if err != nil {
		return fmt.Errorf("finding stale units: %w", err)
	}
	changes = append(changes, pruneChanges...)

	// Execute in strict global order.

	// 1. Stop containers that need stopping (changed, being pruned, or desired stopped).
	for _, c := range changes {
		if !c.needsStop {
			continue
		}
		logger.Info("stopping", "unit", c.fullName)
		if err := sd.StopContainer(ctx, c.fullName); err != nil {
			logger.Error("failed to stop", "unit", c.fullName, "error", err)
			c.errored = true
			c.message = fmt.Sprintf("failed to stop: %v", err)
		}
	}

	// 2. Write config files for changed containers.
	for _, c := range changes {
		if c.errored || c.prune || !c.configChanged {
			continue
		}
		for _, cfgEntry := range c.spec.Configs {
			basename := filepath.Base(cfgEntry.TargetVolumePath)
			logger.Info("writing config", "container", c.spec.Name, "file", basename)
			if err := cfg.Write(c.spec.Name, basename, cfgEntry.Content); err != nil {
				logger.Error("failed to write config", "container", c.spec.Name, "file", basename, "error", err)
			}
		}
		// Remove config files that are on disk but no longer in the spec.
		pruneStaleConfigs(logger, cfg, c.spec)
	}

	// 3. Write unit files for changed specs.
	needReload := false
	for _, c := range changes {
		if c.errored || c.prune || !c.unitChanged {
			continue
		}
		logger.Info("writing unit file", "unit", c.fullName)
		if err := sd.WriteUnitFile(c.fullName, []byte(c.newContent)); err != nil {
			logger.Error("failed to write unit file", "unit", c.fullName, "error", err)
			c.errored = true
			c.message = fmt.Sprintf("failed to write unit file: %v", err)
			continue
		}
		needReload = true
	}

	// 4. Remove pruned unit files and their config dirs.
	for _, c := range changes {
		if !c.prune || c.errored {
			continue
		}
		logger.Info("removing stale unit", "unit", c.fullName)
		if err := sd.RemoveUnitFile(c.fullName); err != nil {
			logger.Error("failed to remove unit file", "unit", c.fullName, "error", err)
		} else {
			needReload = true
		}
		// Remove config directory if this was a container.
		if strings.HasSuffix(c.fullName, ".container") {
			name := strings.TrimSuffix(c.fullName, ".container")
			if err := cfg.RemoveAll(name); err != nil {
				logger.Error("failed to remove config dir", "container", name, "error", err)
			}
		}
	}

	// 5. Single daemon-reload.
	if needReload {
		logger.Info("daemon-reload")
		if err := sd.DaemonReload(ctx); err != nil {
			logger.Error("daemon-reload failed", "error", err)
		}
	}

	// 6. Start containers that need starting.
	for _, c := range changes {
		if c.errored || !c.needsStart {
			continue
		}
		logger.Info("starting", "unit", c.fullName)
		if err := sd.StartContainer(ctx, c.fullName); err != nil {
			logger.Error("failed to start", "unit", c.fullName, "error", err)
			c.errored = true
			c.message = fmt.Sprintf("failed to start: %v", err)
		}
	}

	// Print summary.
	for _, c := range changes {
		msg := c.message
		if !c.errored && msg == "" {
			msg = summarize(c)
		}
		status := "unchanged"
		if c.unitChanged || c.configChanged || c.needsStart || c.needsStop || c.prune {
			status = "changed"
		}
		fmt.Printf("%-40s %-10s %s\n", c.fullName, status, msg)
	}

	// Return error if any changes had errors.
	for _, c := range changes {
		if c.errored {
			return fmt.Errorf("one or more units failed to apply")
		}
	}
	return nil
}

// diffSimple computes the diff for a volume or network spec.
// These are write-only: install the unit file if new or changed.
func diffSimple(sd *systemd.Client, s Spec, newContent string) *change {
	fn := fullUnitName(s)
	c := &change{
		spec:       s,
		fullName:   fn,
		newContent: newContent,
	}

	if !sd.UnitFileExists(fn) {
		c.isNew = true
		c.unitChanged = true
		return c
	}

	existing, err := sd.ReadUnitFile(fn)
	if err != nil {
		c.errored = true
		c.message = fmt.Sprintf("reading installed unit: %v", err)
		return c
	}
	if sha256hex([]byte(newContent)) != sha256hex(existing) {
		c.unitChanged = true
	}
	return c
}

// diffContainer computes the diff for a container spec, including config
// changes and start/stop decisions based on desired state and runtime state.
func diffContainer(ctx context.Context, sd *systemd.Client, cfg *ConfigFileManager, s Spec, newContent string) (*change, error) {
	fn := fullUnitName(s)
	c := &change{
		spec:       s,
		fullName:   fn,
		newContent: newContent,
	}

	if !sd.UnitFileExists(fn) {
		c.isNew = true
		c.unitChanged = true
		c.configChanged = len(s.Configs) > 0
		if strings.ToLower(s.DesiredState) == "running" {
			c.needsStart = true
		}
		return c, nil
	}

	// Check unit file changes.
	existing, err := sd.ReadUnitFile(fn)
	if os.IsNotExist(err) {
		c.isNew = true
		c.unitChanged = true
	} else if err != nil {
		return nil, err
	} else if sha256hex([]byte(newContent)) != sha256hex(existing) {
		c.unitChanged = true
	}

	// Check config changes.
	c.configChanged = configsChanged(cfg, s)

	// Query runtime state.
	state, err := sd.ContainerState(ctx, fn)
	if err != nil {
		return nil, fmt.Errorf("querying state of %s: %w", fn, err)
	}

	// If unit or config changed and currently running, need to stop first.
	isRunning := state.ActiveState == "active" || state.ActiveState == "activating"
	if (c.unitChanged || c.configChanged) && isRunning {
		c.needsStop = true
	}

	switch strings.ToLower(s.DesiredState) {
	case "running":
		if !isRunning || c.needsStop {
			c.needsStart = true
		}
	case "stopped":
		if isRunning {
			c.needsStop = true
		}
	}

	return c, nil
}

// findStaleUnits scans /etc/containers/systemd/ for unit files that are not
// in the current spec set. Returns changes marked for pruning.
func findStaleUnits(ctx context.Context, sd *systemd.Client, specNames map[string]bool) ([]*change, error) {
	var stale []*change
	for _, ext := range []string{".container", ".volume", ".network"} {
		files, err := sd.ListUnitFiles(ext)
		if err != nil {
			return nil, err
		}
		for _, fn := range files {
			if specNames[fn] {
				continue
			}
			c := &change{
				fullName: fn,
				prune:    true,
			}
			// If it's a container, check if it's running so we stop it first.
			if ext == ".container" {
				state, err := sd.ContainerState(ctx, fn)
				if err == nil && (state.ActiveState == "active" || state.ActiveState == "activating") {
					c.needsStop = true
				}
			}
			stale = append(stale, c)
		}
	}
	return stale, nil
}

// configsChanged checks if any config file in the spec differs from what's deployed.
func configsChanged(cfg *ConfigFileManager, s Spec) bool {
	// Check if any spec configs differ from disk.
	for _, ce := range s.Configs {
		basename := filepath.Base(ce.TargetVolumePath)
		changed, err := cfg.IsChanged(s.Name, basename, ce.Content)
		if err != nil || changed {
			return true
		}
	}
	// Check if there are config files on disk that are no longer in the spec.
	deployed, _ := cfg.ListFiles(s.Name)
	specFiles := make(map[string]bool)
	for _, ce := range s.Configs {
		specFiles[filepath.Base(ce.TargetVolumePath)] = true
	}
	for _, f := range deployed {
		if !specFiles[f] {
			return true
		}
	}
	return false
}

// pruneStaleConfigs removes config files that are on disk but no longer
// in the container's spec.
func pruneStaleConfigs(logger *slog.Logger, cfg *ConfigFileManager, s Spec) {
	deployed, err := cfg.ListFiles(s.Name)
	if err != nil {
		return
	}
	specFiles := make(map[string]bool)
	for _, ce := range s.Configs {
		specFiles[filepath.Base(ce.TargetVolumePath)] = true
	}
	for _, f := range deployed {
		if !specFiles[f] {
			logger.Info("removing stale config", "container", s.Name, "file", f)
			if err := cfg.RemoveFile(s.Name, f); err != nil {
				logger.Error("failed to remove stale config", "container", s.Name, "file", f, "error", err)
			}
		}
	}
	// If no configs remain, remove the entire directory.
	if len(s.Configs) == 0 {
		cfg.RemoveAll(s.Name)
	}
}

// fullUnitName returns the quadlet file name for a spec (e.g. "webapp.container").
func fullUnitName(s Spec) string {
	return s.Name + "." + strings.ToLower(s.Type)
}

// summarize builds a human-readable message for a change.
func summarize(c *change) string {
	if c.prune {
		return "removed"
	}
	var parts []string
	if c.isNew {
		parts = append(parts, "created")
	}
	if c.unitChanged && !c.isNew {
		parts = append(parts, "unit updated")
	}
	if c.configChanged {
		parts = append(parts, "config updated")
	}
	if c.needsStop && c.needsStart {
		parts = append(parts, "restarted")
	} else if c.needsStart {
		parts = append(parts, "started")
	} else if c.needsStop {
		parts = append(parts, "stopped")
	}
	if len(parts) == 0 {
		return "up to date"
	}
	return strings.Join(parts, ", ")
}
