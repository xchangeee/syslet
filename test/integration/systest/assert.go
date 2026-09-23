//go:build integration

package systest

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/afero"

	"github.com/xchangeee/syslet/internal/model"
	"github.com/xchangeee/syslet/internal/podman/podmantest"
	"github.com/xchangeee/syslet/internal/syslet"
	"github.com/xchangeee/syslet/internal/systemd/systemdtest"
	"github.com/xchangeee/syslet/internal/util"
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
//
// Existence alone is a weak claim in any test that seeded the unit first: the
// file was already there before apply ran, so the assertion holds whether or
// not apply rewrote it. Where a test's subject is a unit that *changed*, use
// AssertUnitMatches instead.
func (e *Env) AssertUnitExists(name string) {
	e.t.Helper()
	if !e.Systemd.UnitFileExists(name) {
		e.t.Errorf("expected unit file %s to exist", name)
	}
}

// AssertUnitContent checks a unit file's bytes on disk.
func (e *Env) AssertUnitContent(name model.FullUnitName, want string) {
	e.t.Helper()
	got, err := e.Systemd.ReadUnitFile(name)
	if err != nil {
		e.t.Errorf("reading unit file %s: %v", name, err)
		return
	}
	if string(got) != want {
		e.t.Errorf("unit file %s:\ngot:\n%s\nwant:\n%s", name, string(got), want)
	}
}

// AssertUnitMatches checks that the installed unit file is exactly what the
// spec renders to.
//
// This is what most tests naming a changed unit actually mean. A unit file that
// apply forgot to rewrite, or wrote from the wrong spec, is invisible to an
// existence check and produces a host running last deploy's configuration.
func (e *Env) AssertUnitMatches(spec model.Unit) {
	e.t.Helper()
	e.AssertUnitContent(spec.Ref().FullName(), e.Render(spec))
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

// AssertSecretLabels checks the labels a named secret was upserted with.
func (e *Env) AssertSecretLabels(name string, want map[string]string) {
	e.t.Helper()
	for _, u := range e.Podman.UpsertedSecrets() {
		if u.Name != name {
			continue
		}
		if !maps.Equal(u.Labels, want) {
			e.t.Errorf("secret %q upserted with labels %v, want %v", name, u.Labels, want)
		}
		return
	}
	e.t.Errorf("secret %q was never upserted", name)
}

// AssertSecretHashLabel checks the syslet/hash label a named secret was
// upserted with.
//
// This label is the whole of syslet's change detection for secrets: the plan
// compares it against the hash of the desired ciphertext, and a mismatch is what
// makes a secret — and every container consuming it — get rewritten. Upserting
// with a missing or wrong hash is therefore invisible on the run that does it
// and pathological on every run after, which re-upserts and restarts
// indefinitely. Nothing about the upsert itself reveals that; only the label
// does.
func (e *Env) AssertSecretHashLabel(name, wantHash string) {
	e.t.Helper()
	e.AssertSecretLabels(name, map[string]string{"syslet/hash": wantHash})
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
		case podmantest.SecretUpsert:
			seenUpsert = true
		case podmantest.SecretDelete:
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

// AnyContent is the Content of a WantFile whose case is about the mode alone.
//
// It exists so that leaving Content unset cannot quietly mean "do not check the
// content": a forgotten field and a deliberate omission must not look the same,
// or a case named for the thing it checks ends up checking nothing.
const AnyContent = "\x00any-content"

// WantFile is the expected on-disk result for one mounted config file. Set
// Content to AnyContent to assert the mode alone.
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
	if _, err := e.configFs.Stat(path); err != nil {
		e.t.Errorf("expected config file %q to exist: %v", path, err)
	}
}

// AssertConfigFileAbsent checks that a container's config file is gone.
func (e *Env) AssertConfigFileAbsent(container, mountPath string) {
	e.t.Helper()
	path := e.ConfigFilePath(container, mountPath)
	if _, err := e.configFs.Stat(path); err == nil {
		e.t.Errorf("expected config file %q to be absent", path)
	}
}

// AssertConfigDirEmpty checks that no config files remain for a container —
// that removal reclaimed the files, not merely the references to them.
func (e *Env) AssertConfigDirEmpty(container string) {
	e.t.Helper()
	files, err := e.Mgrs.Config.ListFiles(model.ContainerUnitRef(container))
	if err != nil {
		// A store error must fail rather than pass: treating it as "nothing
		// found" would let a broken store satisfy an emptiness check.
		e.t.Errorf("listing config files for %q: %v", container, err)
		return
	}
	if len(files) > 0 {
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

// AssertFiles checks that a container's config store holds exactly the expected
// files, with the expected modes and contents.
//
// The set is exact, so a file that should have been removed is caught as an
// extra rather than passing unnoticed, and an empty want asserts the container
// has no config files at all rather than asserting nothing.
func (e *Env) AssertFiles(container string, want ...WantFile) {
	e.t.Helper()

	wantNames := make(map[string]bool, len(want))
	for _, w := range want {
		wantNames[e.Mgrs.Config.InternalFilename(model.ContainerMountPath(w.MountPath))] = true

		path := e.ConfigFilePath(container, w.MountPath)
		AssertFileMode(e.t, e.configFs, path, w.Mode)
		if w.Content != AnyContent {
			AssertFileContent(e.t, e.configFs, path, w.Content)
		}
	}

	got, err := e.Mgrs.Config.ListFiles(model.ContainerUnitRef(container))
	if err != nil {
		e.t.Errorf("listing config files for %q: %v", container, err)
		return
	}
	for _, name := range got {
		if !wantNames[name] {
			e.t.Errorf("unexpected config file %q for %q; want exactly %d file(s)",
				name, container, len(want))
		}
	}
}

// --- ConfigDir contents ---
//
// A configDir is not asserted like a file mount. Its contents live in a
// numbered version directory, and the container sees them through the ..data
// symlink, so "what is on disk" and "what the container would read" are
// different questions. These assertions all ask the second one: they resolve
// the symlink first, exactly as a bind mount would.
//
// This matters because the cheap path for a configDir change is an in-place
// reload rather than a restart. A reload tells the application to re-read files
// that syslet claims to have updated — so a reload issued over stale or missing
// content is worse than no reload at all, and the reload assertion alone cannot
// tell the two apart.

// AssertConfigDirFiles checks that a container's configDir holds exactly the
// expected files, with the expected contents and modes, as seen through ..data.
func (e *Env) AssertConfigDirFiles(container string, mountPath model.ContainerMountPath, want ...model.ContainerConfigFile) {
	e.t.Helper()
	ref := model.ContainerUnitRef(container)

	version, err := e.Mgrs.Config.CurrentDirVersion(ref, mountPath)
	if err != nil {
		e.t.Fatalf("resolving current configDir version for %q %s: %v", container, mountPath, err)
	}
	contents, err := e.Mgrs.Config.ReadVersionedDirFiles(ref, mountPath, version)
	if err != nil {
		e.t.Fatalf("reading configDir files for %q %s: %v", container, mountPath, err)
	}
	modes, err := e.Mgrs.Config.ReadVersionedDirFileModes(ref, mountPath, version)
	if err != nil {
		e.t.Fatalf("reading configDir modes for %q %s: %v", container, mountPath, err)
	}

	wantNames := make(map[string]bool, len(want))
	for _, w := range want {
		wantNames[w.Name] = true
		got, ok := contents[w.Name]
		if !ok {
			e.t.Errorf("configDir %q %s: missing file %q (present: %v)",
				container, mountPath, w.Name, sortedKeys(contents))
			continue
		}
		if got != w.Content {
			e.t.Errorf("configDir %q %s file %q:\ngot:  %q\nwant: %q",
				container, mountPath, w.Name, got, w.Content)
		}
		wantMode := w.Mode
		if wantMode == 0 {
			wantMode = 0644
		}
		if modes[w.Name].Perm() != wantMode.Perm() {
			e.t.Errorf("configDir %q %s file %q mode: got %04o, want %04o",
				container, mountPath, w.Name, modes[w.Name].Perm(), wantMode.Perm())
		}
	}
	for name := range contents {
		if !wantNames[name] {
			e.t.Errorf("configDir %q %s: unexpected file %q", container, mountPath, name)
		}
	}
}

// AssertConfigDirAbsent checks that a container's configDir is gone from disk.
//
// AssertConfigDirEmpty cannot answer this: it lists the container's files, and a
// configDir is a directory, so an abandoned one is invisible to it. A configDir
// that outlives the mount referencing it is a leak that grows with every spec
// change and is never reclaimed afterwards.
func (e *Env) AssertConfigDirAbsent(container string, mountPath model.ContainerMountPath) {
	e.t.Helper()
	path := e.Mgrs.Config.Resolve(model.ContainerUnitRef(container), mountPath)
	if _, err := e.configFs.Stat(path); err == nil {
		e.t.Errorf("expected configDir %q to be absent from disk", path)
	}
}

// AssertConfigDirVersion checks which version the ..data symlink resolves to,
// pinning that a change produced a new version and a no-op did not.
func (e *Env) AssertConfigDirVersion(container string, mountPath model.ContainerMountPath, want int) {
	e.t.Helper()
	got, err := e.Mgrs.Config.CurrentDirVersion(model.ContainerUnitRef(container), mountPath)
	if err != nil {
		e.t.Fatalf("resolving current configDir version for %q %s: %v", container, mountPath, err)
	}
	if got != want {
		e.t.Errorf("configDir %q %s is at version %d, want %d", container, mountPath, got, want)
	}
}

// AssertConfigDirVersionsKept checks exactly which version directories survive
// on disk.
//
// Every configDir change writes a new numbered directory and repoints ..data at
// it; the old ones are reclaimed afterwards. That reclamation is best-effort and
// silent, so without this assertion a host accumulates a directory per deploy
// forever — a slow leak that no test observing only the current version can see.
func (e *Env) AssertConfigDirVersionsKept(container string, mountPath model.ContainerMountPath, want ...int) {
	e.t.Helper()
	dirPath := e.Mgrs.Config.Resolve(model.ContainerUnitRef(container), mountPath)
	entries, err := afero.ReadDir(e.configFs, dirPath)
	if err != nil {
		e.t.Fatalf("listing configDir versions at %q: %v", dirPath, err)
	}

	var got []int
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if v, convErr := strconv.Atoi(entry.Name()); convErr == nil {
			got = append(got, v)
		}
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		e.t.Errorf("configDir %q %s has versions %v on disk, want %v",
			container, mountPath, got, want)
	}
}

// sortedKeys returns a map's keys in order, for stable failure messages.
func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// --- Build context store ---
//
// The build context is the input to a container image, and it is the half of a
// build unit that does not live in the unit file: a changed Containerfile
// triggers a rebuild with no unit-file diff at all. Asserting only that the
// build service restarted would pass just as well against an apply that rebuilt
// from the previous context.

// BuildContextDir returns the directory a build unit's context files occupy.
func (e *Env) BuildContextDir(unit string) string {
	return filepath.Join(e.Mgrs.Build.BaseDirectory(), unit)
}

// AssertBuildContextFiles checks that a build unit's context directory holds
// exactly the expected files, with the expected contents and modes.
func (e *Env) AssertBuildContextFiles(unit string, want ...WantFile) {
	e.t.Helper()

	wantNames := make(map[string]bool, len(want))
	for _, w := range want {
		// A context file is named directly, not by mount path, so MountPath
		// carries the filename.
		wantNames[w.MountPath] = true
		path := e.Mgrs.Build.Resolve(unit, w.MountPath)
		AssertFileMode(e.t, e.Fs, path, w.Mode)
		if w.Content != AnyContent {
			AssertFileContent(e.t, e.Fs, path, w.Content)
		}
	}

	entries, err := afero.ReadDir(e.Fs, e.BuildContextDir(unit))
	if err != nil {
		e.t.Errorf("listing build context for %q: %v", unit, err)
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() && !wantNames[entry.Name()] {
			e.t.Errorf("unexpected build context file %q for %q", entry.Name(), unit)
		}
	}
}

// AssertBuildContextAbsent checks that a build unit's context directory is
// gone, proving a stale build reclaimed its files and not merely its unit.
func (e *Env) AssertBuildContextAbsent(unit string) {
	e.t.Helper()
	path := e.BuildContextDir(unit)
	if _, err := e.Fs.Stat(path); err == nil {
		e.t.Errorf("expected build context directory %q to be absent", path)
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
