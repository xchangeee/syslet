package render

import (
	"path/filepath"

	gounit "github.com/coreos/go-systemd/v22/unit"

	"github.com/xchangeee/syslet/internal/model"
	"github.com/xchangeee/syslet/internal/util"
)

// testContainerResolve returns a ContainerResolverFactory that mirrors the production
// naming strategy: basename(dest)+sha256[:8] under baseDir/containerName/.
func testContainerResolve(baseDir string) ContainerResolverFactory {
	return func(ref model.ContainerUnitRef, dest model.ContainerMountPath) string {
		return filepath.Join(baseDir, ref.Name(), util.BasenameWithHashSuffix(string(dest)))
	}
}

// testBuildResolve returns a BuildResolverFactory that joins baseDir, build name, and filename.
func testBuildResolve(baseDir string) BuildResolverFactory {
	return func(name, f string) string { return filepath.Join(baseDir, name, f) }
}

// mustRender calls t.Fatalf on error, otherwise returns the RenderedUnit.
func mustRender(t interface {
	Helper()
	Fatalf(string, ...any)
}, ru RenderedUnit, err error) RenderedUnit {
	t.Helper()
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	return ru
}

// assertHasOption fails if no option with the given (section, key, value) is found.
func assertHasOption(t interface {
	Helper()
	Errorf(string, ...any)
}, opts []gounit.UnitOption, section model.SectionName, key model.SectionKey, value string) {
	t.Helper()
	for _, o := range opts {
		if MatchSectionKeyValue(o, section, key, value) {
			return
		}
	}
	t.Errorf("expected option %s.%s=%q to be present", section, key, value)
}

// assertNoOption fails if any option with the given (section, key) is found.
func assertNoOption(t interface {
	Helper()
	Errorf(string, ...any)
}, opts []gounit.UnitOption, section model.SectionName, key model.SectionKey) {
	t.Helper()
	for _, o := range opts {
		if MatchSectionKey(o, section, key) {
			t.Errorf("expected no %s.%s option, got value=%q", section, key, o.Value)
		}
	}
}

// assertUniqueOption fails if there is not exactly one option with (section, key)
// or if its value does not equal wantValue.
func assertUniqueOption(t interface {
	Helper()
	Errorf(string, ...any)
}, opts []gounit.UnitOption, section model.SectionName, key model.SectionKey, wantValue string) {
	t.Helper()
	var count int
	var got string
	for _, o := range opts {
		if MatchSectionKey(o, section, key) {
			count++
			got = o.Value
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 %s.%s option, got %d", section, key, count)
		return
	}
	if got != wantValue {
		t.Errorf("expected %s.%s=%q, got %q", section, key, wantValue, got)
	}
}

// unitOptions calls fn, fails on error, and returns the resulting UnitOptions slice.
func unitOptions(t interface {
	Helper()
	Fatalf(string, ...any)
}, fn func() (RenderedUnit, error)) []gounit.UnitOption {
	t.Helper()
	ru, err := fn()
	return mustRender(t, ru, err).UnitOptions
}

// filterOptions returns all options matching the given section and key.
func filterOptions(opts []gounit.UnitOption, section model.SectionName, key model.SectionKey) []gounit.UnitOption {
	var result []gounit.UnitOption
	for _, o := range opts {
		if MatchSectionKey(o, section, key) {
			result = append(result, o)
		}
	}
	return result
}

// makeUnitOptions builds a UnitOptions map with a single section populated from
// alternating key/value string pairs. Panics if kvs has an odd length.
func makeUnitOptions(section model.SectionName, kvs ...string) model.UnitOptions {
	if len(kvs)%2 != 0 {
		panic("makeUnitOptions: kvs must be key/value pairs")
	}
	m := make(map[model.SectionKey]model.UnitValue, len(kvs)/2)
	for i := 0; i < len(kvs); i += 2 {
		m[model.SectionKey(kvs[i])] = model.UV(kvs[i+1])
	}
	return model.UnitOptions{section: m}
}
