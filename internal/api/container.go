package api

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"

	"codeberg.org/xchangeee/syslet/internal/systemd"
	gounit "github.com/coreos/go-systemd/v22/unit"
)

// ContainerSpec describes a container and maps to a .container quadlet file.
// It includes container-specific configuration like desired state and config files.
type ContainerSpec struct {
	Name           string                          `json:"name"`
	Unit           map[string]map[string]UnitValue `json:"unit"`
	DesiredState   string                          `json:"desiredState,omitempty"`
	Configs        []ConfigEntry                   `json:"configs,omitempty"`
	RemovalAllowed bool                            `json:"removalAllowed,omitempty"`
}

// ConfigEntry defines a config file to mount into a container.
type ConfigEntry struct {
	Content   string `json:"content"`
	MountPath string `json:"mountPath"`
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

	// Add auto-generated description if not explicitly set by user.
	// This provides a human-readable description in systemd.
	ensureUnitOption(&opts, "Unit", "Description", s.Name+" container")

	// Add ContainerName if not already specified in the spec.
	// This ensures the container name matches the spec name for consistency.
	ensureUnitOption(&opts, "Container", "ContainerName", s.Name)

	// Add config bind-mount volumes.
	for _, cfg := range s.Configs {
		basename := filepath.Base(cfg.MountPath)
		hostPath := filepath.Join(containerConfigDir, s.Name, basename)
		opts = append(opts, UnitOption{
			Section: "Container",
			Name:    "Volume",
			Value:   fmt.Sprintf("%s:%s:ro", hostPath, cfg.MountPath),
		})
	}

	// Add RemovalAllowed marker if set.
	// This allows the unit to be removed when its spec is deleted from the input.
	if s.RemovalAllowed {
		opts = append([]UnitOption{{Section: "X-Syslet", Name: "RemovalAllowed", Value: "true"}}, opts...)
	}

	// Add [Install] section and Restart policy when desiredState is "running".
	// This ensures containers with desiredState "stopped" won't auto-start on boot
	// and running containers will automatically restart if they exit.
	// Use ensureUnitOption to avoid overriding user-specified values.
	if s.DesiredState == "running" {
		ensureUnitOption(&opts, "Service", "Restart", "always")
		ensureUnitOption(&opts, "Install", "WantedBy", "multi-user.target default.target")
	}

	return RenderedUnit{Spec: s, UnitOptions: opts}, nil
}

// ShouldRemoveOnPrune checks if the installed unit file has RemovalAllowed=true.
// Returns true only if the marker is present, false otherwise (safe default).
// This is the counterpart to Render(), which writes the RemovalAllowed marker.
func (s *ContainerSpec) ShouldRemoveOnPrune(sd *systemd.Client) bool {
	content, err := sd.ReadUnitFile(s.FullUnitName())
	if err != nil {
		return false
	}

	// Parse unit file using systemd library.
	opts, err := gounit.Deserialize(bytes.NewReader(content))
	if err != nil {
		return false
	}

	// Look for RemovalAllowed=true in X-Syslet section.
	for _, opt := range opts {
		if opt.Section == "X-Syslet" && opt.Name == "RemovalAllowed" {
			return strings.EqualFold(opt.Value, "true")
		}
	}
	return false
}
