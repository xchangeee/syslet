//go:build integration

package systest

import (
	"os"

	"codeberg.org/xchangeee/syslet/internal/model"
	"codeberg.org/xchangeee/syslet/internal/render"
	"codeberg.org/xchangeee/syslet/internal/systemd"
)

// Seeding puts the host into the state a previous apply would have left it in:
// unit files installed, services running, config and build-context files on
// disk. Everything a test seeds is "before"; everything it declares via Specs
// is "after". Keeping the two vocabularies distinct is what makes a test's
// scenario readable without tracing the setup.
//
// Each Seed method renders internally from the same spec type the test already
// has, so a test never has to construct a filesystem up front purely to render
// a unit into a fixture literal.

// Render returns the rendered unit-file content for a spec, resolving config
// and build-context paths through this Env's stores. Use it when the rendered
// content itself is the subject; SeedUnit renders the same way.
func (e *Env) Render(spec model.Unit) string {
	e.t.Helper()
	var (
		rendered render.RenderedUnit
		err      error
	)
	switch s := spec.(type) {
	case *model.ContainerUnit:
		rendered, err = render.NewRenderedUnitFromContainer(s, e.Mgrs.Config.Resolve)
	case *model.VolumeUnit:
		rendered, err = render.NewRenderedUnitFromVolume(s)
	case *model.NetworkUnit:
		rendered, err = render.NewRenderedUnitFromNetwork(s)
	case *model.BuildUnit:
		rendered, err = render.NewRenderedUnitFromBuild(s, e.Mgrs.Build.Resolve)
	default:
		e.t.Fatalf("Render: unsupported unit type %T", spec)
	}
	if err != nil {
		e.t.Fatalf("Render(%s): %v", spec.Ref().FullName(), err)
	}
	return rendered.Content
}

// SeedUnit installs a spec's rendered unit file, as a previous apply would
// have. The service is left inactive; use SeedActive to also mark it running.
func (e *Env) SeedUnit(spec model.Unit) {
	e.t.Helper()
	content := e.Render(spec)
	if err := e.Systemd.WriteUnitFile(spec.Ref().FullName(), []byte(content)); err != nil {
		e.t.Fatalf("SeedUnit(%s): %v", spec.Ref().FullName(), err)
	}
}

// SeedActive installs a spec's unit file and marks its generated service
// active. The service name is derived from the unit's own ref, so tests do not
// restate the mapping (webapp.container → webapp.service, data.volume →
// data-volume.service).
func (e *Env) SeedActive(spec model.Unit) {
	e.t.Helper()
	e.SeedUnit(spec)
	e.Conn.SetUnitState(string(spec.Ref().ServiceUnitName()), systemd.ActiveStateActive)
}

// SeedUnitFile installs raw unit-file content under a name, for the rare test
// whose "before" state is not expressible as a spec — e.g. a file written with
// its multi-value keys in a different order.
func (e *Env) SeedUnitFile(fullName, content string) {
	e.t.Helper()
	if err := e.Systemd.WriteUnitFile(model.FullUnitName(fullName), []byte(content)); err != nil {
		e.t.Fatalf("SeedUnitFile(%s): %v", fullName, err)
	}
}

// SetUnitState marks a generated service active or inactive directly. Prefer
// SeedActive; this is the escape hatch for services whose unit file is not
// being seeded, or which must be staged as explicitly inactive.
func (e *Env) SetUnitState(service, state string) {
	e.t.Helper()
	e.Conn.SetUnitState(service, systemd.ActiveState(state))
}

// SeedConfigFile writes a single mounted config file into the container config
// store, simulating a config already deployed for that container.
func (e *Env) SeedConfigFile(container, mountPath, content string, mode os.FileMode) {
	e.t.Helper()
	err := e.Mgrs.Config.WriteFile(
		model.ContainerUnitRef(container), model.ContainerMountPath(mountPath), content, mode)
	if err != nil {
		e.t.Fatalf("SeedConfigFile(%q, %q): %v", container, mountPath, err)
	}
}

// SeedConfigDir writes a versioned config directory and points the ..data
// symlink at it, which together are what a previously-applied configDir looks
// like on disk. Requires WithOSConfigStore, since the symlink step is not
// supported by the in-memory filesystem.
func (e *Env) SeedConfigDir(container string, mountPath model.ContainerMountPath, version int, files ...model.ContainerConfigFile) {
	e.t.Helper()
	ref := model.ContainerUnitRef(container)
	for _, f := range files {
		mode := f.Mode
		if mode == 0 {
			mode = 0644
		}
		if err := e.Mgrs.Config.WriteVersionedDirFile(ref, mountPath, version, f.Name, mode, f.Content); err != nil {
			e.t.Fatalf("SeedConfigDir WriteVersionedDirFile(%q): %v", f.Name, err)
		}
	}
	if err := e.Mgrs.Config.UpdateDirSymlink(ref, mountPath, version); err != nil {
		e.t.Fatalf("SeedConfigDir UpdateDirSymlink: %v", err)
	}
}

// SeedBuildContext writes a file into a build unit's context directory,
// simulating a Containerfile or support file from a previous apply.
func (e *Env) SeedBuildContext(unit, filename, content string, mode os.FileMode) {
	e.t.Helper()
	if err := e.Mgrs.Build.WriteFile(unit, filename, content, mode); err != nil {
		e.t.Fatalf("SeedBuildContext(%q, %q): %v", unit, filename, err)
	}
}
