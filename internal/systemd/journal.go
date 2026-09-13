package systemd

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// JournalReader reads journal entries produced during a daemon-reload. It is
// injectable so tests can provide a mock without spawning a real journalctl.
type JournalReader interface {
	// QuadletErrorsSince returns all messages logged by quadlet-generator
	// after the given time, or nil if none were found.
	QuadletErrorsSince(ctx context.Context, since time.Time) ([]string, error)
}

// JournalctlReader is the real JournalReader that shells out to journalctl.
type JournalctlReader struct {
	bin string
}

// NewJournalReader returns a JournalctlReader using the system journalctl binary.
func NewJournalReader() *JournalctlReader {
	return &JournalctlReader{bin: "journalctl"}
}

// NewJournalReaderWithBin returns a JournalctlReader using a custom binary path.
func NewJournalReaderWithBin(bin string) *JournalctlReader {
	return &JournalctlReader{bin: bin}
}

func (r *JournalctlReader) QuadletErrorsSince(ctx context.Context, since time.Time) ([]string, error) {
	sinceStr := since.Local().Format("2006-01-02 15:04:05")
	cmd := exec.CommandContext(ctx,
		r.bin, "-b", "-t", "quadlet-generator",
		"--since="+sinceStr, "-o", "cat", "--no-pager",
	)
	out, err := cmd.Output()
	if err != nil {
		// journalctl is not present on non-Linux hosts; treat as no entries.
		if errors.Is(err, exec.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("journalctl: %w", err)
	}
	var lines []string
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines, nil
}
