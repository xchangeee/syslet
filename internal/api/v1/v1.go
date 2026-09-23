// Package v1 holds the JSON structs of spec apiVersion "v1", the format syslet
// has accepted since before versioning existed (a spec without apiVersion is v1).
//
// The package only describes the wire format: the api package dispatches a spec
// object here by its apiVersion, and the loader package converts the resulting
// Specs into domain objects. It must not import model or any other internal
// package, and once a newer version exists it is frozen — breaking changes go
// into a new version package, never here.
package v1

import (
	"encoding/json"
	"fmt"
)

// Version is the apiVersion value selecting this package.
const Version = "v1"

// Container is the JSON deserialization target for a container spec.
type Container struct {
	APIVersion     string                    `json:"apiVersion,omitempty"`
	Type           string                    `json:"type"`
	Name           string                    `json:"name"`
	Unit           map[string]map[string]any `json:"unit"`
	DesiredState   string                    `json:"desiredState,omitempty"`
	RemovalAllowed *bool                     `json:"removalAllowed,omitempty"`
	ConfigFiles    []ConfigFileEntry         `json:"configFiles,omitempty"`
	ConfigDirs     []ConfigDirEntry          `json:"configDirs,omitempty"`
}

// Volume is the JSON deserialization target for a volume spec.
type Volume struct {
	APIVersion     string                    `json:"apiVersion,omitempty"`
	Type           string                    `json:"type"`
	Name           string                    `json:"name"`
	Unit           map[string]map[string]any `json:"unit"`
	RemovalAllowed *bool                     `json:"removalAllowed,omitempty"`
	ReclaimPolicy  string                    `json:"reclaimPolicy,omitempty"`
}

// Network is the JSON deserialization target for a network spec.
type Network struct {
	APIVersion     string                    `json:"apiVersion,omitempty"`
	Type           string                    `json:"type"`
	Name           string                    `json:"name"`
	Unit           map[string]map[string]any `json:"unit"`
	RemovalAllowed *bool                     `json:"removalAllowed,omitempty"`
	ReclaimPolicy  string                    `json:"reclaimPolicy,omitempty"`
}

// Build is the JSON deserialization target for a build spec.
type Build struct {
	APIVersion    string                    `json:"apiVersion,omitempty"`
	Type          string                    `json:"type"`
	Name          string                    `json:"name"`
	Unit          map[string]map[string]any `json:"unit"`
	ReclaimPolicy string                    `json:"reclaimPolicy,omitempty"`
	Containerfile string                    `json:"containerfile"`
	ContextFiles  []BuildFileEntry          `json:"contextFiles,omitempty"`
}

// ConfigFileEntry is the JSON deserialization target for a container config entry.
type ConfigFileEntry struct {
	MountPath string `json:"mountPath"`
	Mode      string `json:"mode,omitempty"`
	Content   string `json:"content"`
}

// ConfigDirEntry is the JSON deserialization target for a container configDir entry.
// A configDir is a directory bind-mounted into the container; its files are updated
// atomically via versioned symlinks, enabling in-place reload without restarting.
type ConfigDirEntry struct {
	MountPath string          `json:"mountPath"`
	Files     []ConfigDirFile `json:"files,omitempty"`
}

// ConfigDirFile is a single file within a ConfigDirEntry.
type ConfigDirFile struct {
	Name    string `json:"name"`
	Mode    string `json:"mode,omitempty"`
	Content string `json:"content"`
}

// BuildFileEntry is the JSON deserialization target for a build context file entry.
type BuildFileEntry struct {
	Filename string `json:"filename"`
	Mode     string `json:"mode,omitempty"`
	Content  string `json:"content"`
}

// Secret is the JSON deserialization target for a secret spec.
// Ciphertext holds the raw SOPS-encrypted YAML file content as a plain string
// (not base64) so spec authors can embed the SOPS YAML text directly. Conversion
// to model.Ciphertext ([]byte) happens in the loader.
type Secret struct {
	APIVersion string `json:"apiVersion,omitempty"`
	Type       string `json:"type"`
	Name       string `json:"name"`
	Ciphertext string `json:"ciphertext"`
}

// Specs groups the decoded v1 specs by type.
type Specs struct {
	Containers []Container
	Volumes    []Volume
	Networks   []Network
	Builds     []Build
	Secrets    []Secret
}

// Empty reports whether no spec has been decoded.
func (s Specs) Empty() bool {
	return len(s.Containers) == 0 && len(s.Volumes) == 0 && len(s.Networks) == 0 && len(s.Builds) == 0 && len(s.Secrets) == 0
}

// Decode unmarshals one spec object of the given type and appends it to out.
// The caller (api) has already peeked at apiVersion and type; filename is only
// used in error messages.
func Decode(typ string, data []byte, filename string, out *Specs) error {
	switch typ {
	case "container", "Container":
		return decodeAppend(data, &out.Containers)
	case "volume", "Volume":
		return decodeAppend(data, &out.Volumes)
	case "network", "Network":
		return decodeAppend(data, &out.Networks)
	case "build", "Build":
		return decodeAppend(data, &out.Builds)
	case "secret", "Secret":
		return decodeAppend(data, &out.Secrets)
	default:
		return fmt.Errorf("unknown spec type %q in %s", typ, filename)
	}
}

func decodeAppend[T any](data []byte, dst *[]T) error {
	var s T
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	*dst = append(*dst, s)
	return nil
}
