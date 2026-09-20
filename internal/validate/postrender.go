package validate

import (
	"fmt"
	"strings"

	"codeberg.org/xchangeee/syslet/internal/model"
	"codeberg.org/xchangeee/syslet/internal/render"
)

// PostRender performs post-render validation on rendered unit options.
func PostRender(result render.Result) error {
	if err := validateContainerVolumeReferencesExist(result.Containers, result.Volumes); err != nil {
		return err
	}
	if err := validateContainerNetworkReferencesExist(result.Containers, result.Networks); err != nil {
		return err
	}
	if err := validateContainerBuildReferencesExist(result.Containers, result.Builds); err != nil {
		return err
	}
	if err := validateContainerNoConflictingVolumeDstPaths(result.Containers); err != nil {
		return err
	}
	if err := validateBuildImageTagsUseLocalhostRegistry(result.Builds); err != nil {
		return err
	}
	return nil
}

func validateContainerVolumeReferencesExist(containers, volumes []render.RenderedUnit) error {
	names := make(map[string]bool)
	for _, v := range volumes {
		names[render.FindOptValue(v.UnitOptions, render.SectionVolume, render.KeyVolumeName)] = true
	}
	return validateContainerRefs(containers, render.KeyContainerVolume, parseVolumeReference, names, "volume")
}

func validateContainerNetworkReferencesExist(containers, networks []render.RenderedUnit) error {
	names := make(map[string]bool)
	for _, n := range networks {
		names[render.FindOptValue(n.UnitOptions, render.SectionNetwork, render.KeyNetworkName)] = true
	}
	return validateContainerRefs(containers, render.KeyContainerNetwork, parseNetworkReference, names, "network")
}

func validateContainerBuildReferencesExist(containers, builds []render.RenderedUnit) error {
	names := make(map[string]bool)
	for _, b := range builds {
		names[b.Unit.Ref().Name()] = true
	}
	return validateContainerRefs(containers, render.KeyContainerImage, parseBuildReference, names, "build")
}

func validateContainerNoConflictingVolumeDstPaths(containers []render.RenderedUnit) error {
	for _, c := range containers {
		container, ok := c.Unit.(*model.ContainerUnit)
		if !ok {
			continue
		}
		dstPaths := make(map[string]string)
		for _, val := range render.FindOptValues(c.UnitOptions, render.SectionContainer, render.KeyContainerVolume) {
			parts := strings.SplitN(val, ":", 3)
			if len(parts) < 2 {
				continue
			}
			src, dst := parts[0], parts[1]
			if existing, ok := dstPaths[dst]; ok {
				return fmt.Errorf("container %q: volume destination conflict: %q used by both %q and %q", container.Ref(), dst, existing, src)
			}
			dstPaths[dst] = src
		}
	}
	return nil
}

func validateBuildImageTagsUseLocalhostRegistry(builds []render.RenderedUnit) error {
	for _, b := range builds {
		buildName := b.Unit.Ref().Name()
		imageTag := render.FindOptValue(b.UnitOptions, render.SectionBuild, render.KeyBuildImageTag)
		if imageTag == "" {
			return fmt.Errorf("build %q: ImageTag is required in Build section", buildName)
		}
		expectedPrefix := fmt.Sprintf("localhost/%s:", buildName)
		if !strings.HasPrefix(imageTag, expectedPrefix) {
			return fmt.Errorf("build %q: ImageTag must start with %q, got %q", buildName, expectedPrefix, imageTag)
		}
	}
	return nil
}

func parseVolumeReference(value string) string {
	parts := strings.SplitN(value, ":", 2)
	if len(parts) < 2 {
		return ""
	}
	return parseSuffixReference(parts[0], "volume")
}

func parseNetworkReference(value string) string {
	return parseSuffixReference(value, "network")
}

func parseBuildReference(value string) string {
	return parseSuffixReference(value, "build")
}

// parseSuffixReference strips a dotted suffix (e.g. ".volume", ".network") from value.
// Returns "" if the suffix is not present.
func parseSuffixReference(value, suffix string) string {
	if before, ok := strings.CutSuffix(value, "."+suffix); ok {
		return before
	}
	return ""
}

// validateContainerRefs checks that every resource reference found in containers
// (via containerKey values parsed by parseRef) exists in names.
func validateContainerRefs(containers []render.RenderedUnit, containerKey model.SectionKey, parseRef func(string) string, names map[string]bool, resourceType string) error {
	for _, c := range containers {
		container, ok := c.Unit.(*model.ContainerUnit)
		if !ok {
			continue
		}
		for _, val := range render.FindOptValues(c.UnitOptions, render.SectionContainer, containerKey) {
			ref := parseRef(val)
			if ref != "" && !names[ref] {
				return fmt.Errorf("container %q: references undefined %s %q", container.Ref(), resourceType, ref)
			}
		}
	}
	return nil
}
