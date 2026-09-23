// Package loader converts the specs decoded by the api package into domain
// objects. It bridges the api (raw JSON, one sub-package per apiVersion) and
// model (domain) packages: each apiVersion has its own converter (v1.go, ...)
// that maps that version's structs straight into model, and Parse/ParseSecrets
// merge the results of all versions. Helpers in this file are version-agnostic
// and may be shared by the converters.
package loader

import (
	"fmt"
	"os"
	"strconv"

	"github.com/xchangeee/syslet/internal/api"
	"github.com/xchangeee/syslet/internal/model"
)

// Parse converts all non-secret specs of every apiVersion into domain units.
func Parse(raw api.LoadResult) ([]model.Unit, error) {
	return parseV1(raw.V1)
}

// ParseSecrets converts the secret specs of every apiVersion into domain
// PodmanSecret values. It reads key names from the SOPS YAML metadata without
// decrypting, and computes the content hash used for change detection during
// the plan phase.
func ParseSecrets(raw api.LoadResult) ([]model.PodmanSecret, error) {
	return parseSecretsV1(raw.V1.Secrets)
}

func convertUnitOptions(raw map[string]map[string]any) (model.UnitOptions, error) {
	if raw == nil {
		return nil, nil
	}
	opts := make(model.UnitOptions, len(raw))
	for section, keys := range raw {
		opts[model.SectionName(section)] = make(map[model.SectionKey]model.UnitValue, len(keys))
		for key, val := range keys {
			uv, err := model.UnitValueFromRaw(model.SectionName(section), model.SectionKey(key), val)
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
