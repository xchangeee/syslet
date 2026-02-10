package api

import (
	"fmt"
	"path/filepath"
)

// ContainerSpec describes a container and maps to a .container quadlet file.
// It includes container-specific configuration like desired state and config files.
type ContainerSpec struct {
	Name         string                          `json:"name"`
	Unit         map[string]map[string]UnitValue `json:"unit"`
	DesiredState string                          `json:"desiredState,omitempty"`
	Configs      []ConfigEntry                   `json:"configs,omitempty"`
}

// ConfigEntry defines a config file to mount into a container.
type ConfigEntry struct {
	Content          string `json:"content"`
	TargetVolumePath string `json:"targetVolumePath"`
}

// Interface implementations
func (s *ContainerSpec) GetName() string                          { return s.Name }
func (s *ContainerSpec) GetType() SpecType                        { return SpecTypeContainer }
func (s *ContainerSpec) GetUnit() map[string]map[string]UnitValue { return s.Unit }
func (s *ContainerSpec) FullUnitName() string                     { return s.Name + ".container" }

// renderContainer converts a ContainerSpec into a RenderedUnit with flattened UnitOptions.
// It processes the JSON "unit" map, adds config bind-mounts, and conditionally adds the
// [Install] section if desiredState is "running".
// Sections and keys are sorted alphabetically for deterministic output.
func (s *ContainerSpec) Render(containerConfigDir string) (RenderedUnit, error) {
	opts, err := flattenUnitMap(s.GetUnit())
	if err != nil {
		return RenderedUnit{}, err
	}

	// Add ContainerName if not already specified in the spec.
	// This ensures the container name matches the spec name for consistency.
	ensureUnitOption(&opts, "Container", "ContainerName", s.Name)

	// Add config bind-mount volumes.
	for _, cfg := range s.Configs {
		basename := filepath.Base(cfg.TargetVolumePath)
		hostPath := filepath.Join(containerConfigDir, s.Name, basename)
		opts = append(opts, UnitOption{
			Section: "Container",
			Name:    "Volume",
			Value:   fmt.Sprintf("%s:%s:ro", hostPath, cfg.TargetVolumePath),
		})
	}

	// Add [Install] section only when desiredState is "running".
	// This ensures containers with desiredState "stopped" won't auto-start on boot.
	if s.DesiredState == "running" {
		opts = append(opts, UnitOption{
			Section: "Install",
			Name:    "WantedBy",
			Value:   "multi-user.target default.target",
		})
	}

	return RenderedUnit{Spec: s, UnitOptions: opts}, nil
}
