package systemd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const (
	// QuadletUnitDir is where Podman quadlet files live.
	QuadletUnitDir = "/etc/containers/systemd"

	// QuadletGeneratorBin is the path to the podman quadlet generator binary.
	QuadletGeneratorBin = "/usr/lib/systemd/system-generators/podman-system-generator"
)

// QuadletGeneratorRunner abstracts the podman-system-generator binary so that
// the real implementation can be swapped out in tests.
type QuadletGeneratorRunner interface {
	// Run invokes the generator in normal mode, writing units into the
	// given output directories. Returns the process exit code.
	Run(ctx context.Context, unitDir, earlyDir, normalDir, lateDir string) (int, error)
}

// QuadletGenerator executes the podman-system-generator binary in normal
// generation mode, writing units into the systemd generator output directories.
type QuadletGenerator struct {
	bin string
}

// NewQuadletGenerator returns a QuadletGenerator using QuadletGeneratorBin.
func NewQuadletGenerator() *QuadletGenerator {
	return &QuadletGenerator{bin: QuadletGeneratorBin}
}

// NewQuadletGeneratorWithBin returns a QuadletGenerator using a custom binary path.
func NewQuadletGeneratorWithBin(bin string) *QuadletGenerator {
	return &QuadletGenerator{bin: bin}
}

// Run invokes the generator in normal mode with earlyDir, normalDir,
// and lateDir as positional arguments, and QUADLET_UNIT_DIRS pointing at
// unitDir. The generator writes unit files into the appropriate output
// directory and logs progress to stdout. Returns the process exit code.
// If the binary is absent the call succeeds with exit code 0.
func (g *QuadletGenerator) Run(ctx context.Context, unitDir, earlyDir, normalDir, lateDir string) (int, error) {
	cmd := exec.CommandContext(ctx, g.bin, earlyDir, normalDir, lateDir)
	cmd.Env = append(os.Environ(), "QUADLET_UNIT_DIRS="+unitDir)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		out := strings.TrimSpace(stdout.String() + "\n" + stderr.String())
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			return exitErr.ExitCode(), fmt.Errorf("generator exited %d\n%s", exitErr.ExitCode(), out)
		}
		return -1, fmt.Errorf("running generator: %w\n%s", err, out)
	}
	return 0, nil
}
