package api

import (
	"encoding/json"
	"fmt"
	"io"

	gounit "github.com/coreos/go-systemd/v22/unit"
)

// UnitValue represents a value in a systemd unit specification.
// It accepts either a single string or an array of strings during JSON unmarshaling.
//
// This custom type is necessary because:
//  1. Systemd unit files only support string values (no numbers, booleans, etc.)
//  2. Some unit options can appear multiple times (e.g., Volume, PublishPort)
//  3. We want to reject non-string values at unmarshal time rather than silently
//     converting them with fmt.Sprintf, which can hide user errors
//
// Examples of valid JSON:
//
//	"Image": "nginx:latest"              → single string
//	"PublishPort": ["8080:80", "443:443"] → array of strings
//
// Invalid (will be rejected during unmarshaling):
//
//	"Port": 8080                         → number
//	"Enabled": true                      → boolean
type UnitValue struct {
	values []string
}

func UV(value string) UnitValue {
	return UnitValue{values: []string{value}}
}

// UnmarshalJSON implements json.Unmarshaler to accept either a string or []string.
// Non-string values are rejected with a descriptive error.
func (uv *UnitValue) UnmarshalJSON(data []byte) error {
	// Try unmarshaling as a string first
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		uv.values = []string{s}
		return nil
	}

	// Try unmarshaling as []string
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		uv.values = arr
		return nil
	}

	return fmt.Errorf("unit value must be a string or array of strings, got: %s", string(data))
}

// MarshalJSON implements json.Marshaler for testing and serialization.
func (uv UnitValue) MarshalJSON() ([]byte, error) {
	if len(uv.values) == 1 {
		return json.Marshal(uv.values[0])
	}
	return json.Marshal(uv.values)
}

// Values returns the underlying string values.
// For single strings, returns a slice with one element.
// For arrays, returns all elements.
func (uv UnitValue) Values() []string {
	if uv.values == nil {
		return []string{}
	}
	return uv.values
}

// RenderedUnit holds a spec and its rendered unit options before serialization.
// The Content field is populated after serialization for use during apply.
type RenderedUnit struct {
	Spec        Spec
	UnitOptions []UnitOption
	Content     string // serialized form (computed after validation)
}

// UnitOption is a single key=value in a systemd unit file section.
type UnitOption struct {
	Section string
	Name    string
	Value   string
}

// serializeUnitOptions converts unit options to systemd unit file content.
func (ru *RenderedUnit) SerializeUnitOptions() (string, error) {
	var goOpts []*gounit.UnitOption
	for _, o := range ru.UnitOptions {
		goOpts = append(goOpts, &gounit.UnitOption{
			Section: o.Section,
			Name:    o.Name,
			Value:   o.Value,
		})
	}

	data, err := io.ReadAll(gounit.Serialize(goOpts))
	if err != nil {
		return "", fmt.Errorf("serializing unit: %w", err)
	}
	return string(data), nil
}
