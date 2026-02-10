package api

import (
	"fmt"
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
	return nil
}

// ValidateRenderedUnits performs post-render validation on rendered unit options.
// This checks that cross-references between specs are valid after flattening
// unit options. It verifies that volumes and networks referenced by containers
// actually exist, and that there are no conflicting volume mount paths.
// Uses pre-rendered unit options to avoid re-flattening during validation.
func ValidateRenderedUnits(containers, volumes, networks []RenderedUnit) error {
	if err := validateVolumeReferences(containers, volumes); err != nil {
		return err
	}
	if err := validateNetworkReferences(containers, networks); err != nil {
		return err
	}
	if err := validateNoVolumeConflicts(containers); err != nil {
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

// validateNoDuplicateNames ensures no two specs share the same name,
// regardless of type. Each unit must have a globally unique name.
// This is a pre-render check on the raw input data.
func validateNoDuplicateNames(specs []Spec) error {
	allNames := make(map[string]SpecType)
	for _, s := range specs {
		name := s.GetName()
		specType := s.GetType()
		if existing, ok := allNames[name]; ok {
			return fmt.Errorf("duplicate unit name %q (type %s and %s)", name, existing, specType)
		}
		allNames[name] = specType
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
			if configPaths[cfg.TargetVolumePath] {
				return fmt.Errorf("container %q: duplicate config targetVolumePath %q", container.Name, cfg.TargetVolumePath)
			}
			configPaths[cfg.TargetVolumePath] = true
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
