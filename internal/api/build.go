package api

import (
	"bytes"
	"path/filepath"
	"strings"

	"codeberg.org/xchangeee/syslet/internal/systemd"
	gounit "github.com/coreos/go-systemd/v22/unit"
)

// BuildFileEntry defines a file to be written to the build context directory.
// Unlike container configs which are mounted, build files are copied into the build context.
type BuildFileEntry struct {
	Content  string `json:"content"`
	Filename string `json:"filename"` // Relative path within build context (e.g., "app.py", "config/settings.json")
}

// BuildSpec describes a container image build and maps to a .build quadlet file.
// Build units represent local containerfile builds that can be referenced as Image=
// in container units. They are implicitly removable when no container references them.
type BuildSpec struct {
	Name          string                          `json:"name"`
	Unit          map[string]map[string]UnitValue `json:"unit"`
	Containerfile string                          `json:"containerfile"`           // Required: Containerfile content
	Configs       []BuildFileEntry                `json:"configs,omitempty"`       // Optional: Additional build context files
	ReclaimPolicy string                          `json:"reclaimPolicy,omitempty"` // "Delete" removes image on unit removal
}

func (s *BuildSpec) GetName() string                          { return s.Name }
func (s *BuildSpec) GetType() SpecType                        { return SpecTypeBuild }
func (s *BuildSpec) GetUnit() map[string]map[string]UnitValue { return s.Unit }
func (s *BuildSpec) FullUnitName() string                     { return s.Name + ".build" }

// ShouldDeleteOnRemoval checks if the built image should be deleted when the unit is removed.
// It reads the installed unit file and checks if ReclaimPolicy is set to "Delete" in the X-Syslet section.
// This is the counterpart to Render(), which writes the ReclaimPolicy to the unit file.
func (s *BuildSpec) ShouldDeleteOnRemoval(sd *systemd.Client) bool {
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

// Render converts a BuildSpec into a RenderedUnit with flattened UnitOptions.
// Sections and keys are sorted alphabetically for deterministic output.
// If ReclaimPolicy is set, adds it to the X-Syslet section for cleanup on removal.
// Auto-generates File= path pointing to the Containerfile in the build context directory.
func (s *BuildSpec) Render(buildContextDir string) (RenderedUnit, error) {
	opts, err := flattenUnitMap(s.GetUnit())
	if err != nil {
		return RenderedUnit{}, err
	}

	// Auto-generate File= path pointing to Containerfile in build context.
	// This is required by Quadlet and must be set based on where we write the files.
	containerfilePath := filepath.Join(buildContextDir, s.Name, "Containerfile")
	ensureUnitOption(&opts, "Build", "File", containerfilePath)

	// Prepend reclaim policy if set.
	if s.ReclaimPolicy != "" {
		opts = append([]UnitOption{{Section: "X-Syslet", Name: "ReclaimPolicy", Value: s.ReclaimPolicy}}, opts...)
	}

	return RenderedUnit{Spec: s, UnitOptions: opts}, nil
}
