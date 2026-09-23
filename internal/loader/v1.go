package loader

import (
	"fmt"
	"strings"

	v1 "github.com/xchangeee/syslet/internal/api/v1"
	"github.com/xchangeee/syslet/internal/model"
	"github.com/xchangeee/syslet/internal/sops"
	"github.com/xchangeee/syslet/internal/util"
)

// This file converts apiVersion v1 specs into domain objects. Like the v1
// package itself, it is frozen once a newer version exists: format changes get
// their own converter file instead of branching here.

// parseSecretsV1 converts v1 secret specs into domain PodmanSecret values.
func parseSecretsV1(raw []v1.Secret) ([]model.PodmanSecret, error) {
	secrets := make([]model.PodmanSecret, 0, len(raw))
	for _, r := range raw {
		ct := model.Ciphertext(r.Ciphertext)
		keys, err := sops.ExtractKeys(ct)
		if err != nil {
			return nil, fmt.Errorf("secret %q: extracting keys: %w", r.Name, err)
		}
		secrets = append(secrets, model.PodmanSecret{
			Name:        r.Name,
			Ciphertext:  ct,
			Keys:        keys,
			ContentHash: util.SHA256Hex([]byte(r.Ciphertext)),
		})
	}
	return secrets, nil
}

// parseV1 converts v1 specs into a flat slice of domain units.
func parseV1(raw v1.Specs) ([]model.Unit, error) {
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

func convertContainer(r v1.Container) (*model.ContainerUnit, error) {
	opts, err := convertUnitOptions(r.Unit)
	if err != nil {
		return nil, err
	}
	desiredState, err := parseDesiredState(r.DesiredState)
	if err != nil {
		return nil, err
	}
	configFiles, err := convertConfigFiles(r.ConfigFiles)
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

func convertVolume(r v1.Volume) (*model.VolumeUnit, error) {
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

func convertNetwork(r v1.Network) (*model.NetworkUnit, error) {
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

func convertBuild(r v1.Build) (*model.BuildUnit, error) {
	opts, err := convertUnitOptions(r.Unit)
	if err != nil {
		return nil, err
	}
	reclaimPolicy, err := parseReclaimPolicy(r.ReclaimPolicy)
	if err != nil {
		return nil, err
	}
	contextFiles, err := convertBuildFiles(r.ContextFiles)
	if err != nil {
		return nil, err
	}
	return model.NewBuildUnit(
		model.BuildUnitRef(r.Name),
		opts,
		r.Containerfile,
		contextFiles,
		reclaimPolicy,
	), nil
}

func convertConfigFiles(raw []v1.ConfigFileEntry) ([]model.ContainerFileMount, error) {
	mounts := make([]model.ContainerFileMount, len(raw))
	for i, c := range raw {
		mode, err := parseMode(c.Mode)
		if err != nil {
			return nil, fmt.Errorf("config[%d].mode: %w", i, err)
		}
		mounts[i] = model.NewContainerFileMount(c.MountPath, c.Content, perm(mode))
	}
	return mounts, nil
}

func convertBuildFiles(raw []v1.BuildFileEntry) ([]model.BuildContextFile, error) {
	contextFiles := make([]model.BuildContextFile, len(raw))
	for i, c := range raw {
		mode, err := parseMode(c.Mode)
		if err != nil {
			return nil, fmt.Errorf("config[%d].mode: %w", i, err)
		}
		contextFiles[i] = model.NewBuildContextFile(c.Filename, c.Content, perm(mode))
	}
	return contextFiles, nil
}

func convertConfigDirs(raw []v1.ConfigDirEntry) ([]model.ContainerDirMount, error) {
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
