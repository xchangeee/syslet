//go:build integration

package systest

import (
	"bytes"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/afero"

	"codeberg.org/xchangeee/syslet/internal/model"
	"codeberg.org/xchangeee/syslet/internal/syslet"
	"codeberg.org/xchangeee/syslet/internal/systemd/systemdtest"
	"codeberg.org/xchangeee/syslet/internal/util"
)

// The systemd assertions are Env methods because Env already holds both the
// mock connection that recorded the calls and the client that owns the unit
// files — a free function would only take those back as parameters.
//
// Two distinctions here are load-bearing and must not be collapsed:
//   - AssertReloaded is a systemd *daemon-reload*; AssertContainerReloaded is an
//     in-place reload of one unit. A change that needs one does not need the other.
//   - A restart (stop+start) and a reload are different outcomes, never
//     interchangeable: a restart costs downtime, a reload does not.

// AssertRestarted checks that exactly one unit was stopped and then started.
func (e *Env) AssertRestarted(unit string) {
	e.t.Helper()
	if len(e.Conn.Stopped) != 1 || e.Conn.Stopped[0] != unit {
		e.t.Errorf("expected %s to be stopped, got: %v", unit, e.Conn.Stopped)
	}
	if len(e.Conn.Started) != 1 || e.Conn.Started[0] != unit {
		e.t.Errorf("expected %s to be started, got: %v", unit, e.Conn.Started)
	}
}

// AssertNoStartStop checks that no units were started or stopped.
func (e *Env) AssertNoStartStop() {
	e.t.Helper()
	e.AssertNoneStopped()
	e.AssertNoneStarted()
}

// AssertNoneStarted checks that no units were started.
func (e *Env) AssertNoneStarted() {
	e.t.Helper()
	if len(e.Conn.Started) != 0 {
		e.t.Errorf("expected no starts, got: %v", e.Conn.Started)
	}
}

// AssertNoneStopped checks that no units were stopped.
func (e *Env) AssertNoneStopped() {
	e.t.Helper()
	if len(e.Conn.Stopped) != 0 {
		e.t.Errorf("expected no stops, got: %v", e.Conn.Stopped)
	}
}

// AssertNotStopped checks that the given unit was not stopped, without
// constraining what else was.
func (e *Env) AssertNotStopped(unit string) {
	e.t.Helper()
	if slices.Contains(e.Conn.Stopped, unit) {
		e.t.Errorf("expected %s not to be stopped, but it was (stopped: %v)", unit, e.Conn.Stopped)
	}
}

// AssertStarted checks that exactly the given units were started, order-independently.
func (e *Env) AssertStarted(units ...string) {
	e.t.Helper()
	assertExactly(e.t, "starts", "started", e.Conn.Started, units)
}

// AssertStopped checks that exactly the given units were stopped, order-independently.
func (e *Env) AssertStopped(units ...string) {
	e.t.Helper()
	assertExactly(e.t, "stops", "stopped", e.Conn.Stopped, units)
}

// assertExactly compares a recorded call list against an expected set, ignoring
// order. Every recorded list in this harness — systemd starts and stops, podman
// deletions — is unordered within its phase, so they all share this comparison.
//
// plural names the calls in the count message ("2 stops"); past names them in
// the per-item message ("expected x to be stopped").
func assertExactly(t *testing.T, plural, past string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("expected %d %s %v, got: %v", len(want), plural, want, got)
		return
	}
	gotSet := make(map[string]bool, len(got))
	for _, g := range got {
		gotSet[g] = true
	}
	for _, w := range want {
		if !gotSet[w] {
			t.Errorf("expected %s to be %s, got: %v", w, past, got)
		}
	}
}

// AssertReloaded checks that systemd daemon-reload was called.
func (e *Env) AssertReloaded() {
	e.t.Helper()
	if !e.Conn.Reloaded {
		e.t.Error("expected daemon-reload to be called")
	}
}

// AssertNotReloaded checks that systemd daemon-reload was not called.
func (e *Env) AssertNotReloaded() {
	e.t.Helper()
	if e.Conn.Reloaded {
		e.t.Error("expected no daemon-reload")
	}
}

// AssertContainerReloaded checks that exactly the given unit was reloaded
// in-place, via ReloadUnitContext rather than daemon-reload.
func (e *Env) AssertContainerReloaded(unit string) {
	e.t.Helper()
	if len(e.Conn.ReloadedUnits) != 1 || e.Conn.ReloadedUnits[0] != unit {
		e.t.Errorf("expected container reload of %s, got: %v", unit, e.Conn.ReloadedUnits)
	}
}

// AssertContainerNotReloaded checks that no unit was reloaded in-place.
func (e *Env) AssertContainerNotReloaded() {
	e.t.Helper()
	if len(e.Conn.ReloadedUnits) != 0 {
		e.t.Errorf("expected no container reload, got: %v", e.Conn.ReloadedUnits)
	}
}

// AssertUnitExists checks that a unit file is present on disk.
func (e *Env) AssertUnitExists(name string) {
	e.t.Helper()
	if !e.Systemd.UnitFileExists(name) {
		e.t.Errorf("expected unit file %s to exist", name)
	}
}

// AssertUnitAbsent checks that a unit file is not present on disk.
func (e *Env) AssertUnitAbsent(name string) {
	e.t.Helper()
	if e.Systemd.UnitFileExists(name) {
		e.t.Errorf("expected unit file %s to not exist", name)
	}
}

// --- Podman reclamation ---
//
// These assert what the apply phase reclaimed on the podman side, as opposed to
// what it did to unit files. The two are deliberately separate outcomes: a stale
// unit always loses its unit file, but only a Delete reclaim policy also destroys
// the backing volume, network or image. Most tests therefore assert the negative
// — that reclamation did *not* reach a resource — which is the blast radius the
// policy exists to bound.

// AssertVolumesDeleted checks that exactly the given podman volumes were
// deleted, order-independently.
func (e *Env) AssertVolumesDeleted(names ...string) {
	e.t.Helper()
	assertExactly(e.t, "volume deletions", "deleted", e.Podman.DeletedVolumes(), names)
}

// AssertNoVolumesDeleted checks that no podman volume was deleted.
func (e *Env) AssertNoVolumesDeleted() {
	e.t.Helper()
	if got := e.Podman.DeletedVolumes(); len(got) != 0 {
		e.t.Errorf("expected no volume deletions, got: %v", got)
	}
}

// AssertNetworksDeleted checks that exactly the given podman networks were
// deleted, order-independently.
func (e *Env) AssertNetworksDeleted(names ...string) {
	e.t.Helper()
	assertExactly(e.t, "network deletions", "deleted", e.Podman.DeletedNetworks(), names)
}

// AssertNoNetworksDeleted checks that no podman network was deleted.
func (e *Env) AssertNoNetworksDeleted() {
	e.t.Helper()
	if got := e.Podman.DeletedNetworks(); len(got) != 0 {
		e.t.Errorf("expected no network deletions, got: %v", got)
	}
}

// AssertImagesDeleted checks that exactly the given podman image tags were
// deleted, order-independently.
func (e *Env) AssertImagesDeleted(tags ...string) {
	e.t.Helper()
	assertExactly(e.t, "image deletions", "deleted", e.Podman.DeletedImages(), tags)
}

// AssertNoImagesDeleted checks that no podman image was deleted.
func (e *Env) AssertNoImagesDeleted() {
	e.t.Helper()
	if got := e.Podman.DeletedImages(); len(got) != 0 {
		e.t.Errorf("expected no image deletions, got: %v", got)
	}
}

// --- Podman secrets ---
//
// These assert the apply side of a secret change — what actually reached the
// podman secret store — as opposed to plan.UpsertPodmanSecrets, which asserts
// only that syslet decided to.

// AssertSecretsUpserted checks that exactly the given secret names were
// upserted, order-independently.
func (e *Env) AssertSecretsUpserted(names ...string) {
	e.t.Helper()
	got := make([]string, len(e.Podman.UpsertedSecrets()))
	for i, u := range e.Podman.UpsertedSecrets() {
		got[i] = u.Name
	}
	assertExactly(e.t, "secret upserts", "upserted", got, names)
}

// AssertSecretsDeleted checks that exactly the given secret names were deleted,
// order-independently.
func (e *Env) AssertSecretsDeleted(names ...string) {
	e.t.Helper()
	assertExactly(e.t, "secret deletions", "deleted", e.Podman.DeletedSecrets(), names)
}

// AssertNoSecretsUpserted checks that no secret reached the podman store.
func (e *Env) AssertNoSecretsUpserted() {
	e.t.Helper()
	if got := e.Podman.UpsertedSecrets(); len(got) != 0 {
		e.t.Errorf("expected no secret upserts, got: %v", got)
	}
}

// AssertNoSecretsDeleted checks that no secret was removed from the podman store.
func (e *Env) AssertNoSecretsDeleted() {
	e.t.Helper()
	if got := e.Podman.DeletedSecrets(); len(got) != 0 {
		e.t.Errorf("expected no secret deletions, got: %v", got)
	}
}

// AssertSecretValue checks the plaintext a named secret was upserted with,
// proving decryption carried through apply rather than stopping at the plan.
func (e *Env) AssertSecretValue(name, want string) {
	e.t.Helper()
	for _, u := range e.Podman.UpsertedSecrets() {
		if u.Name == name {
			if string(u.Value) != want {
				e.t.Errorf("secret %q upserted with value %q, want %q", name, string(u.Value), want)
			}
			return
		}
	}
	e.t.Errorf("secret %q was never upserted, got: %v", name, e.Podman.DeletedSecrets())
}

// AssertSecretDeletesPrecedeUpserts checks that every secret deletion was
// issued before the first upsert.
//
// This is the ordering that makes a key rename safe: deleting first means the
// old and new names are never both present on the host, which a reader holding
// only the name would otherwise be able to observe.
func (e *Env) AssertSecretDeletesPrecedeUpserts() {
	e.t.Helper()
	seenUpsert := false
	for _, c := range e.Podman.SecretCalls() {
		switch c.Op {
		case SecretUpsert:
			seenUpsert = true
		case SecretDelete:
			if seenUpsert {
				e.t.Errorf("secret delete of %q issued after an upsert; call order was %v",
					c.Name, e.Podman.SecretCalls())
				return
			}
		}
	}
}

// --- Config store ---
//
// These are Env methods because the on-disk layout of a config file is the
// store's business, and Env has the store to ask. A test naming a mount path
// should never have to know the hashed filename it lands under.

// WantFile is the expected on-disk result for one mounted config file. An empty
// Content means the case only cares about the mode.
type WantFile struct {
	MountPath string
	Mode      os.FileMode
	Content   string
}

// ConfigFilePath returns the path a container's mounted config file occupies in
// the config store.
func (e *Env) ConfigFilePath(container, mountPath string) string {
	return fmt.Sprintf("%s/%s/%s",
		e.Mgrs.Config.BaseDirectory(), container, util.BasenameWithHashSuffix(mountPath))
}

// AssertConfigFileExists checks that a container's config file is on disk.
func (e *Env) AssertConfigFileExists(container, mountPath string) {
	e.t.Helper()
	path := e.ConfigFilePath(container, mountPath)
	if _, err := e.Fs.Stat(path); err != nil {
		e.t.Errorf("expected config file %q to exist: %v", path, err)
	}
}

// AssertConfigFileAbsent checks that a container's config file is gone.
func (e *Env) AssertConfigFileAbsent(container, mountPath string) {
	e.t.Helper()
	path := e.ConfigFilePath(container, mountPath)
	if _, err := e.Fs.Stat(path); err == nil {
		e.t.Errorf("expected config file %q to be absent", path)
	}
}

// AssertConfigDirEmpty checks that no config files remain for a container —
// that removal reclaimed the files, not merely the references to them.
func (e *Env) AssertConfigDirEmpty(container string) {
	e.t.Helper()
	files, err := e.Mgrs.Config.ListFiles(model.ContainerUnitRef(container))
	if err == nil && len(files) > 0 {
		e.t.Errorf("expected config directory for %q to be empty, found: %v", container, files)
	}
}

// AssertConfigDirExists checks that a container's configDir was materialized as
// a real directory on disk.
//
// It stats the OS filesystem, not Env.Fs: this only applies under
// WithOSConfigStore, where the config store is OS-backed and Env.Fs holds just
// the rendered unit files.
func (e *Env) AssertConfigDirExists(container string, mountPath model.ContainerMountPath) {
	e.t.Helper()
	hostPath := e.Mgrs.Config.Resolve(model.ContainerUnitRef(container), mountPath)
	info, err := os.Stat(hostPath)
	if err != nil {
		e.t.Fatalf("expected configDir for %q to exist on disk at %s, got error: %v", container, hostPath, err)
	}
	if !info.IsDir() {
		e.t.Fatalf("expected %s to be a directory", hostPath)
	}
}

// AssertFiles checks the mode, and where given the content, of each expected
// config file of a container.
func (e *Env) AssertFiles(container string, want ...WantFile) {
	e.t.Helper()
	for _, w := range want {
		path := e.ConfigFilePath(container, w.MountPath)
		AssertFileMode(e.t, e.Fs, path, w.Mode)
		if w.Content != "" {
			AssertFileContent(e.t, e.Fs, path, w.Content)
		}
	}
}

// --- Plan rendering ---
//
// These take a plan rather than an Env because syslet.DisplayPlan is the
// subject: one caller builds an ApplyPlan literal with no Env at all. Rendering
// into a buffer and comparing is harness plumbing, not part of any test's
// intent, so it lives here.

// renderPlan returns what syslet.DisplayPlan writes for a plan.
func renderPlan(plan *syslet.ApplyPlan) string {
	var buf bytes.Buffer
	syslet.DisplayPlan(&buf, plan)
	return buf.String()
}

// AssertPlanHasErrors checks that the plan recorded errors rather than syslet
// failing outright — the contract that lets DisplayPlan report them to the user.
func AssertPlanHasErrors(t *testing.T, plan *syslet.ApplyPlan) {
	t.Helper()
	if !plan.HasErrors() {
		t.Error("plan.HasErrors() == false, want true")
	}
}

// AssertPlanOutput checks the full rendered plan against an expected block.
func AssertPlanOutput(t *testing.T, plan *syslet.ApplyPlan, want string) {
	t.Helper()
	if got := renderPlan(plan); got != want {
		t.Errorf("output mismatch\nExpected:\n%s\nGot:\n%s", want, got)
	}
}

// AssertPlanOutputContains checks that the rendered plan includes each snippet.
func AssertPlanOutputContains(t *testing.T, plan *syslet.ApplyPlan, want ...string) {
	t.Helper()
	out := renderPlan(plan)
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("expected plan output to contain %q, got:\n%s", w, out)
		}
	}
}

// AssertPlanOutputOmits checks that the rendered plan includes none of the
// snippets — used to pin that a plan carrying errors suppresses its diff.
func AssertPlanOutputOmits(t *testing.T, plan *syslet.ApplyPlan, unwanted ...string) {
	t.Helper()
	out := renderPlan(plan)
	for _, u := range unwanted {
		if strings.Contains(out, u) {
			t.Errorf("expected plan output not to contain %q, got:\n%s", u, out)
		}
	}
}

// AssertStaged checks that each named unit file was staged for validation,
// reading back what the generator recorded when the plan ran.
func AssertStaged(t *testing.T, gen *systemdtest.RecordingQuadletGenerator, names ...string) {
	t.Helper()
	staged := make(map[string]bool)
	for _, f := range gen.StagedFiles() {
		staged[f] = true
	}
	for _, name := range names {
		if !staged[name] {
			t.Errorf("expected %s to be staged, got: %v", name, gen.StagedFiles())
		}
	}
}

// --- Filesystem ---
//
// These stay package-level functions taking an explicit fs, because the
// configDir tests assert against the OS-backed store rather than against
// Env.Fs, and so need to name which filesystem they mean.

// AssertFileMode checks that a file on the given filesystem has the expected
// permission bits.
func AssertFileMode(t *testing.T, fs afero.Fs, path string, want os.FileMode) {
	t.Helper()
	info, err := fs.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%q) failed: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("mode of %q: got %04o, want %04o", path, got, want)
	}
}

// AssertFileContent checks the content of a file on the given filesystem.
func AssertFileContent(t *testing.T, fs afero.Fs, path, want string) {
	t.Helper()
	got, err := afero.ReadFile(fs, path)
	if err != nil {
		t.Fatalf("ReadFile(%q) failed: %v", path, err)
	}
	if string(got) != want {
		t.Errorf("content of %q:\ngot:  %q\nwant: %q", path, string(got), want)
	}
}
