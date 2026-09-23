//go:build integration

package systest

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/xchangeee/syslet/internal/api"
	"github.com/xchangeee/syslet/internal/model"
)

// This file turns model values back into the newline-delimited JSON the loader
// reads, so that every test's desired specs enter syslet through the real
// api.LoadSpecsReader rather than being injected past it. That keeps the loader
// itself under test on every integration run.

// Specs declares the desired specs for this host by marshaling them to the
// loader's JSON format and reading them back through api.LoadSpecsReader.
//
// Calling it replaces any specs declared earlier; to mix typed specs with raw
// JSON, use SpecsJSON.
func (e *Env) Specs(specs ...model.Unit) {
	e.t.Helper()
	lines := make([]string, len(specs))
	for i, spec := range specs {
		data, err := marshalSpec(spec)
		if err != nil {
			e.t.Fatalf("marshal spec %d (%s): %v", i, spec.Ref().FullName(), err)
		}
		lines[i] = string(data)
	}
	e.SpecsJSON(lines...)
}

// SpecsJSON declares the desired specs from raw JSON lines.
//
// It exists because there is no model.SecretUnit: a secret reaches the loader
// as JSON only. Pair it with SecretJSON to mix a secret into an otherwise typed
// spec set, e.g.
//
//	env.SpecsJSON(systest.SecretJSON("myapp", ct), env.UnitJSON(spec))
//
// Both paths go through the production loader.
func (e *Env) SpecsJSON(lines ...string) {
	e.t.Helper()
	if len(lines) == 0 {
		e.raw = api.LoadResult{}
		return
	}
	var buf bytes.Buffer
	for _, line := range lines {
		buf.WriteString(line)
		buf.WriteByte('\n')
	}
	raw, err := api.LoadSpecsReader(&buf)
	if err != nil {
		e.t.Fatalf("load specs: %v", err)
	}
	e.raw = raw
}

// SecretJSON renders a secret spec as one loader JSON line.
func SecretJSON(name string, ciphertext model.Ciphertext) string {
	data, err := json.Marshal(map[string]any{
		"apiVersion": "v1",
		"type":       "secret",
		"name":       name,
		"ciphertext": string(ciphertext),
	})
	if err != nil {
		// The map holds only strings; marshaling it cannot fail.
		panic(fmt.Sprintf("marshal secret spec %q: %v", name, err))
	}
	return string(data)
}

// UnitJSON renders a typed unit spec as one loader JSON line, for mixing with
// SecretJSON in a single SpecsJSON call.
func (e *Env) UnitJSON(spec model.Unit) string {
	e.t.Helper()
	data, err := marshalSpec(spec)
	if err != nil {
		e.t.Fatalf("marshal spec %s: %v", spec.Ref().FullName(), err)
	}
	return string(data)
}

// unitOptionsToRaw converts model.UnitOptions to the nested map the loader
// expects, collapsing single-valued keys to a bare string as real input does.
func unitOptionsToRaw(opts model.UnitOptions) map[string]map[string]any {
	raw := make(map[string]map[string]any)
	for section, keys := range opts {
		raw[string(section)] = make(map[string]any)
		for key, uv := range keys {
			values := uv.Values()
			if len(values) == 1 {
				raw[string(section)][string(key)] = values[0]
				continue
			}
			strs := make([]string, len(values))
			copy(strs, values)
			raw[string(section)][string(key)] = strs
		}
	}
	return raw
}

// marshalSpec serializes a model.Unit to the raw JSON format read by the loader.
func marshalSpec(spec model.Unit) ([]byte, error) {
	raw := map[string]any{
		"apiVersion": "v1",
		"type":       string(spec.Ref().UnitType()),
		"name":       spec.Ref().Name(),
		"unit":       unitOptionsToRaw(spec.Options()),
	}
	switch s := spec.(type) {
	case *model.ContainerUnit:
		if s.DesiredState != "" {
			raw["desiredState"] = string(s.DesiredState)
		}
		// Always explicit: an omitted removalAllowed loads as true.
		raw["removalAllowed"] = s.RemovalAllowed
		if len(s.FileMounts) > 0 {
			fileMounts := make([]map[string]any, len(s.FileMounts))
			for i, c := range s.FileMounts {
				entry := map[string]any{
					"mountPath": string(c.FullPath()),
					"content":   c.File.Content,
				}
				if c.File.Mode != 0 {
					entry["mode"] = fmt.Sprintf("%04o", c.File.Mode)
				}
				fileMounts[i] = entry
			}
			raw["configFiles"] = fileMounts
		}
		if len(s.DirMounts) > 0 {
			configDirs := make([]map[string]any, len(s.DirMounts))
			for i, cd := range s.DirMounts {
				files := make([]map[string]any, len(cd.Files))
				for j, f := range cd.Files {
					fe := map[string]any{"name": f.Name, "content": f.Content}
					if f.Mode != 0 {
						fe["mode"] = fmt.Sprintf("%04o", f.Mode)
					}
					files[j] = fe
				}
				configDirs[i] = map[string]any{
					"mountPath": string(cd.Directory),
					"files":     files,
				}
			}
			raw["configDirs"] = configDirs
		}
	case *model.VolumeUnit:
		// Always explicit: an omitted removalAllowed loads as true.
		raw["removalAllowed"] = s.RemovalAllowed
		if s.ReclaimPolicy != "" {
			raw["reclaimPolicy"] = string(s.ReclaimPolicy)
		}
	case *model.NetworkUnit:
		// Always explicit: an omitted removalAllowed loads as true.
		raw["removalAllowed"] = s.RemovalAllowed
		if s.ReclaimPolicy != "" {
			raw["reclaimPolicy"] = string(s.ReclaimPolicy)
		}
	case *model.BuildUnit:
		raw["containerfile"] = s.Containerfile
		if s.ReclaimPolicy != "" {
			raw["reclaimPolicy"] = string(s.ReclaimPolicy)
		}
		if len(s.ContextFiles) > 0 {
			contextFiles := make([]map[string]any, len(s.ContextFiles))
			for i, c := range s.ContextFiles {
				entry := map[string]any{
					"filename": string(c.Filename),
					"content":  c.Content,
				}
				if c.Mode != 0 {
					entry["mode"] = fmt.Sprintf("%04o", c.Mode)
				}
				contextFiles[i] = entry
			}
			raw["contextFiles"] = contextFiles
		}
	}
	return json.Marshal(raw)
}
