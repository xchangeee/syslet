package validate

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/afero"

	"github.com/xchangeee/syslet/internal/model"
	"github.com/xchangeee/syslet/internal/render"
	"github.com/xchangeee/syslet/internal/systemd"
	"github.com/xchangeee/syslet/internal/systemd/systemdtest"
	testlog "github.com/xchangeee/syslet/test/log"
)

func newTestStaging(t *testing.T, fs afero.Fs, gen systemd.QuadletGeneratorRunner, az systemd.AnalyzeRunner) *Staging {
	t.Helper()
	s, err := NewStaging(fs, gen, az)
	if err != nil {
		t.Fatalf("newStaging: %v", err)
	}
	return s
}

func stageUnit(s *Staging, ref model.ContainerUnitRef, content string) {
	s.AddRenderedUnit(render.RenderedUnit{
		Unit:    model.NewContainerUnit(ref, nil, "", nil, false),
		Content: content,
	})
}

// TestStaging_NoFiles_ReturnsNil verifies the early-return when no files are staged.
func TestStaging_NoFiles_ReturnsNil(t *testing.T) {
	fs := afero.NewMemMapFs()
	s := newTestStaging(t, fs, &systemdtest.NoopQuadletGenerator{}, &systemdtest.MockAnalyzeRunner{})

	msgs := s.Validate(context.Background(), testlog.New(), &systemdtest.MockJournalReader{})
	if len(msgs) != 0 {
		t.Errorf("expected no messages for empty staging, got: %v", msgs)
	}
}

// TestStaging_GeneratorFails_ReturnsError verifies that a generator exit error is
// surfaced as an error message and journal messages are appended.
func TestStaging_GeneratorFails_ReturnsError(t *testing.T) {
	fs := afero.NewMemMapFs()
	gen := &systemdtest.NoopQuadletGenerator{GenerateErr: errors.New("generator crashed")}
	jr := &systemdtest.MockJournalReader{Messages: []string{"foo.container: unknown key Bar"}}

	s := newTestStaging(t, fs, gen, &systemdtest.MockAnalyzeRunner{})
	stageUnit(s, "foo", "[Container]\nImage=nginx:latest\n")

	msgs := s.Validate(context.Background(), testlog.New(), jr)
	if len(msgs) == 0 {
		t.Fatal("expected error messages, got none")
	}

	foundGenError := false
	foundJournalError := false
	for _, m := range msgs {
		if containsAll(m, "quadlet generator") {
			foundGenError = true
		}
		if containsAll(m, "foo.container", "unknown key") {
			foundJournalError = true
		}
	}
	if !foundGenError {
		t.Errorf("expected generator error message, got: %v", msgs)
	}
	if !foundJournalError {
		t.Errorf("expected journal error message to be appended, got: %v", msgs)
	}
}

// TestStaging_GeneratorFails_JournalError_NocrashverifyStaging verifies that a journal
// read failure during generator error handling doesn't crash — it should just be logged
// and the generator error still returned.
func TestStaging_GeneratorFails_JournalReadError_StillReturnsGeneratorError(t *testing.T) {
	fs := afero.NewMemMapFs()
	gen := &systemdtest.NoopQuadletGenerator{GenerateErr: errors.New("generator crashed")}
	jr := &systemdtest.MockJournalReader{Err: errors.New("journal unavailable")}

	s := newTestStaging(t, fs, gen, &systemdtest.MockAnalyzeRunner{})
	stageUnit(s, "foo", "[Container]\nImage=nginx:latest\n")

	msgs := s.Validate(context.Background(), testlog.New(), jr)
	if len(msgs) == 0 {
		t.Fatal("expected generator error message, got none")
	}
	if !containsAll(msgs[0], "quadlet generator") {
		t.Errorf("expected generator error message, got: %v", msgs[0])
	}
}

// TestStaging_GeneratorSucceeds_AnalyzerClean_ReturnsNil verifies the happy path:
// generator succeeds, no output files to verify (mock generator writes nothing).
func TestStaging_GeneratorSucceeds_NoOutputFiles_ReturnsNil(t *testing.T) {
	fs := afero.NewMemMapFs()
	s := newTestStaging(t, fs, &systemdtest.NoopQuadletGenerator{}, &systemdtest.MockAnalyzeRunner{})
	stageUnit(s, "foo", "[Container]\nImage=nginx:latest\n")

	msgs := s.Validate(context.Background(), testlog.New(), &systemdtest.MockJournalReader{})
	if len(msgs) != 0 {
		t.Errorf("expected no messages when generator produces no output files, got: %v", msgs)
	}
}

// TestStaging_AnalyzerFails_ReturnsErrorPerFile verifies that systemd-analyze failures
// on generated unit files are surfaced as error messages.
func TestStaging_AnalyzerFails_ReturnsErrorPerFile(t *testing.T) {
	fs := afero.NewMemMapFs()
	az := &systemdtest.MockAnalyzeRunner{
		Result: systemd.AnalyzeResult{
			ExitCode: 1,
			Stderr:   "unit validation failed",
		},
	}
	gen := &systemdtest.FakeQuadletGenerator{
		Fs:    fs,
		Files: map[string]string{"foo.service": "[Service]\nExecStart=/bin/sh\n"},
	}

	s := newTestStaging(t, fs, gen, az)
	stageUnit(s, "foo", "[Container]\nImage=nginx:latest\n")

	msgs := s.Validate(context.Background(), testlog.New(), &systemdtest.MockJournalReader{})
	if len(msgs) == 0 {
		t.Fatal("expected error messages from analyzer, got none")
	}
	if !containsAll(msgs[0], "verification failed") {
		t.Errorf("expected analyzer error message, got: %v", msgs[0])
	}
}

// TestStaging_AnalyzerWarns_ReturnsWarning verifies that systemd-analyze output on
// stderr with exit code 0 (e.g. "ignoring: Invalid argument" warnings) is surfaced
// as a warning message rather than silently dropped.
func TestStaging_AnalyzerWarns_ReturnsWarning(t *testing.T) {
	fs := afero.NewMemMapFs()
	az := &systemdtest.MockAnalyzeRunner{
		Result: systemd.AnalyzeResult{
			ExitCode: 0,
			Stderr:   "mosquitto.service:8: Invalid memory limit 'asd', ignoring: Invalid argument",
		},
	}
	gen := &systemdtest.FakeQuadletGenerator{
		Fs:    fs,
		Files: map[string]string{"mosquitto.service": "[Service]\nExecStart=/bin/mosquitto\n"},
	}

	s := newTestStaging(t, fs, gen, az)
	stageUnit(s, "mosquitto", "[Container]\nImage=mosquitto:latest\n")

	msgs := s.Validate(context.Background(), testlog.New(), &systemdtest.MockJournalReader{})
	if len(msgs) == 0 {
		t.Fatal("expected warning message from analyzer stderr, got none")
	}
	if !containsAll(msgs[0], "mosquitto.service", "Invalid memory limit") {
		t.Errorf("expected warning message with unit name and diagnostic, got: %v", msgs[0])
	}
}

// TestStaging_AnalyzerSucceeds_ReturnsNil verifies that a clean analyzer result
// produces no error messages.
func TestStaging_AnalyzerSucceeds_ReturnsNil(t *testing.T) {
	fs := afero.NewMemMapFs()
	az := &systemdtest.MockAnalyzeRunner{
		Result: systemd.AnalyzeResult{ExitCode: 0},
	}
	gen := &systemdtest.FakeQuadletGenerator{
		Fs:    fs,
		Files: map[string]string{"foo.service": "[Service]\nExecStart=/bin/sh\n"},
	}

	s := newTestStaging(t, fs, gen, az)
	stageUnit(s, "foo", "[Container]\nImage=nginx:latest\n")

	msgs := s.Validate(context.Background(), testlog.New(), &systemdtest.MockJournalReader{})
	if len(msgs) != 0 {
		t.Errorf("expected no messages when analyzer passes, got: %v", msgs)
	}
}

// TestStaging_MultipleOutputFiles_AllAnalyzed verifies that all generated files in
// the output directory are passed to systemd-analyze.
func TestStaging_MultipleOutputFiles_AllAnalyzed(t *testing.T) {
	fs := afero.NewMemMapFs()

	var analyzedPaths []string
	az := &countingAnalyzeRunner{
		inner:   &systemdtest.MockAnalyzeRunner{Result: systemd.AnalyzeResult{ExitCode: 0}},
		visited: &analyzedPaths,
	}
	gen := &systemdtest.FakeQuadletGenerator{
		Fs: fs,
		Files: map[string]string{
			"foo.service": "[Service]\nExecStart=/bin/sh\n",
			"bar.service": "[Service]\nExecStart=/bin/sh\n",
		},
	}

	s := newTestStaging(t, fs, gen, az)
	stageUnit(s, "foo", "[Container]\nImage=nginx:latest\n")
	stageUnit(s, "bar", "[Container]\nImage=nginx:latest\n")

	msgs := s.Validate(context.Background(), testlog.New(), &systemdtest.MockJournalReader{})
	if len(msgs) != 0 {
		t.Errorf("expected no error messages, got: %v", msgs)
	}
	if len(analyzedPaths) != 2 {
		t.Errorf("expected 2 files analyzed, got %d: %v", len(analyzedPaths), analyzedPaths)
	}
}

// countingAnalyzeRunner wraps a real runner and records each path passed to Verify.
type countingAnalyzeRunner struct {
	inner   systemd.AnalyzeRunner
	visited *[]string
}

func (r *countingAnalyzeRunner) Verify(ctx context.Context, paths []string) (systemd.AnalyzeResult, error) {
	*r.visited = append(*r.visited, paths...)
	return r.inner.Verify(ctx, paths)
}

// containsAll returns true if s contains all of the given substrings.
func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
