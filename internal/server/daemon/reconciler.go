// Package daemon implements the syslet reconciliation daemon, which
// continuously drives the system toward the desired state stored in SQLite.
// The top-level [Daemon] owns the reconciliation schedule, the store, and the
// public API, while the [Reconciler] is a stateless engine that computes and
// applies changes against systemd.
package daemon

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	pb "codeberg.org/xchangeee/syslet/proto"

	"codeberg.org/xchangeee/syslet/internal/server/systemd"
)

// Reconciler performs two-phase reconciliation.
type Reconciler struct {
	systemd *systemd.Client
	config  *ConfigFileManager
	logger  *slog.Logger
}

// NewReconciler creates a new reconciler.
func NewReconciler(sd *systemd.Client, cfg *ConfigFileManager, logger *slog.Logger) *Reconciler {
	return &Reconciler{
		systemd: sd,
		config:  cfg,
		logger:  logger,
	}
}

// Diff computes a ChangePlan for all provided units.
func (r *Reconciler) Diff(ctx context.Context, units []ResolvedUnit) (*ChangePlan, error) {
	plan := &ChangePlan{}

	for _, ru := range units {
		change, err := r.diffUnit(ctx, ru)
		if err != nil {
			return nil, fmt.Errorf("diffing %s: %w", ru.FullName(), err)
		}
		if change.UnitChanged {
			plan.NeedReload = true
		}
		plan.Changes = append(plan.Changes, change)
	}

	return plan, nil
}

func (r *Reconciler) diffUnit(ctx context.Context, ru ResolvedUnit) (*UnitChange, error) {
	fullName := ru.FullName()
	unitType := ru.Spec.Type
	change := &UnitChange{
		ResolvedUnit: ru,
	}

	// Check if unit file exists
	installed, err := r.unitFileExists(fullName, unitType)
	if err != nil {
		return nil, err
	}

	if !installed {
		change.IsNew = true
		change.UnitChanged = true
		change.ConfigChanged = len(ru.Configs) > 0

		if unitType.IsImmutable() {
			// New immutable unit: just create it
			return change, nil
		}

		if ru.Spec.DesiredState == pb.DesiredState_DESIRED_STATE_RUNNING && unitType.IsStartable() {
			change.NeedsStart = true
		}
		return change, nil
	}

	// Unit exists - check for changes
	if unitType.IsImmutable() {
		unitChanged, err := r.unitFileChanged(&ru)
		if err != nil {
			return nil, err
		}
		if unitChanged {
			change.Rejected = true
			change.RejectReason = fmt.Sprintf("immutable %s unit %s cannot be modified after creation", unitType.ShortName(), fullName)
			return change, nil
		}
		return change, nil
	}

	// Check unit file changes
	unitChanged, err := r.unitFileChanged(&ru)
	if err != nil {
		return nil, err
	}
	change.UnitChanged = unitChanged

	// Check config file changes
	for _, cf := range ru.Configs {
		changed, err := r.config.IsChanged(fullName, cf.Filename, cf.Content)
		if err != nil {
			return nil, err
		}
		if changed {
			change.ConfigChanged = true
			break
		}
	}

	// If unit or config changed, need to stop first (using old config)
	if (change.UnitChanged || change.ConfigChanged) && unitType.IsStartable() {
		state, err := r.systemd.Container().RuntimeState(ctx, fullName)
		if err != nil {
			return nil, err
		}
		if state.ActiveState == "active" || state.ActiveState == "activating" {
			change.NeedsStop = true
		}
	}

	// Determine start needs
	if unitType.IsStartable() {
		state, err := r.systemd.Container().RuntimeState(ctx, fullName)
		if err != nil {
			return nil, err
		}

		switch ru.Spec.DesiredState {
		case pb.DesiredState_DESIRED_STATE_RUNNING:
			if state.ActiveState != "active" || change.NeedsStop {
				change.NeedsStart = true
			}
		case pb.DesiredState_DESIRED_STATE_STOPPED:
			if state.ActiveState == "active" || state.ActiveState == "activating" {
				change.NeedsStop = true
			}
		}
	}

	return change, nil
}

func (r *Reconciler) unitFileChanged(ru *ResolvedUnit) (bool, error) {
	existing, err := r.readInstalledUnit(ru.FullName(), ru.Spec.Type)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}

	newContent, err := io.ReadAll(ru.SystemdContent())
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
		fullName := c.FullName()
		if c.Rejected {
			r.logger.Error("rejected change", "unit", fullName, "reason", c.RejectReason)
			results = append(results, UnitResult{
				FullName: fullName,
				Type:     c.Spec.Type,
				Changed:  false,
				Message:  c.RejectReason,
				Error:    true,
			})
			continue
		}
		if c.NeedsStop {
			r.logger.Info("stopping unit", "unit", fullName)
			if err := r.systemd.Container().Stop(ctx, fullName); err != nil {
				r.logger.Error("failed to stop unit", "unit", fullName, "error", err)
				results = append(results, UnitResult{
					FullName: fullName,
					Type:     c.Spec.Type,
					Changed:  false,
					Message:  fmt.Sprintf("failed to stop: %v", err),
					Error:    true,
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
		fullName := c.FullName()
		for _, cf := range c.Configs {
			r.logger.Info("writing config", "unit", fullName, "file", cf.Filename)
			if err := r.config.Write(fullName, cf.Filename, cf.Content); err != nil {
				r.logger.Error("failed to write config", "unit", fullName, "file", cf.Filename, "error", err)
			}
		}
	}

	// Phase 3: Write unit files
	for _, c := range plan.Changes {
		if c.Rejected || !c.UnitChanged {
			continue
		}
		fullName := c.FullName()
		r.logger.Info("installing unit file", "unit", fullName)
		if err := r.installUnitFile(&c.ResolvedUnit); err != nil {
			r.logger.Error("failed to install unit file", "unit", fullName, "error", err)
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
		fullName := c.FullName()
		if c.NeedsStart {
			r.logger.Info("starting unit", "unit", fullName)
			if err := r.systemd.Container().Start(ctx, fullName); err != nil {
				r.logger.Error("failed to start unit", "unit", fullName, "error", err)
				results = append(results, UnitResult{
					FullName: fullName,
					Type:     c.Spec.Type,
					Changed:  true,
					Message:  fmt.Sprintf("failed to start: %v", err),
					Error:    true,
				})
				continue
			}
		}

		// Build result for non-rejected, non-errored units
		if !c.Rejected {
			msg := r.buildResultMessage(c)
			results = append(results, UnitResult{
				FullName: fullName,
				Type:     c.Spec.Type,
				Changed:  c.UnitChanged || c.ConfigChanged || c.NeedsStart || c.NeedsStop,
				Message:  msg,
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

func (r *Reconciler) unitFileExists(fullName string, unitType pb.UnitType) (bool, error) {
	switch unitType {
	case pb.UnitType_UNIT_TYPE_CONTAINER:
		return r.systemd.Container().UnitFileExists(fullName)
	case pb.UnitType_UNIT_TYPE_VOLUME:
		return r.systemd.Volume().UnitFileExists(fullName)
	case pb.UnitType_UNIT_TYPE_NETWORK:
		return r.systemd.Network().UnitFileExists(fullName)
	default:
		return false, fmt.Errorf("unsupported unit type: %v", unitType)
	}
}

func (r *Reconciler) readInstalledUnit(fullName string, unitType pb.UnitType) ([]byte, error) {
	switch unitType {
	case pb.UnitType_UNIT_TYPE_CONTAINER:
		return r.systemd.Container().ReadInstalledUnit(fullName)
	case pb.UnitType_UNIT_TYPE_VOLUME:
		return r.systemd.Volume().ReadInstalledUnit(fullName)
	case pb.UnitType_UNIT_TYPE_NETWORK:
		return r.systemd.Network().ReadInstalledUnit(fullName)
	default:
		return nil, fmt.Errorf("unsupported unit type: %v", fullName)
	}
}

func (r *Reconciler) installUnitFile(ru *ResolvedUnit) error {
	fullName := ru.FullName()
	switch ru.Spec.Type {
	case pb.UnitType_UNIT_TYPE_CONTAINER:
		return r.systemd.Container().InstallUnitFile(fullName, ru.SystemdContent())
	case pb.UnitType_UNIT_TYPE_VOLUME:
		return r.systemd.Volume().InstallUnitFile(fullName, ru.SystemdContent())
	case pb.UnitType_UNIT_TYPE_NETWORK:
		return r.systemd.Network().InstallUnitFile(fullName, ru.SystemdContent())
	default:
		return fmt.Errorf("unsupported unit type: %v", ru.Spec.Type)
	}
}
