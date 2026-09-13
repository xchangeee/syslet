package systemd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQuadletGenerator_Run_BinaryNotFound(t *testing.T) {
	g := &QuadletGenerator{bin: "/nonexistent/podman-system-generator"}
	code, err := g.Run(context.Background(), t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir())
	if err == nil {
		t.Fatal("expected error when binary not found, got nil")
	}
	if code != -1 {
		t.Errorf("expected exit code -1, got %d", code)
	}
}

func TestQuadletGenerator_Run_ExitCodeReturned(t *testing.T) {
	script := filepath.Join(t.TempDir(), "fake-generator")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho 'out line'\necho 'err line' >&2\nexit 2\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	g := &QuadletGenerator{bin: script}
	code, err := g.Run(context.Background(), t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir())
	if err == nil {
		t.Fatal("expected error for non-zero exit, got nil")
	}
	if code != 2 {
		t.Errorf("expected exit code 2, got %d", code)
	}
	if !strings.Contains(err.Error(), "out line") {
		t.Errorf("error missing stdout: %v", err)
	}
	if !strings.Contains(err.Error(), "err line") {
		t.Errorf("error missing stderr: %v", err)
	}
}

func TestQuadletGenerator_Run_Success(t *testing.T) {
	script := filepath.Join(t.TempDir(), "fake-generator")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	g := &QuadletGenerator{bin: script}
	code, err := g.Run(context.Background(), t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
}

// TestQuadletGenerator_Run_PassesArgs verifies that the generator receives the
// three output directories as positional arguments and unitDir via the
// QUADLET_UNIT_DIRS environment variable.
func TestQuadletGenerator_Run_PassesArgs(t *testing.T) {
	tmp := t.TempDir()
	out := filepath.Join(tmp, "args.txt")

	// The script writes its argv[1..3] and the env var into a file so we can
	// inspect them after the process exits.
	script := filepath.Join(tmp, "fake-generator")
	content := "#!/bin/sh\nprintf '%s\\n' \"$1\" \"$2\" \"$3\" \"$QUADLET_UNIT_DIRS\" > " + out + "\nexit 0\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}

	unitDir := filepath.Join(tmp, "unit")
	earlyDir := filepath.Join(tmp, "early")
	normalDir := filepath.Join(tmp, "normal")
	lateDir := filepath.Join(tmp, "late")

	g := &QuadletGenerator{bin: script}
	if _, err := g.Run(context.Background(), unitDir, earlyDir, normalDir, lateDir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("reading args file: %v", err)
	}

	want := earlyDir + "\n" + normalDir + "\n" + lateDir + "\n" + unitDir + "\n"
	if string(data) != want {
		t.Errorf("args mismatch\ngot:  %q\nwant: %q", string(data), want)
	}
}
