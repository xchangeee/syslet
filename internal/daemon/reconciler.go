// Package daemon implements the rsystemd reconciliation daemon.
package daemon

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"codeberg.org/xchangeee/rsystemd/internal/config"
	"codeberg.org/xchangeee/rsystemd/internal/parser"
	"codeberg.org/xchangeee/rsystemd/internal/systemd"
)

// UnitAction describes what needs to happen to a single unit.
type UnitAction int

const (
	ActionNone        UnitAction = iota
	ActionStop                   // stop before updating
	ActionWriteConfig            // write config files
	ActionWriteUnit              // write unit file
	ActionStart                  // start the unit
	ActionReject                 // immutable unit changed, reject
)

// UnitChange captures the diff for a single unit.
type UnitChange struct {
	Unit          *parser.ParsedUnit
	ConfigFiles   []config.ConfigFile // configs to write
	UnitChanged   bool
	ConfigChanged bool
	NeedsStop     bool // stop before update (unit or config changed)
	NeedsStart    bool // start after update
	IsNew         bool // unit doesn't exist yet
	Rejected      bool // immutable unit changed
	RejectReason  string
}

// ChangePlan is the result of diffing all units.
type ChangePlan struct {
	Changes    []*UnitChange
	NeedReload bool // at least one unit file changed
}

// Reconciler performs two-phase reconciliation.
type Reconciler struct {
	systemd *systemd.Client
	config  *config.Manager
	logger  *slog.Logger
}

// NewReconciler creates a new reconciler.
func NewReconciler(sd *systemd.Client, cfg *config.Manager, logger *slog.Logger) *Reconciler {
	return &Reconciler{
		systemd: sd,
		config:  cfg,
		logger:  logger,
	}
}

// Diff computes a ChangePlan for all provided units and their configs.
func (r *Reconciler) Diff(ctx context.Context, units []*parser.ParsedUnit, configs map[string][]config.ConfigFile) (*ChangePlan, error) {
	plan := &ChangePlan{}

	for _, u := range units {
		change, err := r.diffUnit(ctx, u, configs[parser.UnitBaseName(u.Name)])
		if err != nil {
			return nil, fmt.Errorf("diffing %s: %w", u.Name, err)
		}
		if change.UnitChanged {
			plan.NeedReload = true
		}
		plan.Changes = append(plan.Changes, change)
	}

	return plan, nil
}

func (r *Reconciler) diffUnit(ctx context.Context, u *parser.ParsedUnit, cfgFiles []config.ConfigFile) (*UnitChange, error) {
	change := &UnitChange{
		Unit:        u,
		ConfigFiles: cfgFiles,
	}

	// Check if unit file exists
	installed := r.systemd.UnitFileExists(u.Name, u.Type)

	if !installed {
		change.IsNew = true
		change.UnitChanged = true
		change.ConfigChanged = len(cfgFiles) > 0

		if u.Type.IsImmutable() {
			// New immutable unit: just create it
			return change, nil
		}

		if u.DesiredState == parser.DesiredStateRunning && u.Type.IsStartable() {
			change.NeedsStart = true
		}
		return change, nil
	}

	// Unit exists - check for changes
	if u.Type.IsImmutable() {
		// Check if immutable unit changed
		unitChanged, err := r.unitFileChanged(u)
		if err != nil {
			return nil, err
		}
		if unitChanged {
			change.Rejected = true
			change.RejectReason = fmt.Sprintf("immutable %s unit %s cannot be modified after creation", u.Type, u.Name)
			return change, nil
		}
		return change, nil
	}

	// Check unit file changes
	unitChanged, err := r.unitFileChanged(u)
	if err != nil {
		return nil, err
	}
	change.UnitChanged = unitChanged

	// Check config file changes
	for _, cf := range cfgFiles {
		changed, err := r.config.Changed(u.Name, u.Type, cf.Filename, cf.Content)
		if err != nil {
			return nil, err
		}
		if changed {
			change.ConfigChanged = true
			break
		}
	}

	// If unit or config changed, need to stop first (using old config)
	if (change.UnitChanged || change.ConfigChanged) && u.Type.IsStartable() {
		// Check if currently running
		state, err := r.systemd.GetUnitState(ctx, u.Name, u.Type)
		if err != nil {
			return nil, err
		}
		if state.ActiveState == "active" || state.ActiveState == "activating" {
			change.NeedsStop = true
		}
	}

	// Determine start needs
	if u.Type.IsStartable() {
		state, err := r.systemd.GetUnitState(ctx, u.Name, u.Type)
		if err != nil {
			return nil, err
		}

		switch u.DesiredState {
		case parser.DesiredStateRunning:
			if state.ActiveState != "active" || change.NeedsStop {
				change.NeedsStart = true
			}
		case parser.DesiredStateStopped:
			if state.ActiveState == "active" || state.ActiveState == "activating" {
				change.NeedsStop = true
			}
		}
	}

	return change, nil
}

func (r *Reconciler) unitFileChanged(u *parser.ParsedUnit) (bool, error) {
	existing, err := r.systemd.ReadInstalledUnit(u.Name, u.Type)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}

	// Compare the systemd-installable content (without [X-Rsystemd])
	newContent, err := io.ReadAll(u.SystemdContent())
	if err != nil {
		return false, err
	}

	return sha256sum(newContent) != sha256sum(existing), nil
}

// Execute applies a ChangePlan in the strict global order.
func (r *Reconciler) Execute(ctx context.Context, plan *ChangePlan) []UnitResult {
	var results []UnitResult

	// Phase 1: Stop all units that need stopping (uses OLD config)
	for _, c := range plan.Changes {
		if c.Rejected {
			r.logger.Error("rejected change", "unit", c.Unit.Name, "reason", c.RejectReason)
			results = append(results, UnitResult{
				Name:    c.Unit.Name,
				Changed: false,
				Message: c.RejectReason,
				Error:   true,
			})
			continue
		}
		if c.NeedsStop {
			r.logger.Info("stopping unit", "unit", c.Unit.Name)
			if err := r.systemd.StopUnit(ctx, c.Unit.Name, c.Unit.Type); err != nil {
				r.logger.Error("failed to stop unit", "unit", c.Unit.Name, "error", err)
				results = append(results, UnitResult{
					Name:    c.Unit.Name,
					Changed: false,
					Message: fmt.Sprintf("failed to stop: %v", err),
					Error:   true,
				})
				continue
			}
		}
	}

	// Phase 2: Write config files
	for _, c := range plan.Changes {
		if c.Rejected || !c.ConfigChanged {
			continue
		}
		for _, cf := range c.ConfigFiles {
			r.logger.Info("writing config", "unit", c.Unit.Name, "file", cf.Filename)
			if err := r.config.Write(c.Unit.Name, c.Unit.Type, cf.Filename, cf.Content); err != nil {
				r.logger.Error("failed to write config", "unit", c.Unit.Name, "file", cf.Filename, "error", err)
			}
		}
	}

	// Phase 3: Write unit files
	for _, c := range plan.Changes {
		if c.Rejected || !c.UnitChanged {
			continue
		}
		r.logger.Info("installing unit file", "unit", c.Unit.Name)
		if err := r.systemd.InstallUnitFile(c.Unit.Name, c.Unit.Type, c.Unit.SystemdContent()); err != nil {
			r.logger.Error("failed to install unit file", "unit", c.Unit.Name, "error", err)
		}
	}

	// Phase 4: Single daemon-reload if any unit files changed
	if plan.NeedReload {
		r.logger.Info("daemon-reload")
		if err := r.systemd.DaemonReload(ctx); err != nil {
			r.logger.Error("daemon-reload failed", "error", err)
		}
	}

	// Phase 5: Start all units that need starting
	for _, c := range plan.Changes {
		if c.Rejected {
			continue
		}
		if c.NeedsStart {
			r.logger.Info("starting unit", "unit", c.Unit.Name)
			if err := r.systemd.StartUnit(ctx, c.Unit.Name, c.Unit.Type); err != nil {
				r.logger.Error("failed to start unit", "unit", c.Unit.Name, "error", err)
				results = append(results, UnitResult{
					Name:    c.Unit.Name,
					Changed: true,
					Message: fmt.Sprintf("failed to start: %v", err),
					Error:   true,
				})
				continue
			}
		}

		// Build result for non-rejected, non-errored units
		if !c.Rejected {
			msg := r.buildResultMessage(c)
			results = append(results, UnitResult{
				Name:    c.Unit.Name,
				Changed: c.UnitChanged || c.ConfigChanged || c.NeedsStart || c.NeedsStop,
				Message: msg,
			})
		}
	}

	return results
}

func (r *Reconciler) buildResultMessage(c *UnitChange) string {
	var parts []string
	if c.IsNew {
		parts = append(parts, "created")
	}
	if c.UnitChanged && !c.IsNew {
		parts = append(parts, "unit updated")
	}
	if c.ConfigChanged {
		parts = append(parts, "config updated")
	}
	if c.NeedsStop && c.NeedsStart {
		parts = append(parts, "restarted")
	} else if c.NeedsStart {
		parts = append(parts, "started")
	} else if c.NeedsStop {
		parts = append(parts, "stopped")
	}
	if len(parts) == 0 {
		return "up to date"
	}
	return strings.Join(parts, ", ")
}

// UnitResult is the outcome of reconciling one unit.
type UnitResult struct {
	Name    string
	Changed bool
	Message string
	Error   bool
}

// LoadUnitsFromDir reads and parses all unit files from a directory.
func LoadUnitsFromDir(dir string) ([]*parser.ParsedUnit, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var units []*parser.ParsedUnit
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if parser.UnitTypeFromExtension(e.Name()) == parser.UnitTypeUnknown {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", e.Name(), err)
		}
		u, err := parser.Parse(e.Name(), string(content))
		if err != nil {
			return nil, err
		}
		units = append(units, u)
	}
	return units, nil
}

// LoadConfigsFromDir reads config files from a configs directory.
// The directory structure is: <dir>/<unit-basename>/<filename>
func LoadConfigsFromDir(dir string) (map[string][]config.ConfigFile, error) {
	configs := make(map[string][]config.ConfigFile)

	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return configs, nil
	}
	if err != nil {
		return nil, err
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		unitName := e.Name()
		unitDir := filepath.Join(dir, unitName)
		files, err := os.ReadDir(unitDir)
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			content, err := os.ReadFile(filepath.Join(unitDir, f.Name()))
			if err != nil {
				return nil, err
			}
			configs[unitName] = append(configs[unitName], config.ConfigFile{
				UnitName: unitName,
				Filename: f.Name(),
				Content:  string(content),
			})
		}
	}

	return configs, nil
}

func sha256sum(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h[:])
}
