// Package validate checks quadlet units at multiple stages: pre-render (spec
// correctness), post-render (unit file syntax), and staging (generator output).
package validate

import (
	"fmt"
	"path/filepath"
	"strings"

	"codeberg.org/xchangeee/syslet/internal/model"
	"codeberg.org/xchangeee/syslet/internal/render"
)

// CollectionValidator validates a collection of specs.
type CollectionValidator func([]model.Unit) error

// UnitValidator validates an individual spec.
type UnitValidator func(model.Unit) error

// PreRender performs pre-render validation on raw input data.
func PreRender(specs []model.Unit) error {
	return validateUnits(
		specs,
		[]CollectionValidator{
			NoDuplicateNames,
		},
		[]UnitValidator{
			SpecNameNotEmpty,
			NoXSysletSection,
			ContainerNoDuplicateConfigPaths,
			ContainerConfigMountPaths,
			ContainerConfigDirsRequireExecReload,
			ContainerConfigDirMountPaths,
			ContainerConfigDirFilenames,
			BuildContainerfilePresent,
			BuildConfigFilenames,
			BuildNoDuplicateFilenames,
		},
	)
}

func validateUnits(
	specs []model.Unit,
	collectionValidators []CollectionValidator,
	unitValidators []UnitValidator,
) error {
	for _, v := range collectionValidators {
		if err := v(specs); err != nil {
			return err
		}
	}
	for _, s := range specs {
		for _, v := range unitValidators {
			if err := v(s); err != nil {
				return err
			}
		}
	}
	return nil
}

// NoDuplicateNames ensures no two specs of the same type share the same name.
func NoDuplicateNames(units []model.Unit) error {
	type key struct {
		specType model.UnitType
		name     string
	}
	seen := make(map[key]struct{})
	for _, s := range units {
		k := key{s.Ref().UnitType(), s.Ref().Name()}
		if _, ok := seen[k]; ok {
			return fmt.Errorf("duplicate unit name %q for type %s", k.name, k.specType)
		}
		seen[k] = struct{}{}
	}
	return nil
}

// SpecNameNotEmpty ensures the spec has a non-empty name.
func SpecNameNotEmpty(unit model.Unit) error {
	if unit.Ref().Name() == "" {
		return fmt.Errorf("spec has empty name")
	}
	return nil
}

// NoXSysletSection ensures that the spec does not provide an X-Syslet section.
func NoXSysletSection(unit model.Unit) error {
	for section := range unit.Options() {
		if section == render.SectionXSyslet {
			return fmt.Errorf("%s %q: X-Syslet section is reserved for internal use and cannot be provided in spec", unit.Ref().UnitType(), unit.Ref())
		}
	}
	return nil
}

// ContainerNoDuplicateConfigPaths ensures no two config entries target the same path.
func ContainerNoDuplicateConfigPaths(unit model.Unit) error {
	container, ok := unit.(*model.ContainerUnit)
	if !ok {
		return nil
	}
	seen := make(map[model.ContainerMountPath]bool)
	for _, cfg := range container.FileMounts {
		fp := cfg.FullPath()
		if seen[fp] {
			return fmt.Errorf("container %q: duplicate config mountPath %q", container.Ref(), fp)
		}
		seen[fp] = true
	}
	return nil
}

// ContainerConfigMountPaths ensures each config entry has a valid mount path.
func ContainerConfigMountPaths(unit model.Unit) error {
	container, ok := unit.(*model.ContainerUnit)
	if !ok {
		return nil
	}
	for _, cfg := range container.FileMounts {
		if cfg.Directory == "" {
			return fmt.Errorf("container %q: config mountPath cannot be empty", container.Ref())
		}
		if !filepath.IsAbs(string(cfg.Directory)) {
			return fmt.Errorf("container %q: config mountPath must be absolute: %s", container.Ref(), cfg.FullPath())
		}
		if strings.Contains(string(cfg.Directory), "..") {
			return fmt.Errorf("container %q: config mountPath cannot contain '..': %s", container.Ref(), cfg.Directory)
		}
	}
	return nil
}

// BuildContainerfilePresent ensures the build spec provides a Containerfile.
func BuildContainerfilePresent(unit model.Unit) error {
	build, ok := unit.(*model.BuildUnit)
	if !ok {
		return nil
	}
	if build.Containerfile == "" {
		return fmt.Errorf("build %q: containerfile field is required", build.Ref())
	}
	return nil
}

// BuildNoDuplicateFilenames ensures no two config entries share the same filename.
func BuildNoDuplicateFilenames(unit model.Unit) error {
	build, ok := unit.(*model.BuildUnit)
	if !ok {
		return nil
	}
	seen := map[model.BuildFilePath]bool{"Containerfile": true}
	for _, ce := range build.ContextFiles {
		if seen[ce.Filename] {
			return fmt.Errorf("build %q: duplicate filename: %s", build.Ref(), ce.Filename)
		}
		seen[ce.Filename] = true
	}
	return nil
}

// ContainerConfigDirMountPaths ensures each configDir entry has a valid absolute mount path.
func ContainerConfigDirMountPaths(unit model.Unit) error {
	container, ok := unit.(*model.ContainerUnit)
	if !ok {
		return nil
	}
	for i, cd := range container.DirMounts {
		if !filepath.IsAbs(string(cd.Directory)) {
			return fmt.Errorf("container %q: configDir[%d].mountPath must be absolute, got %q", container.Ref(), i, cd.Directory)
		}
	}
	return nil
}

// ContainerConfigDirFilenames ensures each file within a configDir has a plain filename
// with no path separators.
func ContainerConfigDirFilenames(unit model.Unit) error {
	container, ok := unit.(*model.ContainerUnit)
	if !ok {
		return nil
	}
	for i, cd := range container.DirMounts {
		for j, f := range cd.Files {
			if filepath.Base(f.Name) != f.Name || f.Name == "." {
				return fmt.Errorf("container %q: configDir[%d].files[%d].name must be a plain filename with no path separators, got %q", container.Ref(), i, j, f.Name)
			}
		}
	}
	return nil
}

// ContainerConfigDirsRequireExecReload ensures that any container using configDirs
// has [Service] ExecReload= set, since configDirs imply in-place reload semantics.
func ContainerConfigDirsRequireExecReload(unit model.Unit) error {
	container, ok := unit.(*model.ContainerUnit)
	if !ok || len(container.DirMounts) == 0 {
		return nil
	}
	svc := container.Options()[render.SectionService]
	reloadVal, ok := svc[model.SectionKey("ExecReload")]
	if !ok {
		return fmt.Errorf("unit %s: configDirs require [Service] ExecReload= to be set", container.Ref())
	}
	for _, v := range reloadVal.Values() {
		if v != "" {
			return nil
		}
	}
	return fmt.Errorf("unit %s: configDirs require [Service] ExecReload= to be set", container.Ref())
}

// BuildConfigFilenames ensures each config entry has a valid relative filename.
func BuildConfigFilenames(unit model.Unit) error {
	build, ok := unit.(*model.BuildUnit)
	if !ok {
		return nil
	}
	for _, ce := range build.ContextFiles {
		if ce.Filename == "" {
			return fmt.Errorf("build %q: config filename cannot be empty", build.Ref())
		}
		if filepath.IsAbs(string(ce.Filename)) {
			return fmt.Errorf("build %q: config filename cannot be absolute path: %s", build.Ref(), ce.Filename)
		}
		if strings.Contains(string(ce.Filename), "..") {
			return fmt.Errorf("build %q: config filename cannot contain '..': %s", build.Ref(), ce.Filename)
		}
	}
	return nil
}
