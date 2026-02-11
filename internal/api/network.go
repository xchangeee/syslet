package api

import (
	"bytes"
	"strings"

	"codeberg.org/xchangeee/syslet/internal/systemd"
	gounit "github.com/coreos/go-systemd/v22/unit"
)

// NetworkSpec describes a network and maps to a .network quadlet file.
type NetworkSpec struct {
	Name           string                          `json:"name"`
	Unit           map[string]map[string]UnitValue `json:"unit"`
	ReclaimPolicy  string                          `json:"reclaimPolicy,omitempty"`
	RemovalAllowed bool                            `json:"removalAllowed,omitempty"`
}

func (s *NetworkSpec) GetName() string                          { return s.Name }
func (s *NetworkSpec) GetType() SpecType                        { return SpecTypeNetwork }
func (s *NetworkSpec) GetUnit() map[string]map[string]UnitValue { return s.Unit }
func (s *NetworkSpec) FullUnitName() string                     { return s.Name + ".network" }

// ShouldDeleteOnRemoval checks if the network resource should be deleted when the unit is removed.
// It reads the installed unit file and checks if ReclaimPolicy is set to "Delete" in the X-Syslet section.
// This is the counterpart to Render(), which writes the ReclaimPolicy to the unit file.
func (s *NetworkSpec) ShouldDeleteOnRemoval(sd *systemd.Client) bool {
	content, err := sd.ReadUnitFile(s.FullUnitName())
	if err != nil {
		return false
	}

	// Parse unit file using systemd library.
	opts, err := gounit.Deserialize(bytes.NewReader(content))
	if err != nil {
		return false
	}

	// Look for ReclaimPolicy=Delete in X-Syslet section.
	for _, opt := range opts {
		if opt.Section == "X-Syslet" && opt.Name == "ReclaimPolicy" {
			return strings.EqualFold(opt.Value, "Delete")
		}
	}
	return false
}

// ShouldRemoveOnPrune checks if the installed unit file has RemovalAllowed=true.
// Returns true only if the marker is present, false otherwise (safe default).
// This is the counterpart to Render(), which writes the RemovalAllowed marker.
func (s *NetworkSpec) ShouldRemoveOnPrune(sd *systemd.Client) bool {
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

// renderNetwork converts a NetworkSpec into a RenderedUnit with flattened UnitOptions.
// Sections and keys are sorted alphabetically for deterministic output.
// If ReclaimPolicy is set, adds it to the X-Syslet section for cleanup on removal.
func (s *NetworkSpec) Render() (RenderedUnit, error) {
	opts, err := flattenUnitMap(s.GetUnit())
	if err != nil {
		return RenderedUnit{}, err
	}

	// Add NetworkName if not already specified in the spec.
	// This ensures the network name matches the spec name for consistency.
	ensureUnitOption(&opts, "Network", "NetworkName", s.Name)

	// Prepend reclaim policy if set.
	if s.ReclaimPolicy != "" {
		opts = append([]UnitOption{{Section: "X-Syslet", Name: "ReclaimPolicy", Value: s.ReclaimPolicy}}, opts...)
	}

	// Add RemovalAllowed marker if set.
	// This allows the unit to be removed when its spec is deleted from the input.
	if s.RemovalAllowed {
		opts = append([]UnitOption{{Section: "X-Syslet", Name: "RemovalAllowed", Value: "true"}}, opts...)
	}

	return RenderedUnit{Spec: s, UnitOptions: opts}, nil
}
