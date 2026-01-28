package testutil

import (
	"encoding/json"
	"fmt"

	"codeberg.org/xchangeee/syslet/internal/server/containerconfig"
	"codeberg.org/xchangeee/syslet/internal/server/parser"
	"codeberg.org/xchangeee/syslet/internal/server/spec"
)

// ParseSpec parses a JSON spec and returns the parsed unit and config files.
func ParseSpec(specJSON, configBase string) (*parser.ParsedUnit, []containerconfig.ConfigFile, error) {
	var cs spec.ContainerSpec
	if err := json.Unmarshal([]byte(specJSON), &cs); err != nil {
		return nil, nil, fmt.Errorf("parsing spec JSON: %w", err)
	}
	return spec.Convert(&cs, configBase)
}

// SpecBuilder builds JSON specs for testing.
type SpecBuilder struct {
	name         string
	unitType     string
	desiredState string
	unit         map[string]map[string]any
	configs      []configEntry
}

type configEntry struct {
	Content          string `json:"content"`
	TargetVolumePath string `json:"targetVolumePath"`
}

// ContainerSpec creates a new container spec builder.
func ContainerSpec(name string) *SpecBuilder {
	return &SpecBuilder{
		name:         name,
		unitType:     "container",
		desiredState: "running",
		unit: map[string]map[string]any{
			"Container": {},
		},
	}
}

// VolumeSpec creates a new volume spec builder.
func VolumeSpec(name string) *SpecBuilder {
	return &SpecBuilder{
		name:     name,
		unitType: "volume",
		unit: map[string]map[string]any{
			"Volume": {},
		},
	}
}

// NetworkSpec creates a new network spec builder.
func NetworkSpec(name string) *SpecBuilder {
	return &SpecBuilder{
		name:     name,
		unitType: "network",
		unit: map[string]map[string]any{
			"Network": {},
		},
	}
}

// Image sets the container image.
func (b *SpecBuilder) Image(image string) *SpecBuilder {
	b.unit["Container"]["Image"] = image
	return b
}

// PublishPort adds a port mapping.
func (b *SpecBuilder) PublishPort(mapping string) *SpecBuilder {
	ports, _ := b.unit["Container"]["PublishPort"].([]string)
	ports = append(ports, mapping)
	b.unit["Container"]["PublishPort"] = ports
	return b
}

// Volume adds a volume mount.
func (b *SpecBuilder) Volume(mount string) *SpecBuilder {
	volumes, _ := b.unit["Container"]["Volume"].([]string)
	volumes = append(volumes, mount)
	b.unit["Container"]["Volume"] = volumes
	return b
}

// Network adds a network.
func (b *SpecBuilder) Network(network string) *SpecBuilder {
	networks, _ := b.unit["Container"]["Network"].([]string)
	networks = append(networks, network)
	b.unit["Container"]["Network"] = networks
	return b
}

// Environment adds an environment variable.
func (b *SpecBuilder) Environment(env string) *SpecBuilder {
	envs, _ := b.unit["Container"]["Environment"].([]string)
	envs = append(envs, env)
	b.unit["Container"]["Environment"] = envs
	return b
}

// DesiredState sets the desired state (running or stopped).
func (b *SpecBuilder) DesiredState(state string) *SpecBuilder {
	b.desiredState = state
	return b
}

// Running is a shorthand for DesiredState("running").
func (b *SpecBuilder) Running() *SpecBuilder {
	return b.DesiredState("running")
}

// Stopped is a shorthand for DesiredState("stopped").
func (b *SpecBuilder) Stopped() *SpecBuilder {
	return b.DesiredState("stopped")
}

// WithConfig adds a config file.
func (b *SpecBuilder) WithConfig(targetPath, content string) *SpecBuilder {
	b.configs = append(b.configs, configEntry{
		Content:          content,
		TargetVolumePath: targetPath,
	})
	return b
}

// Label adds a label (for volumes).
func (b *SpecBuilder) Label(label string) *SpecBuilder {
	if _, ok := b.unit["Volume"]; ok {
		b.unit["Volume"]["Label"] = label
	}
	return b
}

// Subnet sets the network subnet.
func (b *SpecBuilder) Subnet(subnet string) *SpecBuilder {
	if _, ok := b.unit["Network"]; ok {
		b.unit["Network"]["Subnet"] = subnet
	}
	return b
}

// Gateway sets the network gateway.
func (b *SpecBuilder) Gateway(gateway string) *SpecBuilder {
	if _, ok := b.unit["Network"]; ok {
		b.unit["Network"]["Gateway"] = gateway
	}
	return b
}

// SetOption sets a custom option in the primary section.
func (b *SpecBuilder) SetOption(key string, value any) *SpecBuilder {
	// Determine primary section based on unit type
	var section string
	switch b.unitType {
	case "container":
		section = "Container"
	case "volume":
		section = "Volume"
	case "network":
		section = "Network"
	}
	if section != "" {
		b.unit[section][key] = value
	}
	return b
}

// Build returns the spec as a JSON string.
func (b *SpecBuilder) Build() string {
	s := map[string]any{
		"name": b.name,
		"type": b.unitType,
		"unit": b.unit,
	}

	if b.desiredState != "" {
		s["desiredState"] = b.desiredState
	}

	if len(b.configs) > 0 {
		s["configs"] = b.configs
	}

	data, _ := json.Marshal(s)
	return string(data)
}

// MustBuild is like Build but panics on error (for tests).
func (b *SpecBuilder) MustBuild() string {
	return b.Build()
}

// --- Convenience functions for quick specs ---

// QuickContainer creates a minimal container spec JSON.
func QuickContainer(name, image string) string {
	return ContainerSpec(name).Image(image).Build()
}

// QuickContainerStopped creates a stopped container spec JSON.
func QuickContainerStopped(name, image string) string {
	return ContainerSpec(name).Image(image).Stopped().Build()
}

// QuickVolume creates a minimal volume spec JSON.
func QuickVolume(name string) string {
	return VolumeSpec(name).Build()
}

// QuickVolumeWithLabel creates a volume spec with a label.
func QuickVolumeWithLabel(name, label string) string {
	return VolumeSpec(name).Label(label).Build()
}

// QuickNetwork creates a minimal network spec JSON.
func QuickNetwork(name string) string {
	return NetworkSpec(name).Build()
}

// QuickNetworkWithSubnet creates a network spec with subnet.
func QuickNetworkWithSubnet(name, subnet, gateway string) string {
	return NetworkSpec(name).Subnet(subnet).Gateway(gateway).Build()
}
