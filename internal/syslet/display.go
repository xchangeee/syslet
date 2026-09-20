package syslet

import (
	"cmp"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/aymanbagabas/go-udiff"
	gounit "github.com/coreos/go-systemd/v22/unit"

	"codeberg.org/xchangeee/syslet/internal/model"
)

// secretKey extracts the key portion from a full podman secret name "<specname>-<key>".
func secretKey(specName, fullName string) string {
	return strings.TrimPrefix(fullName, specName+"-")
}

// DisplayPlan prints a human-readable diff showing what would change.
// It shows operations that would be performed and the final status of each unit.
// If the plan has any errors (validation or per-unit), only those errors are
// printed — the diff is suppressed because the plan cannot safely be applied.
func DisplayPlan(w io.Writer, plan *ApplyPlan) {
	if plan.HasErrors() {
		_, _ = fmt.Fprintln(w, "Validation errors:")
		for _, msg := range plan.Errors {
			_, _ = fmt.Fprintf(w, "  error: %s\n", msg)
		}
		for _, r := range plan.Results {
			if r.errored {
				_, _ = fmt.Fprintf(w, "  error [%s]: %s\n", string(r.fullUnitName), r.message)
			}
		}
		return
	}

	hasChanges := false

	// 1. Unit file changes
	if len(plan.WriteFsQuadletUnitFiles) > 0 {
		hasChanges = true
		_, _ = fmt.Fprintln(w, "\nUnit file changes:")
		for _, op := range plan.WriteFsQuadletUnitFiles {
			displayContentDiff(w, string(op.fullUnitName), op.oldContent, op.content)
		}
	}

	if len(plan.DeleteFsQuadletUnitFiles) > 0 {
		hasChanges = true
		_, _ = fmt.Fprintln(w, "\nUnit files to delete:")
		for _, op := range plan.DeleteFsQuadletUnitFiles {
			_, _ = fmt.Fprintf(w, "  - %s\n", op.fullUnitName)
		}
	}

	// 2. Config file changes
	if len(plan.WriteFsContainerConfigFiles) > 0 {
		hasChanges = true
		_, _ = fmt.Fprintln(w, "\nConfig file changes:")
		for _, op := range plan.WriteFsContainerConfigFiles {
			displayContentDiff(w, fmt.Sprintf("%s:%s", op.container, op.mountPath), op.oldContent, op.content)
			if op.oldMode != 0 && op.oldMode != op.mode {
				_, _ = fmt.Fprintf(w, "  %s:%s mode: %04o → %04o\n", op.container, op.mountPath, op.oldMode, op.mode)
			}
		}
	}

	if len(plan.DeleteFsContainerConfigFiles) > 0 {
		hasChanges = true
		_, _ = fmt.Fprintln(w, "\nConfig files to delete:")
		for _, op := range plan.DeleteFsContainerConfigFiles {
			_, _ = fmt.Fprintf(w, "  - %s/%s\n", op.container, op.internalFilename)
		}
	}

	if len(plan.DeleteFsContainerConfigs) > 0 {
		hasChanges = true
		_, _ = fmt.Fprintln(w, "\nConfig directories to delete:")
		for _, op := range plan.DeleteFsContainerConfigs {
			_, _ = fmt.Fprintf(w, "  - %s/\n", op.container)
		}
	}

	// 3. ConfigDir changes
	if len(plan.WriteFsContainerConfigDirs) > 0 {
		hasChanges = true
		_, _ = fmt.Fprintln(w, "\nConfigDir changes:")
		for _, op := range plan.WriteFsContainerConfigDirs {
			_, _ = fmt.Fprintf(w, "  %s:%s → version %d\n", op.container, op.mountPath, op.version)
			seen := make(map[string]bool, len(op.files))
			for _, f := range op.files {
				seen[f.Name] = true
				displayTextDiff(w, f.Name, op.oldFiles[f.Name], f.Content)
				if oldMode, ok := op.oldModes[f.Name]; ok && oldMode != f.Mode {
					_, _ = fmt.Fprintf(w, "  %s mode: %04o → %04o\n", f.Name, oldMode, f.Mode)
				}
			}
			for name, oldContent := range op.oldFiles {
				if !seen[name] {
					displayTextDiff(w, name, oldContent, "")
				}
			}
		}
	}

	if len(plan.DeleteFsContainerConfigDirs) > 0 {
		hasChanges = true
		_, _ = fmt.Fprintln(w, "\nConfigDir groups to delete:")
		for _, op := range plan.DeleteFsContainerConfigDirs {
			_, _ = fmt.Fprintf(w, "  - %s/%s\n", op.container, op.mountPathHash)
		}
	}

	// Build context changes (grouped with file changes)
	if len(plan.WriteFsBuildContextFiles) > 0 {
		hasChanges = true
		_, _ = fmt.Fprintln(w, "\nBuild context file changes:")
		for _, op := range plan.WriteFsBuildContextFiles {
			displayContentDiff(w, fmt.Sprintf("%s:%s", op.build, op.destination), op.oldContent, op.content)
			if op.oldMode != 0 && op.oldMode != op.mode {
				_, _ = fmt.Fprintf(w, "  %s:%s mode: %04o → %04o\n", op.build, op.destination, op.oldMode, op.mode)
			}
		}
	}

	if len(plan.DeleteFsBuildContextFiles) > 0 {
		hasChanges = true
		_, _ = fmt.Fprintln(w, "\nBuild context files to delete:")
		for _, op := range plan.DeleteFsBuildContextFiles {
			_, _ = fmt.Fprintf(w, "  - %s/%s\n", op.build, op.filename)
		}
	}

	if len(plan.DeleteFsBuildContexts) > 0 {
		hasChanges = true
		_, _ = fmt.Fprintln(w, "\nBuild context directories to delete:")
		for _, op := range plan.DeleteFsBuildContexts {
			_, _ = fmt.Fprintf(w, "  - %s/\n", op.build)
		}
	}

	// 4. Secret changes
	if len(plan.UpsertPodmanSecrets) > 0 || len(plan.DeletePodmanSecrets) > 0 {
		hasChanges = true
		seen := make(map[string]bool)
		var specOrder []string
		for _, op := range plan.DeletePodmanSecrets {
			if !seen[op.SpecName] {
				seen[op.SpecName] = true
				specOrder = append(specOrder, op.SpecName)
			}
		}
		for _, op := range plan.UpsertPodmanSecrets {
			if !seen[op.SpecName] {
				seen[op.SpecName] = true
				specOrder = append(specOrder, op.SpecName)
			}
		}
		_, _ = fmt.Fprintln(w, "\nSecret changes:")
		for _, specName := range specOrder {
			label := specName
			if label == "" {
				label = "(orphaned)"
			}
			_, _ = fmt.Fprintf(w, "  %s:\n", label)
			for _, op := range plan.DeletePodmanSecrets {
				if op.SpecName == specName {
					_, _ = fmt.Fprintf(w, "    - %s=(secret)\n", secretKey(specName, op.Name))
				}
			}
			for _, op := range plan.UpsertPodmanSecrets {
				if op.SpecName == specName {
					_, _ = fmt.Fprintf(w, "    + %s=%s\n", secretKey(specName, op.Name), op.Value)
				}
			}
		}
	}

	// 5. Systemd actions
	if len(plan.StopSystemdServices) > 0 {
		hasChanges = true
		_, _ = fmt.Fprintln(w, "\nServices to stop:")
		for _, op := range plan.StopSystemdServices {
			_, _ = fmt.Fprintf(w, "  - %s\n", op.ref.FullName())
		}
	}

	if plan.NeedsReload() {
		hasChanges = true
		_, _ = fmt.Fprintln(w, "\nSystemd daemon-reload: required")
	}

	if len(plan.StartSystemdServices) > 0 {
		hasChanges = true
		_, _ = fmt.Fprintln(w, "\nContainers to start:")
		for _, op := range plan.StartSystemdServices {
			_, _ = fmt.Fprintf(w, "  - %s\n", op.ref.FullName())
		}
	}

	if len(plan.DeletePodmanVolumes) > 0 {
		hasChanges = true
		_, _ = fmt.Fprintln(w, "\nPodman volumes to delete:")
		for _, op := range plan.DeletePodmanVolumes {
			_, _ = fmt.Fprintf(w, "  - %s\n", op.volume)
		}
	}

	if len(plan.DeletePodmanNetworks) > 0 {
		hasChanges = true
		_, _ = fmt.Fprintln(w, "\nPodman networks to delete:")
		for _, op := range plan.DeletePodmanNetworks {
			_, _ = fmt.Fprintf(w, "  - %s\n", op.network)
		}
	}

	if len(plan.DeletePodmanImages) > 0 {
		hasChanges = true
		_, _ = fmt.Fprintln(w, "\nPodman images to delete:")
		for _, op := range plan.DeletePodmanImages {
			_, _ = fmt.Fprintf(w, "  - %s\n", op.tag)
		}
	}

	// Summary
	_, _ = fmt.Fprintln(w, "\nSummary:")
	_, _ = fmt.Fprintf(w, "%-40s %-10s %s\n", "UNIT", "STATUS", "CHANGES")
	for _, result := range plan.Results {
		_, _ = fmt.Fprintf(w, "%-40s %-10s %s\n", result.fullUnitName, result.status, result.message)
	}

	if !hasChanges {
		_, _ = fmt.Fprintln(w, "\nNo changes detected. All units are up to date.")
	}
}

// DisplayResults prints the post-apply results summary table.
func DisplayResults(w io.Writer, plan *ApplyPlan) {
	for _, msg := range plan.Errors {
		_, _ = fmt.Fprintf(w, "error: %s\n", msg)
	}
	for _, result := range plan.Results {
		_, _ = fmt.Fprintf(w, "%-40s %-10s %s\n", result.fullUnitName, result.status, result.message)
	}
}

// displayContentDiff prints a structured diff between old and new unit file content.
// For systemd unit files it groups changes into added/changed/removed blocks.
// For config files it falls back to unified text diff.
func displayContentDiff(w io.Writer, name, oldContent, newContent string) {
	_, err := model.ParseFullUnitName(model.FullUnitName(name))
	if err != nil {
		displayTextDiff(w, name, oldContent, newContent)
		return
	}

	oldOpts, err := gounit.DeserializeOptions(strings.NewReader(oldContent))
	if err != nil {
		displayTextDiff(w, name, oldContent, newContent)
		return
	}

	newOpts, err := gounit.DeserializeOptions(strings.NewReader(newContent))
	if err != nil {
		displayTextDiff(w, name, oldContent, newContent)
		return
	}

	oldCount := make(map[string]int)
	newCount := make(map[string]int)
	optKey := func(opt *gounit.UnitOption) string {
		return fmt.Sprintf("%s\x00%s\x00%s", opt.Section, opt.Name, opt.Value)
	}
	for _, opt := range oldOpts {
		oldCount[optKey(opt)]++
	}
	for _, opt := range newOpts {
		newCount[optKey(opt)]++
	}

	var removals, additions []*gounit.UnitOption
	for _, opt := range oldOpts {
		k := optKey(opt)
		if oldCount[k] > newCount[k] {
			removals = append(removals, opt)
			oldCount[k]--
		}
	}
	for _, opt := range newOpts {
		k := optKey(opt)
		if newCount[k] > oldCount[k] {
			additions = append(additions, opt)
			newCount[k]--
		}
	}

	if len(removals) == 0 && len(additions) == 0 {
		return
	}

	// Identify "changed" entries: same [section] key appears exactly once in
	// both removals and additions, meaning only the value was updated.
	type skKey struct{ section, name string }
	removedBySK := make(map[skKey][]*gounit.UnitOption)
	addedBySK := make(map[skKey][]*gounit.UnitOption)
	for _, opt := range removals {
		sk := skKey{opt.Section, opt.Name}
		removedBySK[sk] = append(removedBySK[sk], opt)
	}
	for _, opt := range additions {
		sk := skKey{opt.Section, opt.Name}
		addedBySK[sk] = append(addedBySK[sk], opt)
	}

	type changedEntry struct{ section, name, oldVal, newVal string }
	var changed []changedEntry
	changedKeys := make(map[skKey]bool)
	for sk, rems := range removedBySK {
		if adds := addedBySK[sk]; len(rems) == 1 && len(adds) == 1 {
			changed = append(changed, changedEntry{sk.section, sk.name, rems[0].Value, adds[0].Value})
			changedKeys[sk] = true
		}
	}
	slices.SortFunc(changed, func(a, b changedEntry) int {
		if n := cmp.Compare(a.section, b.section); n != 0 {
			return n
		}
		return cmp.Compare(a.name, b.name)
	})

	var pureAdded, pureRemoved []*gounit.UnitOption
	for _, opt := range additions {
		if !changedKeys[skKey{opt.Section, opt.Name}] {
			pureAdded = append(pureAdded, opt)
		}
	}
	for _, opt := range removals {
		if !changedKeys[skKey{opt.Section, opt.Name}] {
			pureRemoved = append(pureRemoved, opt)
		}
	}

	_, _ = fmt.Fprintf(w, "\n--- %s\n", name)

	if len(pureAdded) > 0 {
		_, _ = fmt.Fprintln(w, "added:")
		for _, opt := range pureAdded {
			_, _ = fmt.Fprintf(w, "  [%s] %s=%s\n", opt.Section, opt.Name, opt.Value)
		}
	}
	if len(changed) > 0 {
		_, _ = fmt.Fprintln(w, "changed:")
		for _, e := range changed {
			_, _ = fmt.Fprintf(w, "  [%s] %s\n", e.section, e.name)
			_, _ = fmt.Fprintf(w, "    old: %s\n", e.oldVal)
			_, _ = fmt.Fprintf(w, "    new: %s\n", e.newVal)
		}
	}
	if len(pureRemoved) > 0 {
		_, _ = fmt.Fprintln(w, "removed:")
		for _, opt := range pureRemoved {
			_, _ = fmt.Fprintf(w, "  [%s] %s=%s\n", opt.Section, opt.Name, opt.Value)
		}
	}
}

// displayTextDiff is a fallback for non-unit files or when parsing fails.
// Uses unified diff format for better readability.
func displayTextDiff(w io.Writer, name, oldContent, newContent string) {
	diff := udiff.Unified(name+" (current)", name+" (new)", oldContent, newContent)
	if diff != "" {
		_, _ = fmt.Fprint(w, diff)
	}
}
