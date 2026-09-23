// Package validate checks quadlet units at multiple stages: pre-render (spec
// correctness), post-render (unit file syntax), and staging (generator output).
package validate

import (
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"codeberg.org/xchangeee/syslet/internal/model"
	"codeberg.org/xchangeee/syslet/internal/render"
)

var secretKeyRe = regexp.MustCompile(`^[a-z0-9-]+$`)

// CollectionValidator validates a collection of specs.
type CollectionValidator func([]model.Unit) error

// UnitValidator validates an individual spec.
type UnitValidator func(model.Unit) error

// PreRender performs pre-render validation on raw input data, including secret
// key name format and container Secret= reference resolution.
func PreRender(units []model.Unit, secrets []model.PodmanSecret) error {
	for _, s := range secrets {
		if err := SecretKeyNames(s); err != nil {
			return err
		}
	}
	return validateUnits(
		units,
		[]CollectionValidator{
			NoDuplicateNames,
			SecretReferences(secrets),
		},
		[]UnitValidator{
			SpecNameNotEmpty,
			NoXSysletSection,
			ContainerConfigMountPaths,
			ContainerConfigDirsRequireExecReload,
			ContainerConfigDirMountPaths,
			ContainerNoOverlappingMountPaths,
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

// ContainerNoOverlappingMountPaths ensures no configFile or configDir mountPath
// equals or sits under another one. Each entry becomes its own bind mount, so an
// overlap lets one mount shadow part of another; for configDirs that would also
// hide files from the atomic symlink swap that in-place reload relies on. It runs
// after the per-entry path validators, so paths are known to be absolute.
func ContainerNoOverlappingMountPaths(unit model.Unit) error {
	container, ok := unit.(*model.ContainerUnit)
	if !ok {
		return nil
	}
	type mount struct {
		label string
		path  string
	}
	var mounts []mount
	for i, fm := range container.FileMounts {
		mounts = append(mounts, mount{fmt.Sprintf("configFiles[%d]", i), path.Clean(string(fm.FullPath()))})
	}
	for i, dm := range container.DirMounts {
		mounts = append(mounts, mount{fmt.Sprintf("configDirs[%d]", i), path.Clean(string(dm.Directory))})
	}
	for i, a := range mounts {
		for _, b := range mounts[i+1:] {
			if pathContains(a.path, b.path) || pathContains(b.path, a.path) {
				return fmt.Errorf("container %q: %s.mountPath %q overlaps %s.mountPath %q",
					container.Ref(), a.label, a.path, b.label, b.path)
			}
		}
	}
	return nil
}

// pathContains reports whether the cleaned absolute path child equals parent
// or lies beneath it. A plain prefix check is not enough: /etc/app2 is not
// under /etc/app.
func pathContains(parent, child string) bool {
	if parent == child || parent == "/" {
		return true
	}
	return strings.HasPrefix(child, parent+"/")
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
		if cd.Directory == "" {
			return fmt.Errorf("container %q: configDir[%d].mountPath cannot be empty", container.Ref(), i)
		}
		if !filepath.IsAbs(string(cd.Directory)) {
			return fmt.Errorf("container %q: configDir[%d].mountPath must be absolute, got %q", container.Ref(), i, cd.Directory)
		}
		if strings.Contains(string(cd.Directory), "..") {
			return fmt.Errorf("container %q: configDir[%d].mountPath cannot contain '..': %s", container.Ref(), i, cd.Directory)
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

// SecretKeyNames validates that all key names in a PodmanSecret match [a-z0-9-]+.
// Keys are plaintext in SOPS YAML so this check runs pre-decryption.
func SecretKeyNames(secret model.PodmanSecret) error {
	for _, k := range secret.Keys {
		if !secretKeyRe.MatchString(k) {
			return fmt.Errorf("secret %q: key %q must match [a-z0-9-]", secret.Name, k)
		}
	}
	return nil
}

// SecretReferences returns a CollectionValidator that checks every container
// Secret= option against the known set of secret specs and their keys.
// Runs pre-decryption: key names are plaintext in SOPS YAML.
func SecretReferences(secrets []model.PodmanSecret) CollectionValidator {
	// valid holds every legal "specname-key" string for O(1) lookup.
	valid := make(map[string]bool)
	// specNames is used only in the error path to distinguish "spec absent" from "key absent".
	specNames := make(map[string]bool)
	for _, s := range secrets {
		specNames[s.Name] = true
		for _, k := range s.Keys {
			valid[s.Name+"-"+k] = true
		}
	}

	return func(units []model.Unit) error {
		for _, unit := range units {
			container, ok := unit.(*model.ContainerUnit)
			if !ok {
				continue
			}
			secretRefs, ok := container.Options()[render.SectionContainer][model.SectionKey("Secret")]
			if !ok {
				continue
			}
			for _, ref := range secretRefs.Values() {
				secretName := ref
				if before, _, ok := strings.Cut(ref, ","); ok {
					secretName = before
				}
				if valid[secretName] {
					continue
				}
				// Determine whether a spec name is a prefix (key absent) or not (spec absent).
				for specName := range specNames {
					if strings.HasPrefix(secretName, specName+"-") {
						key := secretName[len(specName)+1:]
						return fmt.Errorf("container %q: Secret=%s: key %q not found in secret %q", container.Ref(), ref, key, specName)
					}
				}
				return fmt.Errorf("container %q: Secret=%s: no SecretSpec found for this reference", container.Ref(), ref)
			}
		}
		return nil
	}
}
