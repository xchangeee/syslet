package validate

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/afero"

	"github.com/xchangeee/syslet/internal/render"
	"github.com/xchangeee/syslet/internal/systemd"
)

// Staging manages a persistent temporary directory for pre-flight validation
// of quadlet unit files. Files are accumulated via AddFile; Validate runs the
// generator and verifier against them and returns any error messages. The
// staging directory is always preserved so the operator can inspect what was
// staged, regardless of whether validation succeeded or failed.
type Staging struct {
	fs               afero.Fs
	dir              string // base path of the staging tree
	files            map[string][]byte
	quadletGenerator systemd.QuadletGeneratorRunner
	systemdAnalyze   systemd.AnalyzeRunner
}

// NewStaging creates a Staging instance rooted in a new temporary directory
// within the provided filesystem.
func NewStaging(fs afero.Fs, generator systemd.QuadletGeneratorRunner, analyzer systemd.AnalyzeRunner) (*Staging, error) {
	dir, err := afero.TempDir(fs, "", "syslet-stage-")
	if err != nil {
		return nil, fmt.Errorf("creating staging dir: %w", err)
	}
	return &Staging{
		fs:               fs,
		dir:              dir,
		files:            make(map[string][]byte),
		quadletGenerator: generator,
		systemdAnalyze:   analyzer,
	}, nil
}

// AddRenderedUnit registers a rendered quadlet unit to be validated.
func (s *Staging) AddRenderedUnit(ru render.RenderedUnit) {
	s.files[string(ru.Unit.Ref().FullName())] = []byte(ru.Content)
}

// Validate orchestrates pre-flight validation of staged unit files:
//  1. Write files into staging/units/
//  2. Run podman-system-generator against that directory, writing output into
//     staging/out/{early,normal,late}/
//  3. On generator failure, read journal logs and return error messages
//  4. Run systemd-analyze verify on every unit file in all output dirs —
//     since we own the staging tree, all files there are generator output
//  5. Return any error messages; the staging directory is always kept
func (s *Staging) Validate(ctx context.Context, logger *slog.Logger, jr systemd.JournalReader) []string {
	if len(s.files) == 0 {
		return nil
	}

	logger.Info("staging unit files for validation", "dir", s.dir)

	unitsDir := filepath.Join(s.dir, "units")
	earlyDir := filepath.Join(s.dir, "out", "early")
	normalDir := filepath.Join(s.dir, "out", "normal")
	lateDir := filepath.Join(s.dir, "out", "late")

	for _, d := range []string{unitsDir, earlyDir, normalDir, lateDir} {
		if err := s.fs.MkdirAll(d, 0755); err != nil {
			return []string{fmt.Sprintf("creating staging dir %s: %v", d, err)}
		}
	}

	for name, content := range s.files {
		if err := afero.WriteFile(s.fs, filepath.Join(unitsDir, name), content, 0644); err != nil {
			return []string{fmt.Sprintf("staging %s: %v", name, err)}
		}
	}

	genTime := time.Now()
	if _, err := s.quadletGenerator.Run(ctx, unitsDir, earlyDir, normalDir, lateDir); err != nil {
		msgs := []string{fmt.Sprintf("quadlet generator failed: %v", err)}
		messages, jErr := jr.QuadletErrorsSince(ctx, genTime)
		if jErr != nil {
			logger.Warn("could not read journal errors from failed generator run", "error", jErr)
		}
		for _, jmsg := range messages {
			msgs = append(msgs, fmt.Sprintf("quadlet generator error: %s", jmsg))
		}
		return msgs
	}

	// Collect all generated unit paths across output dirs. systemd-analyze loads
	// the full dependency graph per invocation, so calling it once per file
	// produces the same combined output for every file. A single call with all
	// paths avoids that duplication.
	var allPaths []string
	for _, dir := range []string{earlyDir, normalDir, lateDir} {
		entries, err := afero.ReadDir(s.fs, dir)
		if err != nil {
			logger.Warn("could not read output dir", "dir", dir, "error", err)
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				allPaths = append(allPaths, filepath.Join(dir, e.Name()))
			}
		}
	}

	var msgs []string
	if len(allPaths) > 0 {
		result, err := s.systemdAnalyze.Verify(ctx, allPaths)
		if err != nil {
			logger.Warn("could not verify units", "error", err)
		} else {
			output := strings.TrimSpace(result.Stdout + "\n" + result.Stderr)
			if result.ExitCode != 0 {
				msgs = append(msgs, fmt.Sprintf("unit verification failed: %s", output))
			} else if output != "" {
				// systemd-analyze exits 0 for non-fatal warnings (e.g. "ignoring: Invalid argument").
				// They are errors like any other message here and refuse the apply: an ignored
				// setting means the unit wouldn't run as specified, and the operator fixes the spec.
				msgs = append(msgs, fmt.Sprintf("unit verification warnings: %s", output))
			}
		}
	}
	return msgs
}
