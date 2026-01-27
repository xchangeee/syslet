// Package journal reads logs from journalctl for managed units.
package journal

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Entry represents a single journal log entry.
type Entry struct {
	Timestamp string
	Message   string
	Priority  string
}

// Read reads recent log entries for a unit.
func Read(ctx context.Context, unitName string, lines int) ([]Entry, error) {
	if lines <= 0 {
		lines = 100
	}

	cmd := exec.CommandContext(ctx, "journalctl",
		"-u", unitName,
		"-n", fmt.Sprintf("%d", lines),
		"--no-pager",
		"-o", "short-iso",
	)

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("journalctl: %w", err)
	}

	return parseOutput(string(out)), nil
}

// Follow streams log entries for a unit. Entries are sent to the channel
// until the context is cancelled.
func Follow(ctx context.Context, unitName string, lines int, ch chan<- Entry) error {
	if lines <= 0 {
		lines = 100
	}

	cmd := exec.CommandContext(ctx, "journalctl",
		"-u", unitName,
		"-n", fmt.Sprintf("%d", lines),
		"-f",
		"--no-pager",
		"-o", "short-iso",
	)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("creating pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting journalctl: %w", err)
	}

	go func() {
		defer close(ch)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			entry := parseLine(scanner.Text())
			select {
			case ch <- entry:
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		<-ctx.Done()
		_ = cmd.Process.Kill()
	}()

	return cmd.Wait()
}

func parseOutput(output string) []Entry {
	var entries []Entry
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "-- ") {
			continue
		}
		entries = append(entries, parseLine(line))
	}
	return entries
}

func parseLine(line string) Entry {
	// journalctl short-iso format: "2024-01-15T10:30:00+0000 hostname unit[pid]: message"
	// We extract timestamp (first field) and message (everything after the unit[pid]:)
	parts := strings.SplitN(line, " ", 4)
	if len(parts) < 4 {
		return Entry{Message: line}
	}

	timestamp := parts[0]
	message := parts[3]

	// Try to extract after "unit[pid]: "
	if idx := strings.Index(message, ": "); idx >= 0 {
		message = message[idx+2:]
	}

	return Entry{
		Timestamp: timestamp,
		Message:   message,
		Priority:  "info", // journalctl short-iso doesn't include priority; could use -o json
	}
}
