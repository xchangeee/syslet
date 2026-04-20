package api

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ValidateSpecs performs pre-render validation on raw input data.
// This checks the spec data before any rendering or unit generation happens.
// It ensures the input specs are well-formed and internally consistent.
func ValidateSpecs(specs []Spec) error {
	if err := validateSpecNamesNotEmpty(specs); err != nil {
		return err
	}
	if err := validateNoDuplicateNames(specs); err != nil {
		return err
	}
	if err := validateNoDuplicateConfigPaths(specs); err != nil {
		return err
	}
	if err := validateDesiredState(specs); err != nil {
		return err
	}
	if err := validateNoXSysletSection(specs); err != nil {
		return err
	}
	if err := validateBuildSpecs(specs); err != nil {
		return err
	}
	return nil
}

// ValidateRenderedUnits performs post-render validation on rendered unit options.
// This checks that cross-references between specs are valid after flattening
// unit options. It verifies that volumes, networks, and builds referenced by containers
// actually exist, and that there are no conflicting volume mount paths.
// Uses pre-rendered unit options to avoid re-flattening during validation.
func ValidateRenderedUnits(containers, volumes, networks, builds []RenderedUnit) error {
	if err := validateVolumeReferences(containers, volumes); err != nil {
		return err
	}
	if err := validateNetworkReferences(containers, networks); err != nil {
		return err
	}
	if err := validateBuildReferences(containers, builds); err != nil {
		return err
	}
	if err := validateNoVolumeConflicts(containers); err != nil {
		return err
	}
	if err := validateBuildImageTags(builds); err != nil {
		return err
	}
	return nil
}

// validateSpecNamesNotEmpty ensures all specs have non-empty names.
// This is a pre-render check on the raw input data.
func validateSpecNamesNotEmpty(specs []Spec) error {
	for _, s := range specs {
		if s.GetName() == "" {
			return fmt.Errorf("spec has empty name")
		}
	}
	return nil
}

// validateNoDuplicateNames ensures no two specs of the same type share the same
// name. Names are scoped per type, so the same name may appear across different
// unit types without conflict.
// This is a pre-render check on the raw input data.
func validateNoDuplicateNames(specs []Spec) error {
	type key struct {
		specType SpecType
		name     string
	}
	seen := make(map[key]struct{})
	for _, s := range specs {
		k := key{s.GetType(), s.GetName()}
		if _, ok := seen[k]; ok {
			return fmt.Errorf("duplicate unit name %q for type %s", k.name, k.specType)
		}
		seen[k] = struct{}{}
	}
	return nil
}

// validateNoDuplicateConfigPaths ensures that within each container,
// no two config entries target the same path. Duplicate target paths
// would cause bind mount conflicts.
// This is a pre-render check on the raw input data.
func validateNoDuplicateConfigPaths(specs []Spec) error {
	for _, s := range specs {
		container, ok := s.(*ContainerSpec)
		if !ok {
			continue
		}
		configPaths := make(map[string]bool)
		for _, cfg := range container.Configs {
			if configPaths[cfg.MountPath] {
				return fmt.Errorf("container %q: duplicate config mountPath %q", container.Name, cfg.MountPath)
			}
			configPaths[cfg.MountPath] = true
		}
	}
	return nil
}

// validateDesiredState ensures that desiredState field only contains valid values.
// Valid values are: "running", "stopped", or empty string (field is optional).
// This prevents unexpected values that could lead to undefined behavior.
// This is a pre-render check on the raw input data.
func validateDesiredState(specs []Spec) error {
	for _, s := range specs {
		container, ok := s.(*ContainerSpec)
		if !ok {
			continue
		}
		// Empty string is valid (field is optional, omitempty in JSON)
		if container.DesiredState == "" {
			continue
		}
		// Convert to lowercase for case-insensitive comparison
		state := strings.ToLower(container.DesiredState)
		if state != "running" && state != "stopped" {
			return fmt.Errorf("container %q: invalid desiredState %q (must be \"running\", \"stopped\", or omitted)", container.Name, container.DesiredState)
		}
	}
	return nil
}

// validateNoXSysletSection ensures that users don't provide X-Syslet section in their unit maps.
// The X-Syslet section is internal metadata managed by syslet and should not be user-provided.
// This is a pre-render check on the raw input data.
func validateNoXSysletSection(specs []Spec) error {
	for _, s := range specs {
		unitMap := s.GetUnit()
		for section := range unitMap {
			if section == "X-Syslet" {
				return fmt.Errorf("%s %q: X-Syslet section is reserved for internal use and cannot be provided in spec", s.GetType(), s.GetName())
			}
		}
	}
	return nil
}

// validateVolumeReferences checks that all volumes referenced by containers
// (via Volume= entries in the Container section) exist as volume specs.
// This is a post-render check that uses pre-rendered unit options.
func validateVolumeReferences(containers, volumes []RenderedUnit) error {
	volumeNames := make(map[string]bool)
	for _, v := range volumes {
		volumeNames[v.Spec.GetName()] = true
	}

	for _, c := range containers {
		container, ok := c.Spec.(*ContainerSpec)
		if !ok {
			continue
		}
		for _, o := range c.UnitOptions {
			if o.Section == "Container" && o.Name == "Volume" {
				volRef := parseVolumeReference(o.Value)
				if volRef != "" && !volumeNames[volRef] {
					return fmt.Errorf("container %q: references undefined volume %q", container.Name, volRef)
				}
			}
		}
	}
	return nil
}

// validateNetworkReferences checks that all networks referenced by containers
// (via Network= entries in the Container section) exist as network specs.
// This is a post-render check that uses pre-rendered unit options.
func validateNetworkReferences(containers, networks []RenderedUnit) error {
	networkNames := make(map[string]bool)
	for _, n := range networks {
		networkNames[n.Spec.GetName()] = true
	}

	for _, c := range containers {
		container, ok := c.Spec.(*ContainerSpec)
		if !ok {
			continue
		}
		for _, o := range c.UnitOptions {
			if o.Section == "Container" && o.Name == "Network" {
				netRef := parseNetworkReference(o.Value)
				if netRef != "" && !networkNames[netRef] {
					return fmt.Errorf("container %q: references undefined network %q", container.Name, netRef)
				}
			}
		}
	}
	return nil
}

// validateNoVolumeConflicts checks that within each container, there are no
// conflicting volume mount paths (e.g., same destination used multiple times).
// This is a post-render check that examines the pre-rendered Volume= entries.
func validateNoVolumeConflicts(containers []RenderedUnit) error {
	for _, c := range containers {
		container, ok := c.Spec.(*ContainerSpec)
		if !ok {
			continue
		}

		// Track both source and destination paths to detect conflicts.
		dstPaths := make(map[string]string) // dst → source that uses it
		for _, o := range c.UnitOptions {
			if o.Section == "Container" && o.Name == "Volume" {
				parts := strings.SplitN(o.Value, ":", 3)
				if len(parts) < 2 {
					continue
				}
				src := parts[0]
				dst := parts[1]

				if existing, ok := dstPaths[dst]; ok {
					return fmt.Errorf("container %q: volume destination conflict: %q used by both %q and %q", container.Name, dst, existing, src)
				}
				dstPaths[dst] = src
			}
		}
	}
	return nil
}

// parseVolumeReference extracts the volume name from a Volume= entry.
// Returns the volume name (without extension) if the source references a
// named volume (e.g. "webapp-data.volume:/data" → "webapp-data").
// Returns "" for host-path bind mounts (e.g. "/host/path:/container/path").
func parseVolumeReference(value string) string {
	parts := strings.SplitN(value, ":", 2)
	if len(parts) < 2 {
		return ""
	}
	source := parts[0]
	if strings.HasSuffix(source, ".volume") {
		return strings.TrimSuffix(source, ".volume")
	}
	return ""
}

// parseNetworkReference extracts the network name from a Network= entry.
// Returns the network name (without extension) if it references a named
// network (e.g. "webapp-net.network" → "webapp-net").
// Returns "" otherwise.
func parseNetworkReference(value string) string {
	if strings.HasSuffix(value, ".network") {
		return strings.TrimSuffix(value, ".network")
	}
	return ""
}

// parseBuildReference extracts the build name from an Image= entry.
// Returns the build name (without extension) if it references a build unit
// (e.g. "myapp.build" → "myapp").
// Returns "" otherwise.
func parseBuildReference(value string) string {
	if strings.HasSuffix(value, ".build") {
		return strings.TrimSuffix(value, ".build")
	}
	return ""
}

// validateBuildSpecs performs build-specific pre-render validation.
// Checks that Containerfile is present, filenames are valid, and no duplicates exist.
func validateBuildSpecs(specs []Spec) error {
	for _, s := range specs {
		build, ok := s.(*BuildSpec)
		if !ok {
			continue
		}

		// Containerfile is required
		if build.Containerfile == "" {
			return fmt.Errorf("build %q: containerfile field is required", build.Name)
		}

		// Validate config filenames (no absolute paths, no ..)
		for _, ce := range build.Configs {
			if ce.Filename == "" {
				return fmt.Errorf("build %q: config filename cannot be empty", build.Name)
			}
			if filepath.IsAbs(ce.Filename) {
				return fmt.Errorf("build %q: config filename cannot be absolute path: %s", build.Name, ce.Filename)
			}
			if strings.Contains(ce.Filename, "..") {
				return fmt.Errorf("build %q: config filename cannot contain '..': %s", build.Name, ce.Filename)
			}
		}

		// Validate no duplicate filenames (including Containerfile)
		filenames := map[string]bool{"Containerfile": true}
		for _, ce := range build.Configs {
			if filenames[ce.Filename] {
				return fmt.Errorf("build %q: duplicate filename: %s", build.Name, ce.Filename)
			}
			filenames[ce.Filename] = true
		}
	}
	return nil
}

// validateBuildReferences checks that all builds referenced by containers
// (via Image= entries pointing to .build units) exist as build specs.
// This is a post-render check that uses pre-rendered unit options.
func validateBuildReferences(containers, builds []RenderedUnit) error {
	buildNames := make(map[string]bool)
	for _, b := range builds {
		buildNames[b.Spec.GetName()] = true
	}

	for _, c := range containers {
		container, ok := c.Spec.(*ContainerSpec)
		if !ok {
			continue
		}
		for _, o := range c.UnitOptions {
			if o.Section == "Container" && o.Name == "Image" {
				buildRef := parseBuildReference(o.Value)
				if buildRef != "" && !buildNames[buildRef] {
					return fmt.Errorf("container %q: references undefined build %q", container.Name, buildRef)
				}
			}
		}
	}
	return nil
}

// validateBuildImageTags checks that all build units have a required ImageTag
// and that it starts with the expected prefix "localhost/{buildName}:".
// This is a post-render check that uses pre-rendered unit options.
func validateBuildImageTags(builds []RenderedUnit) error {
	for _, b := range builds {
		buildName := b.Spec.GetName()

		// ImageTag is required
		imageTag := ""
		for _, opt := range b.UnitOptions {
			if opt.Section == "Build" && opt.Name == "ImageTag" {
				imageTag = opt.Value
				break
			}
		}

		if imageTag == "" {
			return fmt.Errorf("build %q: ImageTag is required in Build section", buildName)
		}

		// ImageTag must start with localhost/{buildName}:
		expectedPrefix := fmt.Sprintf("localhost/%s:", buildName)
		if !strings.HasPrefix(imageTag, expectedPrefix) {
			return fmt.Errorf("build %q: ImageTag must start with %q, got %q",
				buildName, expectedPrefix, imageTag)
		}
	}
	return nil
}
