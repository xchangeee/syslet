// Package loader reads spec files from disk or zip archives and converts them
// into domain objects. It bridges the api (raw JSON) and model (domain) packages.
package loader

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"codeberg.org/xchangeee/syslet/internal/api"
	"codeberg.org/xchangeee/syslet/internal/model"
)

// convert transforms a LoadResult of raw specs into a flat slice of domain Spec values.
func Parse(raw api.LoadResult) ([]model.Unit, error) {
	var specs []model.Unit
	for _, r := range raw.Containers {
		s, err := convertContainer(r)
		if err != nil {
			return nil, fmt.Errorf("container %q: %w", r.Name, err)
		}
		specs = append(specs, s)
	}
	for _, r := range raw.Volumes {
		s, err := convertVolume(r)
		if err != nil {
			return nil, fmt.Errorf("volume %q: %w", r.Name, err)
		}
		specs = append(specs, s)
	}
	for _, r := range raw.Networks {
		s, err := convertNetwork(r)
		if err != nil {
			return nil, fmt.Errorf("network %q: %w", r.Name, err)
		}
		specs = append(specs, s)
	}
	for _, r := range raw.Builds {
		s, err := convertBuild(r)
		if err != nil {
			return nil, fmt.Errorf("build %q: %w", r.Name, err)
		}
		specs = append(specs, s)
	}
	return specs, nil
}

func convertContainer(r api.RawContainerSpec) (*model.ContainerUnit, error) {
	opts, err := convertUnitOptions(r.Unit)
	if err != nil {
		return nil, err
	}
	desiredState, err := parseDesiredState(r.DesiredState)
	if err != nil {
		return nil, err
	}
	configFiles, err := convertConfigFiles(r.Configs)
	if err != nil {
		return nil, err
	}
	configDirs, err := convertConfigDirs(r.ConfigDirs)
	if err != nil {
		return nil, err
	}
	return model.NewContainerUnitWithDirs(
		model.ContainerUnitRef(r.Name),
		opts,
		desiredState,
		configFiles,
		configDirs,
		r.RemovalAllowed,
	), nil
}

func convertVolume(r api.RawVolumeSpec) (*model.VolumeUnit, error) {
	opts, err := convertUnitOptions(r.Unit)
	if err != nil {
		return nil, err
	}
	reclaimPolicy, err := parseReclaimPolicy(r.ReclaimPolicy)
	if err != nil {
		return nil, err
	}
	return model.NewVolumeUnit(
		model.VolumeUnitRef(r.Name),
		opts,
		r.RemovalAllowed,
		reclaimPolicy,
	), nil
}

func convertNetwork(r api.RawNetworkSpec) (*model.NetworkUnit, error) {
	opts, err := convertUnitOptions(r.Unit)
	if err != nil {
		return nil, err
	}
	reclaimPolicy, err := parseReclaimPolicy(r.ReclaimPolicy)
	if err != nil {
		return nil, err
	}
	return model.NewNetworkUnit(
		model.NetworkUnitRef(r.Name),
		opts,
		r.RemovalAllowed,
		reclaimPolicy,
	), nil
}

func convertBuild(r api.RawBuildSpec) (*model.BuildUnit, error) {
	opts, err := convertUnitOptions(r.Unit)
	if err != nil {
		return nil, err
	}
	reclaimPolicy, err := parseReclaimPolicy(r.ReclaimPolicy)
	if err != nil {
		return nil, err
	}
	configs, err := convertBuildFiles(r.Configs)
	if err != nil {
		return nil, err
	}
	return model.NewBuildUnit(
		model.BuildUnitRef(r.Name),
		opts,
		r.Containerfile,
		configs,
		reclaimPolicy,
	), nil
}

// convertUnitOptions converts the raw map[string]map[string]any from JSON
// into model.UnitOptions, validating that all values are strings or string arrays.
func convertUnitOptions(raw map[string]map[string]any) (model.UnitOptions, error) {
	if raw == nil {
		return nil, nil
	}
	opts := make(model.UnitOptions, len(raw))
	for section, keys := range raw {
		opts[model.SectionName(section)] = make(map[model.SectionKey]model.UnitValue, len(keys))
		for key, val := range keys {
			uv, err := model.UnitValueFromRaw(val)
			if err != nil {
				return nil, fmt.Errorf("[%s] %s: %w", section, key, err)
			}
			opts[model.SectionName(section)][model.SectionKey(key)] = uv
		}
	}
	return opts, nil
}

// parseMode parses an octal mode string (e.g. "0755"). Empty string returns 0 (default 0644 via Perm()).
func parseMode(s string) (os.FileMode, error) {
	if s == "" {
		return 0, nil
	}
	// Strip leading zeros and parse as octal.
	v, err := strconv.ParseUint(s, 8, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid octal mode %q: %w", s, err)
	}
	return os.FileMode(v), nil
}

// perm returns mode, or 0644 when mode is zero (the default for user-facing config files).
func perm(mode os.FileMode) os.FileMode {
	if mode == 0 {
		return 0644
	}
	return mode
}

func convertConfigFiles(raw []api.RawConfigEntry) ([]model.ContainerFileMount, error) {
	configs := make([]model.ContainerFileMount, len(raw))
	for i, c := range raw {
		mode, err := parseMode(c.Mode)
		if err != nil {
			return nil, fmt.Errorf("config[%d].mode: %w", i, err)
		}
		configs[i] = model.NewContainerFileMount(c.MountPath, c.Content, perm(mode))
	}
	return configs, nil
}

func convertBuildFiles(raw []api.RawBuildFileEntry) ([]model.BuildContextFile, error) {
	configs := make([]model.BuildContextFile, len(raw))
	for i, c := range raw {
		mode, err := parseMode(c.Mode)
		if err != nil {
			return nil, fmt.Errorf("config[%d].mode: %w", i, err)
		}
		configs[i] = model.NewBuildContextFile(c.Filename, c.Content, perm(mode))
	}
	return configs, nil
}

func convertConfigDirs(raw []api.RawConfigDirEntry) ([]model.ContainerDirMount, error) {
	dirs := make([]model.ContainerDirMount, len(raw))
	for i, cd := range raw {
		dir := model.NewContainerDirMount(cd.MountPath)
		for j, f := range cd.Files {
			mode, err := parseMode(f.Mode)
			if err != nil {
				return nil, fmt.Errorf("configDir[%d].files[%d].mode: %w", i, j, err)
			}
			dir = dir.AddFile(f.Name, f.Content, perm(mode))
		}
		dirs[i] = dir
	}
	return dirs, nil
}

func parseDesiredState(s string) (model.DesiredState, error) {
	switch strings.ToLower(s) {
	case "stopped", "":
		return model.DesiredStateStopped, nil
	case "running":
		return model.DesiredStateRunning, nil
	case "oneshot":
		return model.DesiredStateOneshot, nil
	default:
		return "", fmt.Errorf("invalid desiredState %q", s)
	}
}

// parseReclaimPolicy converts a raw string to model.ReclaimPolicy, defaulting to Retain.
func parseReclaimPolicy(s string) (model.ReclaimPolicy, error) {
	switch s {
	case "Delete":
		return model.ReclaimPolicyDelete, nil
	case "Retain", "":
		return model.ReclaimPolicyRetain, nil
	default:
		return "", fmt.Errorf("invalid reclaimPolicy %q (must be \"Delete\" or \"Retain\")", s)
	}
}
