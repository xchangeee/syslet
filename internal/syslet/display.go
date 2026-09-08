package syslet

import (
	"fmt"
	"io"
	"strings"

	"github.com/aymanbagabas/go-udiff"
	gounit "github.com/coreos/go-systemd/v22/unit"
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

	// Print operations that would be performed
	hasChanges := false

	if len(plan.StopSystemdServices) > 0 {
		hasChanges = true
		_, _ = fmt.Fprintln(w, "\nServices to stop:")
		for _, op := range plan.StopSystemdServices {
			_, _ = fmt.Fprintf(w, "  - %s\n", op.ref.FullName())
		}
	}

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

	if len(plan.WriteFsContainerConfigDirs) > 0 {
		hasChanges = true
		_, _ = fmt.Fprintln(w, "\nConfigDir changes:")
		for _, op := range plan.WriteFsContainerConfigDirs {
			_, _ = fmt.Fprintf(w, "  %s:%s → version %d\n", op.container, op.mountPath, op.version)
			// Collect all filenames: new files and any old files that are being removed.
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

	if len(plan.UpsertPodmanSecrets) > 0 || len(plan.DeletePodmanSecrets) > 0 {
		hasChanges = true
		// Collect ordered spec names while preserving first-seen order.
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
		for _, specName := range specOrder {
			_, _ = fmt.Fprintf(w, "\nSecret changes (%s):\n", specName)
			for _, op := range plan.DeletePodmanSecrets {
				if op.SpecName == specName {
					_, _ = fmt.Fprintf(w, "- %s=(secret)\n", secretKey(specName, op.Name))
				}
			}
			for _, op := range plan.UpsertPodmanSecrets {
				if op.SpecName == specName {
					_, _ = fmt.Fprintf(w, "+ %s=%s\n", secretKey(specName, op.Name), op.Value)
				}
			}
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

	// Print summary of all units
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

// displayContentDiff prints a semantic diff between old and new content.
// For systemd unit files, parses the content into structured options and compares
// them semantically, showing additions/removals with section context.
// For config files, uses text-based unified diff.
func displayContentDiff(w io.Writer, name, oldContent, newContent string) {
	// Only use semantic diff for systemd unit files (not config files)
	isUnitFile := strings.HasSuffix(name, ".container") ||
		strings.HasSuffix(name, ".volume") ||
		strings.HasSuffix(name, ".network")

	if !isUnitFile {
		// Config files: use text diff
		displayTextDiff(w, name, oldContent, newContent)
		return
	}

	// Parse both contents into structured options
	oldOpts, err := gounit.Deserialize(strings.NewReader(oldContent))
	if err != nil {
		// Fallback to text diff if parsing fails
		displayTextDiff(w, name, oldContent, newContent)
		return
	}

	newOpts, err := gounit.Deserialize(strings.NewReader(newContent))
	if err != nil {
		// Fallback to text diff if parsing fails
		displayTextDiff(w, name, oldContent, newContent)
		return
	}

	// Create maps for comparison: "section:name:value" -> count
	oldMap := make(map[string]int)
	newMap := make(map[string]int)

	for _, opt := range oldOpts {
		key := fmt.Sprintf("%s:%s:%s", opt.Section, opt.Name, opt.Value)
		oldMap[key]++
	}

	for _, opt := range newOpts {
		key := fmt.Sprintf("%s:%s:%s", opt.Section, opt.Name, opt.Value)
		newMap[key]++
	}

	// Find differences
	var additions, removals []*gounit.UnitOption

	// Find removals (in old but not in new, or fewer occurrences)
	for _, opt := range oldOpts {
		key := fmt.Sprintf("%s:%s:%s", opt.Section, opt.Name, opt.Value)
		if oldMap[key] > newMap[key] {
			removals = append(removals, opt)
			oldMap[key]-- // Track that we've processed one occurrence
		}
	}

	// Find additions (in new but not in old, or more occurrences)
	for _, opt := range newOpts {
		key := fmt.Sprintf("%s:%s:%s", opt.Section, opt.Name, opt.Value)
		if newMap[key] > oldMap[key] {
			additions = append(additions, opt)
			newMap[key]-- // Track that we've processed one occurrence
		}
	}

	if len(removals) == 0 && len(additions) == 0 {
		return // No semantic changes
	}

	_, _ = fmt.Fprintf(w, "\n--- %s (current)\n", name)
	_, _ = fmt.Fprintf(w, "+++ %s (new)\n", name)

	for _, opt := range removals {
		_, _ = fmt.Fprintf(w, "- [%s] %s=%s\n", opt.Section, opt.Name, opt.Value)
	}
	for _, opt := range additions {
		_, _ = fmt.Fprintf(w, "+ [%s] %s=%s\n", opt.Section, opt.Name, opt.Value)
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
