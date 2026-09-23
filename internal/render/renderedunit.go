package render

import (
	"bytes"
	"cmp"
	"fmt"
	"io"
	"slices"
	"strings"

	gounit "github.com/coreos/go-systemd/v22/unit"

	"github.com/xchangeee/syslet/internal/model"
)

// RenderedUnit holds a unit and its rendered unit options before serialization.
// File and directory data (config mounts, build context files) live on the Unit itself;
// plan builders access them via type assertions on Unit.
type RenderedUnit struct {
	Unit        model.Unit
	UnitOptions []gounit.UnitOption
	Content     string
}

// NewUnitOption constructs a gounit.UnitOption from typed section and key identifiers.
func NewUnitOption(section model.SectionName, key model.SectionKey, value string) gounit.UnitOption {
	return gounit.UnitOption{Section: string(section), Name: string(key), Value: value}
}

// MatchSection reports whether opt belongs to section.
func MatchSection(opt gounit.UnitOption, section model.SectionName) bool {
	return opt.Section == string(section)
}

// MatchSectionKey reports whether opt belongs to section and has the given key.
func MatchSectionKey(opt gounit.UnitOption, section model.SectionName, key model.SectionKey) bool {
	return opt.Section == string(section) && opt.Name == string(key)
}

// MatchSectionKeyValue reports whether opt exactly matches section, key, and value.
func MatchSectionKeyValue(opt gounit.UnitOption, section model.SectionName, key model.SectionKey, value string) bool {
	return MatchSectionKey(opt, section, key) && opt.Value == value
}

// FindOptValue returns the value of the first option matching section and key, or "".
func FindOptValue(opts []gounit.UnitOption, section model.SectionName, key model.SectionKey) string {
	for _, opt := range opts {
		if MatchSectionKey(opt, section, key) {
			return opt.Value
		}
	}
	return ""
}

// FindOptValues returns all values matching section and key, in order.
func FindOptValues(opts []gounit.UnitOption, section model.SectionName, key model.SectionKey) []string {
	var vals []string
	for _, opt := range opts {
		if MatchSectionKey(opt, section, key) {
			vals = append(vals, opt.Value)
		}
	}
	return vals
}

// UnitRenderer accumulates render-time configuration for a unit and produces a RenderedUnit.
type UnitRenderer struct {
	unit      model.Unit
	extra     []gounit.UnitOption
	defaults  []gounit.UnitOption
	overrides []gounit.UnitOption
}

// NewUnitRenderer creates a UnitRenderer for the given unit.
func NewUnitRenderer(unit model.Unit) *UnitRenderer { return &UnitRenderer{unit: unit} }

// Append adds an option that is appended after the unit's own options.
func (r *UnitRenderer) Append(section model.SectionName, key model.SectionKey, value string) *UnitRenderer {
	r.extra = append(r.extra, NewUnitOption(section, key, value))
	return r
}

// Default sets an option only when the unit does not already define it.
func (r *UnitRenderer) Default(section model.SectionName, key model.SectionKey, value string) *UnitRenderer {
	r.defaults = append(r.defaults, NewUnitOption(section, key, value))
	return r
}

// Override replaces any existing value for the option, or adds it if absent.
func (r *UnitRenderer) Override(section model.SectionName, key model.SectionKey, value string) *UnitRenderer {
	r.overrides = append(r.overrides, NewUnitOption(section, key, value))
	return r
}

// RenderedUnit finalizes the render and returns the fully-populated RenderedUnit.
func (r *UnitRenderer) RenderedUnit() (RenderedUnit, error) {
	flat := flattenUnitOptions(r.unit.Options())
	flat = append(flat, r.extra...)
	for _, d := range r.defaults {
		flat = ensureUnitOption(flat, model.SectionName(d.Section), model.SectionKey(d.Name), d.Value)
	}
	for _, o := range r.overrides {
		flat = forceUnitOption(flat, model.SectionName(o.Section), model.SectionKey(o.Name), o.Value)
	}
	// Sort to canonical order so the serialized Content is identical regardless of whether
	// options came from user-defined sections or syslet overrides. Without this,
	// flattenUnitOptions sorts user sections alphabetically while override/default options
	// are prepended when absent, producing different section orderings for logically
	// identical units and causing spurious "unit updated" events.
	slices.SortFunc(flat, compareUnitOptions)
	goOpts := make([]*gounit.UnitOption, len(flat))
	for i := range flat {
		opt := flat[i]
		if model.SectionKey(opt.Name) == KeyEnvironment {
			opt.Value = quoteUnitValue(opt.Value)
		}
		goOpts[i] = &opt
	}
	data, err := io.ReadAll(gounit.Serialize(goOpts))
	if err != nil {
		return RenderedUnit{}, fmt.Errorf("serializing unit: %w", err)
	}
	return RenderedUnit{
		Unit:        r.unit,
		UnitOptions: flat,
		Content:     string(data),
	}, nil
}

// quoteUnitValue wraps value in double quotes if it contains whitespace, so systemd
// parses it as a single token rather than splitting it on spaces into several
// assignments. This is only applied to Environment= values: systemd documents
// Environment= as accepting a space-separated list of assignments with quoting
// support (systemd.exec(5)/systemd.syntax(7)); other unit keys don't share that
// list-with-quoting syntax, so quoting them here would just embed literal quote
// characters in the value instead of being parsed away. Without this, a value like
// "extra_params=--a=1 --b=2" would be split into two separate environment variables
// instead of one.
func quoteUnitValue(value string) string {
	if !strings.ContainsAny(value, " \t") {
		return value
	}
	if isAlreadyQuoted(value) {
		return value
	}
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range value {
		if r == '"' || r == '\\' {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String()
}

// isAlreadyQuoted reports whether value is already wrapped in a matching pair of
// quotes (double or single, per systemd's own quoting rules), so quoteUnitValue
// does not wrap it a second time.
func isAlreadyQuoted(value string) bool {
	if len(value) < 2 {
		return false
	}
	first, last := value[0], value[len(value)-1]
	return (first == '"' || first == '\'') && first == last
}

func flattenUnitOptions(unitMap model.UnitOptions) []gounit.UnitOption {
	sections := make([]model.SectionName, 0, len(unitMap))
	for section := range unitMap {
		sections = append(sections, section)
	}
	slices.Sort(sections)

	var opts []gounit.UnitOption
	for _, section := range sections {
		keys := make([]model.SectionKey, 0, len(unitMap[section]))
		for k := range unitMap[section] {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, key := range keys {
			for _, v := range unitMap[section][key].Values() {
				opts = append(opts, NewUnitOption(section, key, v))
			}
		}
	}
	return opts
}

func ensureUnitOption(opts []gounit.UnitOption, section model.SectionName, key model.SectionKey, value string) []gounit.UnitOption {
	for _, opt := range opts {
		if MatchSectionKey(opt, section, key) {
			return opts
		}
	}
	return append([]gounit.UnitOption{NewUnitOption(section, key, value)}, opts...)
}

func forceUnitOption(opts []gounit.UnitOption, section model.SectionName, key model.SectionKey, value string) []gounit.UnitOption {
	for i, opt := range opts {
		if MatchSectionKey(opt, section, key) {
			opts[i].Value = value
			return opts
		}
	}
	return append([]gounit.UnitOption{NewUnitOption(section, key, value)}, opts...)
}

// compareUnitOptions defines the canonical (section, key, value) ordering used when
// serializing unit files and when stripping metadata for content comparison.
func compareUnitOptions(a, b gounit.UnitOption) int {
	if n := cmp.Compare(a.Section, b.Section); n != 0 {
		return n
	}
	if n := cmp.Compare(a.Name, b.Name); n != 0 {
		return n
	}
	return cmp.Compare(a.Value, b.Value)
}

// StripMetadataSections removes X-Syslet section and Unit.Description before comparing content.
func StripMetadataSections(content string) (string, error) {
	opts, err := gounit.DeserializeOptions(bytes.NewReader([]byte(content)))
	if err != nil {
		return "", fmt.Errorf("deserializing unit content: %w", err)
	}

	var filtered []*gounit.UnitOption
	for _, opt := range opts {
		if MatchSection(*opt, SectionXSyslet) {
			continue
		}
		if MatchSectionKey(*opt, SectionUnit, KeyUnitDescription) {
			continue
		}
		filtered = append(filtered, opt)
	}

	slices.SortFunc(filtered, func(a, b *gounit.UnitOption) int {
		return compareUnitOptions(*a, *b)
	})

	data, err := io.ReadAll(gounit.Serialize(filtered))
	if err != nil {
		return "", fmt.Errorf("serializing stripped unit: %w", err)
	}
	return string(data), nil
}
