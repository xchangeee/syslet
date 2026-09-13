package systemd

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSystemdAnalyze_Verify_BinaryNotFound(t *testing.T) {
	a := &SystemdAnalyze{bin: "/nonexistent/systemd-analyze"}
	_, err := a.Verify(context.Background(), []string{"/some/unit.service"})
	if err == nil {
		t.Fatal("expected error when binary not found, got nil")
	}
}

func TestSystemdAnalyze_Verify_ExitCodeReturned(t *testing.T) {
	// Use a script that exits with a non-zero code to simulate a failed verify.
	script := filepath.Join(t.TempDir(), "fake-analyze")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho 'bad unit' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	a := &SystemdAnalyze{bin: script}
	result, err := a.Verify(context.Background(), []string{"/some/unit.service"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 1 {
		t.Errorf("expected ExitCode 1, got %d", result.ExitCode)
	}
	if result.Stderr != "bad unit" {
		t.Errorf("expected stderr 'bad unit', got %q", result.Stderr)
	}
}

func TestSystemdAnalyze_Verify_Success(t *testing.T) {
	script := filepath.Join(t.TempDir(), "fake-analyze")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho 'ok'\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	a := &SystemdAnalyze{bin: script}
	result, err := a.Verify(context.Background(), []string{"/some/unit.service"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected ExitCode 0, got %d", result.ExitCode)
	}
	if result.Stdout != "ok" {
		t.Errorf("expected stdout 'ok', got %q", result.Stdout)
	}
}
