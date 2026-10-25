// Package render converts domain model units into systemd quadlet unit file content.
package render

import (
	"fmt"
	"strconv"

	"github.com/xchangeee/syslet/internal/model"
)

// volumeMount returns a read-only Volume= value for a quadlet container unit.
func volumeMount(hostPath string, containerPath model.ContainerMountPath) string {
	return fmt.Sprintf("%s:%s:ro,Z", hostPath, containerPath)
}

// ContainerResolverFactory maps a container unit ref and mount path to its full host-disk path.
// The implementation is responsible for deriving the internal filename from the mount path
// (e.g. via BasenameWithHashSuffix); render only passes the container-side path through.
type ContainerResolverFactory func(ref model.ContainerUnitRef, dest model.ContainerMountPath) string

// BuildResolverFactory maps a build name and context filename to its full host-disk path.
type BuildResolverFactory func(buildName, filename string) string

// Result holds the output of Render, grouped by unit type.
type Result struct {
	Containers []RenderedUnit
	Volumes    []RenderedUnit
	Networks   []RenderedUnit
	Builds     []RenderedUnit
	// UnitNames is the set of full unit names (e.g. "foo.container") present in the result.
	UnitNames map[model.FullUnitName]bool
}

// HostCapabilities lists the version-dependent systemd features the target
// host supports. The planner derives it from the host's systemd version via
// CapabilitiesForSystemd before rendering; render picks options accordingly,
// since staging validation fails on any option the host's systemd doesn't know.
type HostCapabilities struct {
	// ExponentialRestartBackoff reports support for RestartSteps= and
	// RestartMaxDelaySec=, added in systemd 254.
	ExponentialRestartBackoff bool
}

// CapabilitiesForSystemd maps a systemd major version to the features syslet
// can render for it. A version of 0 stands for "unknown" and yields the most
// conservative set.
func CapabilitiesForSystemd(major int) HostCapabilities {
	return HostCapabilities{ExponentialRestartBackoff: major >= 254}
}

func NewResult(units []model.Unit, configResolve ContainerResolverFactory, buildResolve BuildResolverFactory, host HostCapabilities) (Result, error) {
	var result Result
	result.UnitNames = make(map[model.FullUnitName]bool)
	for _, unit := range units {
		var r RenderedUnit
		var err error
		switch unit.Ref().UnitType() {
		case model.UnitTypeContainer:
			r, err = NewRenderedUnitFromContainer(unit.(*model.ContainerUnit), configResolve, host)
			if err != nil {
				return Result{}, fmt.Errorf("rendering %s: %w", unit.Ref(), err)
			}
			result.Containers = append(result.Containers, r)
		case model.UnitTypeVolume:
			r, err = NewRenderedUnitFromVolume(unit.(*model.VolumeUnit))
			if err != nil {
				return Result{}, fmt.Errorf("rendering %s: %w", unit.Ref(), err)
			}
			result.Volumes = append(result.Volumes, r)
		case model.UnitTypeNetwork:
			r, err = NewRenderedUnitFromNetwork(unit.(*model.NetworkUnit))
			if err != nil {
				return Result{}, fmt.Errorf("rendering %s: %w", unit.Ref(), err)
			}
			result.Networks = append(result.Networks, r)
		case model.UnitTypeBuild:
			r, err = NewRenderedUnitFromBuild(unit.(*model.BuildUnit), buildResolve)
			if err != nil {
				return Result{}, fmt.Errorf("rendering %s: %w", unit.Ref(), err)
			}
			result.Builds = append(result.Builds, r)
		}
		result.UnitNames[unit.Ref().FullName()] = true
	}
	return result, nil
}

// NewRenderedUnitFromContainer converts a ContainerUnit into a RenderedUnit.
// Volume= entries for file and directory mounts are emitted here using resolveFor to obtain
// the host-side path; the mount data itself remains on the unit for plan builders to access.
// host decides which restart backoff options the unit gets.
func NewRenderedUnitFromContainer(unit *model.ContainerUnit, resolveFor ContainerResolverFactory, host HostCapabilities) (RenderedUnit, error) {
	r := NewUnitRenderer(unit)
	r.Default(SectionUnit, KeyUnitDescription, unit.Ref().Name()+" container")
	r.Override(SectionContainer, KeyContainerName, unit.Ref().Name())
	r.Override(SectionXSyslet, KeyXSysletRemovalAllowed, strconv.FormatBool(unit.RemovalAllowed))
	switch unit.DesiredState {
	case model.DesiredStateRunning:
		r.Override(SectionService, KeyServiceRestart, ServiceRestartAlways)
		// Restart=always alone retries every 100ms and hits systemd's start limit
		// after five crashes, leaving the unit failed. These defaults back off
		// instead and keep retrying; a spec can still set its own values. They
		// are restart-neutral (see StripRestartNeutralOptions), so changing them,
		// e.g. when the host's systemd is upgraded, never restarts a running
		// container. Without RestartSteps= support a fixed delay is used instead.
		if host.ExponentialRestartBackoff {
			r.Default(SectionService, KeyServiceRestartSec, ServiceRestartSecDefault)
			r.Default(SectionService, KeyServiceRestartSteps, ServiceRestartStepsDefault)
			r.Default(SectionService, KeyServiceRestartMaxDelaySec, ServiceRestartMaxDelaySecDefault)
		} else {
			r.Default(SectionService, KeyServiceRestartSec, ServiceRestartSecFixed)
		}
		r.Default(SectionUnit, KeyUnitStartLimitIntervalSec, UnitStartLimitIntervalSecDefault)
		r.Override(SectionInstall, KeyInstallWantedBy, InstallMultiUserTarget)
	case model.DesiredStateOneshot:
		r.Override(SectionService, KeyServiceType, ServiceTypeOneshot)
	}
	for _, fileMount := range unit.FileMounts {
		r.Append(SectionContainer, KeyContainerVolume,
			volumeMount(resolveFor(unit.TypedUnitRef(), fileMount.FullPath()), fileMount.FullPath()))
	}
	for _, cd := range unit.DirMounts {
		r.Append(SectionContainer, KeyContainerVolume,
			volumeMount(resolveFor(unit.TypedUnitRef(), cd.Directory), cd.Directory))
	}
	return r.RenderedUnit()
}

// NewRenderedUnitFromVolume converts a VolumeUnit into a RenderedUnit.
func NewRenderedUnitFromVolume(unit *model.VolumeUnit) (RenderedUnit, error) {
	r := NewUnitRenderer(unit)
	r.Override(SectionVolume, KeyVolumeName, unit.Ref().Name())
	r.Override(SectionXSyslet, KeyXSysletRemovalAllowed, strconv.FormatBool(unit.RemovalAllowed))
	r.Override(SectionXSyslet, KeyXSysletReclaimPolicy, string(unit.ReclaimPolicy))
	return r.RenderedUnit()
}

// NewRenderedUnitFromNetwork converts a NetworkUnit into a RenderedUnit.
func NewRenderedUnitFromNetwork(unit *model.NetworkUnit) (RenderedUnit, error) {
	r := NewUnitRenderer(unit)
	r.Override(SectionNetwork, KeyNetworkName, unit.Ref().Name())
	r.Override(SectionXSyslet, KeyXSysletRemovalAllowed, strconv.FormatBool(unit.RemovalAllowed))
	r.Override(SectionXSyslet, KeyXSysletReclaimPolicy, string(unit.ReclaimPolicy))
	return r.RenderedUnit()
}

// NewRenderedUnitFromBuild converts a BuildUnit into a RenderedUnit.
// Only the unit file (Build= path, ReclaimPolicy) is produced here; the Containerfile and
// context files remain on the unit for the plan builder to handle uniformly.
func NewRenderedUnitFromBuild(unit *model.BuildUnit, resolveFor BuildResolverFactory) (RenderedUnit, error) {
	r := NewUnitRenderer(unit)
	r.Override(SectionBuild, KeyBuildFile, resolveFor(unit.Ref().Name(), "Containerfile"))
	r.Override(SectionXSyslet, KeyXSysletReclaimPolicy, string(unit.ReclaimPolicy))
	return r.RenderedUnit()
}
