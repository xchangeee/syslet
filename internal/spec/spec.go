// Package spec defines the JSON container spec format and converts it to
// internal representations used by the reconciler.
package spec

// ContainerSpec is the JSON document users author to declare a managed unit.
type ContainerSpec struct {
	Name         string                       `json:"name"`
	Type         string                       `json:"type"`         // "container", "volume", "network"
	DesiredState string                       `json:"desiredState"` // "running", "stopped"
	Unit         map[string]map[string]any    `json:"unit"`
	Configs      []ConfigEntry                `json:"configs,omitempty"`
}

// ConfigEntry describes a config file to mount into a container.
type ConfigEntry struct {
	Content          string `json:"content"`
	TargetVolumePath string `json:"targetVolumePath"` // path inside the container
}
