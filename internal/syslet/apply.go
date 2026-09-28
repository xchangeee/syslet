package syslet

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/xchangeee/syslet/internal/filestore"
	"github.com/xchangeee/syslet/internal/model"
	"github.com/xchangeee/syslet/internal/podman"
	"github.com/xchangeee/syslet/internal/systemd"
)

// ApplyReport is the result of an apply: the plan it executed and the units
// whose operations failed. Apply returns it and leaves the plan unchanged;
// DisplayReport prints it as the plan's outcomes with the failures in place.
type ApplyReport struct {
	plan *ApplyPlan
	// failures holds one entry per failed unit, in the order they failed.
	failures []UnitError
}

// recordError marks a unit as failed. A later failure of the same unit
// replaces the earlier one, as it did when failures overwrote the outcome.
func (r *ApplyReport) recordError(name model.FullUnitName, message string) {
	for i := range r.failures {
		if r.failures[i].Unit == name {
			r.failures[i].Message = message
			return
		}
	}
	r.failures = append(r.failures, UnitError{Unit: name, Message: message})
}

func (r *ApplyReport) failed() bool {
	return len(r.failures) > 0
}

// outcomes returns the plan's outcomes with every failed unit turned into a
// StatusError row carrying the failure message. A failed unit the plan has no
// outcome for gets a row of its own at the end, so no failure goes unreported.
func (r *ApplyReport) outcomes() []UnitOutcome {
	outcomes := slices.Clone(r.plan.Outcomes)
	for _, f := range r.failures {
		i := slices.IndexFunc(outcomes, func(o UnitOutcome) bool { return o.unit == f.Unit })
		failed := UnitOutcome{unit: f.Unit, status: StatusError, message: f.Message}
		if i < 0 {
			outcomes = append(outcomes, failed)
		} else {
			outcomes[i] = failed
		}
	}
	return outcomes
}

// Apply executes a plan from BuildPlan in coordinated order:
//  0. Pull container images missing from the local store; if any pull fails,
//     return before anything else changes
//  1. Stop containers that changed or are being pruned
//  2. Write config files
//  3. Write unit files
//  4. Remove pruned unit files and config dirs
//  5. Delete podman volumes and networks (if ReclaimPolicy is "Delete", or to
//     recreate a changed network)
//  6. Single daemon-reload
//  7. Start containers that should be running
//
// The report is returned even when Apply fails, so the caller can show which
// units failed.
func Apply(ctx context.Context, logger *slog.Logger, sd *systemd.Client, jr systemd.JournalReader, pc podman.Interface, mgrs filestore.FileManagers, plan *ApplyPlan) (*ApplyReport, error) {
	report := &ApplyReport{plan: plan}
	r := &applyRunner{logger: logger, report: report}

	// Execute in strict global order (4 phases).

	// Phase 0: Pull missing images while the old containers still run, so the
	// download doesn't count as downtime. A failed pull aborts the apply before
	// the host is touched: restarting a container whose image can't be pulled
	// would only repeat the failing pull inside the start, turning it into an
	// outage. Every pull is attempted first, so one run reports all failures.
	for _, op := range plan.PullPodmanImages {
		logger.Info("pulling image", "image", op.image)
		if err := pc.PullImage(ctx, op.image); err != nil {
			logger.Error("pulling image failed", "image", op.image, "error", err)
			for _, unit := range op.units {
				report.recordError(unit.FullName(), fmt.Sprintf("pulling image: %v", err))
			}
		}
	}
	if report.failed() {
		return report, fmt.Errorf("pulling images failed")
	}

	// Phase 1: Stop all services that need stopping (containers and network services).
	for _, op := range plan.StopSystemdServices {
		r.execUnit("stopping", op.ref.FullName(),
			func() error { return sd.StopUnit(ctx, op.ref.ServiceUnitName()) },
			"unit", op.ref.FullName())
	}

	// Phase 2: Write and delete config files.
	for _, op := range plan.WriteFsContainerConfigFiles {
		r.execUnit("writing config", op.container.FullName(),
			func() error { return mgrs.Config.WriteFile(op.container, op.mountPath, op.content, op.mode) },
			"container", op.container, "mountPath", op.mountPath)
	}
	for _, op := range plan.DeleteFsContainerConfigFiles {
		r.exec("removing stale config",
			func() error { return mgrs.Config.RemoveFile(op.container, op.internalFilename) },
			"container", op.container, "file", op.internalFilename)
	}
	for _, op := range plan.DeleteFsContainerConfigs {
		r.exec("removing config directory",
			func() error { return mgrs.Config.RemoveUnit(op.container) },
			"container", op.container)
	}

	// Phase 3a: Write and delete configDir groups.
	for _, op := range plan.WriteFsContainerConfigDirs {
		logger.Info("writing configDir", "container", op.container, "mountPath", op.mountPath, "version", op.version)
		writeErr := false
		for _, f := range op.files {
			if err := mgrs.Config.WriteVersionedDirFile(op.container, op.mountPath, op.version, f.Name, f.Mode, f.Content); err != nil {
				logger.Error("failed to write configDir file", "container", op.container, "mountPath", op.mountPath, "file", f.Name, "error", err)
				report.recordError(op.container.FullName(), fmt.Sprintf("failed to write configDir %s file %s: %v", op.mountPath, f.Name, err))
				writeErr = true
			}
		}
		if writeErr {
			continue
		}
		if err := mgrs.Config.UpdateDirSymlink(op.container, op.mountPath, op.version); err != nil {
			logger.Error("failed to update configDir symlink", "container", op.container, "mountPath", op.mountPath, "error", err)
			report.recordError(op.container.FullName(), fmt.Sprintf("failed to update configDir symlink %s: %v", op.mountPath, err))
			continue
		}
		if err := mgrs.Config.PruneOldVersions(op.container, op.mountPath, op.version); err != nil {
			logger.Error("failed to prune old configDir versions", "container", op.container, "mountPath", op.mountPath, "error", err)
		}
	}
	for _, op := range plan.DeleteFsContainerConfigDirs {
		r.exec("removing stale configDir group",
			func() error { return mgrs.Config.RemoveDir(op.container, op.mountPathHash) },
			"container", op.container, "hash", op.mountPathHash)
	}

	// Write and delete build context files.
	for _, op := range plan.WriteFsBuildContextFiles {
		r.execUnit("writing build file", op.build.FullName(),
			func() error { return mgrs.Build.WriteFile(string(op.build), op.filename, op.content, op.mode) },
			"build", op.build, "file", op.filename)
	}
	for _, op := range plan.DeleteFsBuildContextFiles {
		r.exec("removing stale build file",
			func() error { return mgrs.Build.RemoveFile(string(op.build), op.filename) },
			"build", op.build, "file", op.filename)
	}
	for _, op := range plan.DeleteFsBuildContexts {
		r.exec("removing build context directory",
			func() error { return mgrs.Build.RemoveUnit(string(op.build)) },
			"build", op.build)
	}

	// Delete then upsert podman secrets. Deletes run first so a key rename
	// (delete old + upsert new) never leaves both names present simultaneously.
	for _, op := range plan.DeletePodmanSecrets {
		r.exec("deleting podman secret",
			func() error { return pc.DeleteSecret(ctx, op.Name) },
			"name", op.Name)
	}
	for _, op := range plan.UpsertPodmanSecrets {
		r.exec("upserting podman secret",
			func() error { return pc.UpsertSecret(ctx, op.Name, op.Value, op.Labels) },
			"name", op.Name)
	}

	// Phase 3: Write and delete unit files.
	for _, op := range plan.WriteFsQuadletUnitFiles {
		r.execUnit("writing unit file", op.fullUnitName,
			func() error { return sd.WriteUnitFile(op.fullUnitName, []byte(op.content)) },
			"unit", op.fullUnitName)
	}
	for _, op := range plan.DeleteFsQuadletUnitFiles {
		r.exec("removing stale unit",
			func() error { return sd.RemoveUnitFile(op.fullUnitName) },
			"unit", op.fullUnitName)
	}

	// Delete podman volumes and networks if requested.
	for _, op := range plan.DeletePodmanVolumes {
		r.exec("deleting podman volume",
			func() error { return pc.DeleteVolume(ctx, string(op.volume)) },
			"name", op.volume)
	}
	// A failed recreate delete fails the network unit: its service would
	// otherwise find the old network and keep it with the old settings.
	for _, op := range plan.DeletePodmanNetworks {
		deleteFn := func() error { return pc.DeleteNetwork(ctx, string(op.network)) }
		if op.recreate {
			r.execUnit("deleting podman network", op.network.FullName(), deleteFn, "name", op.network)
		} else {
			r.exec("deleting podman network", deleteFn, "name", op.network)
		}
	}
	for _, op := range plan.DeletePodmanImages {
		r.exec("deleting podman image",
			func() error { return pc.DeleteImage(ctx, string(op.tag)) },
			"tag", op.tag)
	}

	// Single daemon-reload if needed.
	if plan.NeedsReload() {
		reloadTime := time.Now()
		logger.Info("daemon-reload")
		if err := sd.DaemonReload(ctx); err != nil {
			logger.Error("daemon-reload failed", "error", err)
			messages, jErr := jr.QuadletErrorsSince(ctx, reloadTime)
			if jErr != nil {
				logger.Warn("could not read quadlet-generator errors from journal", "error", jErr)
			} else {
				for _, msg := range messages {
					logger.Error("quadlet generator error", "message", msg)
				}
			}
			return report, fmt.Errorf("daemon-reload failed: %w", err)
		}
	}

	// Phase 4a: Reload containers that only need in-place config reload (no restart).
	for _, op := range plan.ReloadSystemdServices {
		fullName := op.container.FullName()
		r.execUnit("reloading", fullName,
			func() error { return sd.ReloadUnit(ctx, string(op.container.ServiceUnitName())) },
			"unit", fullName)
	}

	// Phase 4: Start containers that need starting.
	for _, op := range plan.StartSystemdServices {
		r.execUnit("starting", op.ref.FullName(),
			func() error { return sd.StartUnit(ctx, op.ref.ServiceUnitName()) },
			"unit", op.ref.FullName())
	}

	if report.failed() {
		return report, fmt.Errorf("one or more units failed to apply")
	}
	return report, nil
}
