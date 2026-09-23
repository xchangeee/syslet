//go:build integration

package systest

import (
	"github.com/xchangeee/syslet/internal/model"
)

// Spec builders produce the model.Unit values a test declares or seeds.
//
// They are option-style rather than a family of make*Spec* variants so that a
// test states only what it varies: a bare NewContainer is a plain running
// container, and each option names one deviation. The defaults are chosen to
// match the overwhelmingly common case, so that what appears at a call site is
// exactly what the scenario is about.

// specBuild accumulates option effects before the model constructor runs.
// A builder is needed because the model constructors take everything
// positionally and up front, while options arrive in any order.
type specBuild struct {
	options        model.UnitOptions
	desiredState   model.DesiredState
	removalAllowed bool
	reclaimPolicy  model.ReclaimPolicy
	fileMounts     []model.ContainerFileMount
	dirMounts      []model.ContainerDirMount
}

// set records a key in a section, creating the section on first use.
func (b *specBuild) set(section, key string, value model.UnitValue) {
	if b.options[model.SectionName(section)] == nil {
		b.options[model.SectionName(section)] = map[model.SectionKey]model.UnitValue{}
	}
	b.options[model.SectionName(section)][model.SectionKey(key)] = value
}

// append adds a value to a multi-valued key such as Network= or Volume=,
// preserving any values already set.
func (b *specBuild) append(section, key, value string) {
	sec := model.SectionName(section)
	if b.options[sec] == nil {
		b.options[sec] = map[model.SectionKey]model.UnitValue{}
	}
	existing := b.options[sec][model.SectionKey(key)]
	b.set(section, key, model.MultiUV(append(existing.Values(), value)...))
}

// UnitOpt customizes any unit type. Options that only make sense for a
// container are ContainerOpt instead, so the compiler rejects e.g. Running on a
// volume rather than silently ignoring it.
type UnitOpt func(*specBuild)

// ContainerOpt customizes a container spec. Every UnitOpt is also a
// ContainerOpt via Unit.
type ContainerOpt func(*specBuild)

// Unit adapts a UnitOpt for use on a container.
func Unit(opt UnitOpt) ContainerOpt {
	return func(b *specBuild) { opt(b) }
}

// --- Options ---

// Running marks a container as desired-running. This is the default for
// NewContainer, so it is only needed for emphasis.
func Running(b *specBuild) { b.desiredState = model.DesiredStateRunning }

// Stopped marks a container as desired-stopped.
func Stopped(b *specBuild) { b.desiredState = model.DesiredStateStopped }

// Stale marks a unit as removable, i.e. eligible for pruning once it drops out
// of the desired set. For a container it also implies desired-stopped, matching
// how a stale container is actually declared.
func Stale(b *specBuild) {
	b.removalAllowed = true
	b.desiredState = model.DesiredStateStopped
}

// Removable marks a unit as removable without touching its desired state.
func Removable(b *specBuild) { b.removalAllowed = true }

// Oneshot makes the unit a run-to-completion job rather than a long-running
// service: desiredState "oneshot" plus [Service] Type=oneshot. Both are set,
// since the validator rejects Type=oneshot on the default desired-running
// container.
func Oneshot(b *specBuild) {
	b.desiredState = model.DesiredStateOneshot
	b.set("Service", "Type", model.UV("oneshot"))
}

// Reclaim sets the reclaim policy, which decides whether the backing podman
// resource is deleted when the unit goes away.
func Reclaim(policy model.ReclaimPolicy) UnitOpt {
	return func(b *specBuild) { b.reclaimPolicy = policy }
}

// Set sets an arbitrary unit-file key. Multiple values produce a repeated key,
// as systemd's multi-value syntax requires.
//
// Named Set rather than Option to leave that name to the Env option type.
func Set(section, key string, values ...string) UnitOpt {
	return func(b *specBuild) { b.set(section, key, model.MultiUV(values...)) }
}

// Files attaches single-file config mounts to a container. Each becomes a
// read-only bind mount, so any change to one costs a restart.
func Files(mounts ...model.ContainerFileMount) ContainerOpt {
	return func(b *specBuild) { b.fileMounts = append(b.fileMounts, mounts...) }
}

// Dirs attaches config-directory mounts to a container.
//
// It also sets [Service] ExecReload=, which the validator requires of any
// container declaring a configDir: a configDir's contents can be re-synced
// under a live container, and that cheap path only exists if the unit can be
// told to reload itself.
func Dirs(dirs ...model.ContainerDirMount) ContainerOpt {
	return func(b *specBuild) {
		b.dirMounts = append(b.dirMounts, dirs...)
		b.set("Service", "ExecReload", model.UV("/bin/kill -HUP $MAINPID"))
	}
}

// WithNetwork adds a Network= reference to the named network unit.
func WithNetwork(name string) ContainerOpt {
	return func(b *specBuild) { b.append("Container", "Network", name+".network") }
}

// WithVolume adds a Volume= reference mounting the named volume unit at path.
func WithVolume(volume, path string) ContainerOpt {
	return func(b *specBuild) { b.append("Container", "Volume", volume+".volume:"+path) }
}

// --- Constructors ---

// NewContainer builds a running container spec. Running is the default because
// it is overwhelmingly the common case; pass Stopped or Stale to vary it.
func NewContainer(name, image string, opts ...ContainerOpt) *model.ContainerUnit {
	b := &specBuild{
		options:      model.UnitOptions{},
		desiredState: model.DesiredStateRunning,
	}
	b.set("Container", "Image", model.UV(image))
	for _, opt := range opts {
		opt(b)
	}
	return model.NewContainerUnitWithDirs(
		model.ContainerUnitRef(name), b.options, b.desiredState,
		b.fileMounts, b.dirMounts, b.removalAllowed)
}

// NewBuiltContainer builds a container whose image comes from a build unit,
// i.e. Image=<build>.build.
func NewBuiltContainer(name, buildName string, opts ...ContainerOpt) *model.ContainerUnit {
	return NewContainer(name, buildName+".build", opts...)
}

// NewVolume builds a volume spec with the given Device=, retaining its backing
// podman volume unless Reclaim says otherwise.
func NewVolume(name, device string, opts ...UnitOpt) *model.VolumeUnit {
	b := &specBuild{options: model.UnitOptions{}, reclaimPolicy: model.ReclaimPolicyRetain}
	b.set("Volume", "Device", model.UV(device))
	for _, opt := range opts {
		opt(b)
	}
	return model.NewVolumeUnit(model.VolumeUnitRef(name), b.options, b.removalAllowed, b.reclaimPolicy)
}

// NewNetwork builds a network spec with the given Driver=.
func NewNetwork(name, driver string, opts ...UnitOpt) *model.NetworkUnit {
	b := &specBuild{options: model.UnitOptions{}, reclaimPolicy: model.ReclaimPolicyRetain}
	if driver != "" {
		b.set("Network", "Driver", model.UV(driver))
	}
	for _, opt := range opts {
		opt(b)
	}
	return model.NewNetworkUnit(model.NetworkUnitRef(name), b.options, b.removalAllowed, b.reclaimPolicy)
}

// NewBuild builds a build spec with the given ImageTag= and a trivial
// Containerfile, which is all the tests that are not about context files need.
func NewBuild(name, imageTag string, opts ...UnitOpt) *model.BuildUnit {
	b := &specBuild{options: model.UnitOptions{}, reclaimPolicy: model.ReclaimPolicyRetain}
	b.set("Build", "ImageTag", model.UV(imageTag))
	for _, opt := range opts {
		opt(b)
	}
	return model.NewBuildUnit(model.BuildUnitRef(name), b.options, "FROM scratch", nil, b.reclaimPolicy)
}
