package systemd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

const (
	// SystemdAnalyzeBin is the path to the systemd-analyze binary.
	SystemdAnalyzeBin = "systemd-analyze"
)

// AnalyzeRunner abstracts the systemd-analyze binary so that the real
// implementation can be swapped out in tests.
type AnalyzeRunner interface {
	// Verify runs "systemd-analyze verify" on one or more unit files and returns
	// the captured stdout, stderr, and process exit code. A non-zero exit code
	// means at least one unit failed verification. An error is only returned for
	// execution failures unrelated to the unit content (e.g. binary not found).
	// Passing multiple paths in a single call avoids duplicate output: systemd-analyze
	// loads the full dependency graph per invocation, so per-file calls produce the
	// same combined error output for every file.
	Verify(ctx context.Context, unitPaths []string) (AnalyzeResult, error)
}

// AnalyzeResult holds the captured output of a systemd-analyze verify call.
type AnalyzeResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Analyze executes the systemd-analyze binary.
type Analyze struct {
	bin string
}

// NewAnalyze returns a SystemdAnalyze using SystemdAnalyzeBin.
func NewAnalyze() *Analyze {
	return &Analyze{bin: SystemdAnalyzeBin}
}

// Verify runs "systemd-analyze verify" on the given unit file paths, capturing
// stdout and stderr separately. A non-zero exit code is returned in the result
// rather than as an error. If the binary is not found the call returns an error
// wrapping exec.ErrNotFound so callers can skip verification gracefully.
func (a *Analyze) Verify(ctx context.Context, unitPaths []string) (AnalyzeResult, error) {
	args := append([]string{"verify"}, unitPaths...)
	cmd := exec.CommandContext(ctx, a.bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := AnalyzeResult{
		Stdout: strings.TrimSpace(stdout.String()),
		Stderr: strings.TrimSpace(stderr.String()),
	}
	if err != nil {
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			result.ExitCode = exitErr.ExitCode()
			return result, nil
		}
		return result, fmt.Errorf("running systemd-analyze: %w", err)
	}
	return result, nil
}
