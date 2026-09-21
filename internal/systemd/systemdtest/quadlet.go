package systemdtest

import (
	"context"
	"path/filepath"

	"github.com/spf13/afero"
)

// The three types below all satisfy systemd.QuadletGeneratorRunner and differ
// only in what they do with the staging directories. They share a naming family
// so the choice is legible at the call site:
//
//	Noop      — generates nothing; use when the test does not exercise staging.
//	Fake      — writes canned files into the normal-unit output directory,
//	            so post-render validation has something to verify.
//	Recording — records which unit files were staged as input, so the test can
//	            assert on what syslet handed the generator.

// NoopQuadletGenerator is a generator that does nothing and reports a
// configurable exit code and error.
type NoopQuadletGenerator struct {
	GenerateCode int
	GenerateErr  error
}

// Run returns the configured exit code and error without touching either directory.
func (g *NoopQuadletGenerator) Run(_ context.Context, _, _, _, _ string) (int, error) {
	return g.GenerateCode, g.GenerateErr
}

// FakeQuadletGenerator writes a fixed set of generated files into the normal
// output directory, standing in for the unit files a real quadlet generator
// would produce from the staged input.
type FakeQuadletGenerator struct {
	Fs    afero.Fs
	Files map[string]string // filename -> content
}

// Run writes each configured file into normalDir.
func (g *FakeQuadletGenerator) Run(_ context.Context, _, _, normalDir, _ string) (int, error) {
	for name, content := range g.Files {
		path := filepath.Join(normalDir, name)
		if err := afero.WriteFile(g.Fs, path, []byte(content), 0644); err != nil {
			return 1, err
		}
	}
	return 0, nil
}

// RecordingQuadletGenerator records the filenames present in the units input
// directory each time it runs, so tests can verify which unit files syslet
// staged for validation.
type RecordingQuadletGenerator struct {
	Fs afero.Fs

	stagedFiles []string
}

// Run records the non-directory entries found in unitsDir.
func (g *RecordingQuadletGenerator) Run(_ context.Context, unitsDir, _, _, _ string) (int, error) {
	entries, _ := afero.ReadDir(g.Fs, unitsDir)
	for _, e := range entries {
		if !e.IsDir() {
			g.stagedFiles = append(g.stagedFiles, e.Name())
		}
	}
	return 0, nil
}

// StagedFiles returns the unit filenames staged across all runs, in the order
// they were observed.
func (g *RecordingQuadletGenerator) StagedFiles() []string {
	return g.stagedFiles
}
