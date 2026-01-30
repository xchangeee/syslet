package daemon

import (
	"fmt"
	"io"
	"path/filepath"

	pb "codeberg.org/xchangeee/syslet/proto"

	"github.com/coreos/go-systemd/v22/unit"
)


// resolveSpec converts a pb.UnitSpec into a ResolvedUnit.
// configBase is the host directory where config files are stored (e.g. /etc/containers/config).
func resolveSpec(spec *pb.UnitSpec, configBase string) (*ResolvedUnit, error) {
	if spec.Type == pb.UnitType_UNIT_TYPE_UNSPECIFIED {
		return nil, fmt.Errorf("unspecified unit type for %q", spec.Name)
	}

	// Map pb options to go-systemd UnitOptions.
	var opts []*unit.UnitOption
	for _, o := range spec.Options {
		opts = append(opts, &unit.UnitOption{
			Section: o.Section,
			Name:    o.Name,
			Value:   o.Value,
		})
	}

	// Process configs: generate Volume= entries and ConfigFile list.
	var cfgFiles []ConfigFile
	seen := make(map[string]bool)

	for _, ce := range spec.Configs {
		basename := filepath.Base(ce.TargetVolumePath)
		if seen[basename] {
			return nil, fmt.Errorf("duplicate config basename %q (from targetVolumePath %q)", basename, ce.TargetVolumePath)
		}
		seen[basename] = true

		hostPath := filepath.Join(configBase, spec.Name, basename)
		volumeEntry := fmt.Sprintf("%s:%s:ro", hostPath, ce.TargetVolumePath)

		opts = append(opts, &unit.UnitOption{
			Section: "Container",
			Name:    "Volume",
			Value:   volumeEntry,
		})

		cfgFiles = append(cfgFiles, ConfigFile{
			UnitName: spec.Name,
			Filename: basename,
			Content:  ce.Content,
		})
	}

	// Auto-generate [Install] section for startable units.
	if spec.Type.IsStartable() {
		opts = append(opts, &unit.UnitOption{
			Section: "Install",
			Name:    "WantedBy",
			Value:   "multi-user.target default.target",
		})
	}

	return &ResolvedUnit{
		Spec:    spec,
		Options: opts,
		Configs: cfgFiles,
	}, nil
}

func configFilenames(configs []ConfigFile) []string {
	names := make([]string, len(configs))
	for i, c := range configs {
		names[i] = c.Filename
	}
	return names
}
