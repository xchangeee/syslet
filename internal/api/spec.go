// Package api loads syslet spec files (a directory of JSON files or a JSON
// stream of spec objects) and deserializes them into domain-level LoadResult
// values consumed by the loader package. The JSON structs themselves live in
// one sub-package per apiVersion (v1, ...); this package only reads the input
// and routes each spec object to the package matching its apiVersion.
package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/spf13/afero"

	v1 "github.com/xchangeee/syslet/internal/api/v1"
)

// LoadResult groups the decoded specs by apiVersion. Each supported version
// has its own field holding that version package's structs; the loader converts
// every field into domain objects. Adding a version means adding a field here,
// a case in unmarshalInto, and a converter in the loader.
type LoadResult struct {
	V1 v1.Specs
}

// LoadSpecsFS reads specs from a path, dispatching on whether it is a directory
// (each .json file is one spec) or a regular file containing a JSON stream of
// specs (an array, NDJSON, or concatenated objects — see LoadSpecsReader).
func LoadSpecsFS(fs afero.Fs, path string) (LoadResult, error) {
	info, err := fs.Stat(path)
	if err != nil {
		return LoadResult{}, fmt.Errorf("reading %s: %w", path, err)
	}
	if info.IsDir() {
		return LoadSpecsDir(fs, path)
	}
	f, err := fs.Open(path)
	if err != nil {
		return LoadResult{}, fmt.Errorf("opening %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	result, err := LoadSpecsReader(f)
	if err != nil {
		return LoadResult{}, fmt.Errorf("loading specs from %s: %w", path, err)
	}
	return result, nil
}

// LoadSpecsStream reads a JSON spec stream from r and decodes it via
// LoadSpecsReader. When persistPath is non-empty the raw stream is first written
// there (creating parent directories) so callers such as the daemon can keep a
// re-applyable on-disk record of what was applied; pass "" to decode without
// persisting (e.g. a dry-run). The persist path layout is the caller's policy —
// this function only honors the path it is given.
func LoadSpecsStream(fs afero.Fs, r io.Reader, persistPath string) (LoadResult, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return LoadResult{}, fmt.Errorf("reading spec stream: %w", err)
	}
	if persistPath != "" {
		dir := filepath.Dir(persistPath)
		if err := fs.MkdirAll(dir, 0755); err != nil {
			return LoadResult{}, fmt.Errorf("creating %s: %w", dir, err)
		}
		if err := afero.WriteFile(fs, persistPath, data, 0600); err != nil {
			return LoadResult{}, fmt.Errorf("persisting %s: %w", persistPath, err)
		}
	}
	return LoadSpecsReader(bytes.NewReader(data))
}

// LoadSpecsDir reads all .json files from a directory.
func LoadSpecsDir(fs afero.Fs, dirPath string) (LoadResult, error) {
	entries, err := afero.ReadDir(fs, dirPath)
	if err != nil {
		return LoadResult{}, fmt.Errorf("reading directory %s: %w", dirPath, err)
	}

	var result LoadResult
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dirPath, entry.Name())
		data, err := afero.ReadFile(fs, path)
		if err != nil {
			return LoadResult{}, fmt.Errorf("reading %s: %w", path, err)
		}
		if err := unmarshalInto(data, entry.Name(), &result); err != nil {
			return LoadResult{}, fmt.Errorf("parsing %s: %w", entry.Name(), err)
		}
	}

	if result.empty() {
		return LoadResult{}, fmt.Errorf("no .json spec files found in directory %s", dirPath)
	}
	return result, nil
}

// LoadSpecsReader reads a JSON stream of specs from r. It accepts the shapes a
// deploy can produce over stdin or from a persisted config.json: a top-level
// JSON array of spec objects, newline-delimited JSON, or whitespace/`cat`-
// concatenated objects (e.g. `cat dir/*.json`). Each object is dispatched
// through unmarshalInto by its "type" field.
//
// An empty array is a valid desired state with no specs, which lets the planner
// prune every unit whose removal is allowed; locking a unit is the explicit
// opt-out. A stream without any JSON value is rejected instead, since that is
// what a broken pipe or a failed export produces rather than a declared state.
func LoadSpecsReader(r io.Reader) (LoadResult, error) {
	var result LoadResult
	dec := json.NewDecoder(r)
	i := 0
	for ; ; i++ {
		var msg json.RawMessage
		if err := dec.Decode(&msg); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return LoadResult{}, fmt.Errorf("decoding spec stream: %w", err)
		}
		if err := addStreamValue(msg, i, &result); err != nil {
			return LoadResult{}, err
		}
	}

	if i == 0 {
		return LoadResult{}, fmt.Errorf("no specs found in input")
	}
	return result, nil
}

// addStreamValue handles a single decoded JSON value from a spec stream. A value
// may itself be an array (the top-level-array form) whose elements are specs, or
// a single spec object (the NDJSON / concatenated-object form).
func addStreamValue(msg json.RawMessage, index int, result *LoadResult) error {
	trimmed := bytesTrimLeadingSpace(msg)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var arr []json.RawMessage
		if err := json.Unmarshal(msg, &arr); err != nil {
			return fmt.Errorf("parsing spec array: %w", err)
		}
		for j, elem := range arr {
			if err := unmarshalInto(elem, fmt.Sprintf("spec[%d]", j), result); err != nil {
				return fmt.Errorf("parsing spec[%d]: %w", j, err)
			}
		}
		return nil
	}
	if err := unmarshalInto(msg, fmt.Sprintf("spec %d", index), result); err != nil {
		return fmt.Errorf("parsing spec %d: %w", index, err)
	}
	return nil
}

// bytesTrimLeadingSpace returns b without leading JSON whitespace, used to peek
// at the first significant byte of a decoded value.
func bytesTrimLeadingSpace(b []byte) []byte {
	for len(b) > 0 {
		switch b[0] {
		case ' ', '\t', '\r', '\n':
			b = b[1:]
		default:
			return b
		}
	}
	return b
}

func (r LoadResult) empty() bool {
	return r.V1.Empty()
}

// unmarshalInto peeks at the apiVersion and type fields and hands the spec to
// the matching version package. This switch is the only place that knows which
// apiVersions exist. apiVersion is required, so the version a spec was written
// for is always explicit and never guessed. Unknown versions are rejected rather
// than decoded on a best-effort basis, so a spec written for a newer syslet
// fails loudly.
func unmarshalInto(data []byte, filename string, result *LoadResult) error {
	var peek struct {
		APIVersion string `json:"apiVersion"`
		Type       string `json:"type"`
	}
	if err := json.Unmarshal(data, &peek); err != nil {
		return err
	}

	switch peek.APIVersion {
	case "":
		return fmt.Errorf("apiVersion is required (supported: %s)", v1.Version)
	case v1.Version:
		return v1.Decode(peek.Type, data, filename, &result.V1)
	default:
		return fmt.Errorf("apiVersion %q is not supported (supported: %s); upgrade syslet", peek.APIVersion, v1.Version)
	}
}
