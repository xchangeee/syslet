package api

import (
	"fmt"
	"io"

	gounit "github.com/coreos/go-systemd/v22/unit"
)

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
