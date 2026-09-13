// Package render converts domain model units into systemd quadlet unit file content.
package render

import (
	"fmt"
	"strconv"

	"codeberg.org/xchangeee/syslet/internal/model"
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

// RenderResult holds the output of Render, grouped by unit type.
type RenderResult struct {
	Containers []RenderedUnit
	Volumes    []RenderedUnit
	Networks   []RenderedUnit
	Builds     []RenderedUnit
	// UnitNames is the set of full unit names (e.g. "foo.container") present in the result.
	UnitNames map[model.FullUnitName]bool
}

func Render(units []model.Unit, configResolve ContainerResolverFactory, buildResolve BuildResolverFactory) (RenderResult, error) {
	var result RenderResult
	result.UnitNames = make(map[model.FullUnitName]bool)
	for _, unit := range units {
		var r RenderedUnit
		var err error
		switch unit.Ref().UnitType() {
		case model.UnitTypeContainer:
			r, err = RenderContainer(unit.(*model.ContainerUnit), configResolve)
			if err != nil {
				return RenderResult{}, fmt.Errorf("rendering %s: %w", unit.Ref(), err)
			}
			result.Containers = append(result.Containers, r)
		case model.UnitTypeVolume:
			r, err = RenderVolume(unit.(*model.VolumeUnit))
			if err != nil {
				return RenderResult{}, fmt.Errorf("rendering %s: %w", unit.Ref(), err)
			}
			result.Volumes = append(result.Volumes, r)
		case model.UnitTypeNetwork:
			r, err = RenderNetwork(unit.(*model.NetworkUnit))
			if err != nil {
				return RenderResult{}, fmt.Errorf("rendering %s: %w", unit.Ref(), err)
			}
			result.Networks = append(result.Networks, r)
		case model.UnitTypeBuild:
			r, err = RenderBuild(unit.(*model.BuildUnit), buildResolve)
			if err != nil {
				return RenderResult{}, fmt.Errorf("rendering %s: %w", unit.Ref(), err)
			}
			result.Builds = append(result.Builds, r)
		}
		result.UnitNames[unit.Ref().FullName()] = true
	}
	return result, nil
}

// RenderContainer converts a ContainerUnit into a RenderedUnit.
// Volume= entries for file and directory mounts are emitted here using resolveFor to obtain
// the host-side path; the mount data itself remains on the unit for plan builders to access.
func RenderContainer(unit *model.ContainerUnit, resolveFor ContainerResolverFactory) (RenderedUnit, error) {
	r := RenderUnit(unit)
	r.Default(SectionUnit, KeyUnitDescription, unit.Ref().Name()+" container")
	r.Override(SectionContainer, KeyContainerName, unit.Ref().Name())
	r.Override(SectionXSyslet, KeyXSysletRemovalAllowed, strconv.FormatBool(unit.RemovalAllowed))
	switch unit.DesiredState {
	case model.DesiredStateRunning:
		r.Override(SectionService, KeyServiceRestart, ServiceRestartAlways)
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

// RenderVolume converts a VolumeUnit into a RenderedUnit.
func RenderVolume(unit *model.VolumeUnit) (RenderedUnit, error) {
	r := RenderUnit(unit)
	r.Override(SectionVolume, KeyVolumeName, unit.Ref().Name())
	r.Override(SectionXSyslet, KeyXSysletRemovalAllowed, strconv.FormatBool(unit.RemovalAllowed))
	r.Override(SectionXSyslet, KeyXSysletReclaimPolicy, string(unit.ReclaimPolicy))
	return r.RenderedUnit()
}

// RenderNetwork converts a NetworkUnit into a RenderedUnit.
func RenderNetwork(unit *model.NetworkUnit) (RenderedUnit, error) {
	r := RenderUnit(unit)
	r.Override(SectionNetwork, KeyNetworkName, unit.Ref().Name())
	r.Override(SectionXSyslet, KeyXSysletRemovalAllowed, strconv.FormatBool(unit.RemovalAllowed))
	r.Override(SectionXSyslet, KeyXSysletReclaimPolicy, string(unit.ReclaimPolicy))
	return r.RenderedUnit()
}

// RenderBuild converts a BuildUnit into a RenderedUnit.
// Only the unit file (Build= path, ReclaimPolicy) is produced here; the Containerfile and
// context files remain on the unit for the plan builder to handle uniformly.
func RenderBuild(unit *model.BuildUnit, resolveFor BuildResolverFactory) (RenderedUnit, error) {
	r := RenderUnit(unit)
	r.Override(SectionBuild, KeyBuildFile, resolveFor(unit.Ref().Name(), "Containerfile"))
	r.Override(SectionXSyslet, KeyXSysletReclaimPolicy, string(unit.ReclaimPolicy))
	return r.RenderedUnit()
}
