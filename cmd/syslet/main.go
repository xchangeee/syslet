// syslet reads a zip file containing JSON spec files, validates them,
// generates Podman quadlet unit files and container config files, prunes
// outdated files, and restarts systemd units as needed.
//
// Usage:
//
//	syslet [config.zip]
//
// If no path is given, defaults to /etc/syslet/config.zip.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"codeberg.org/xchangeee/syslet/internal/syslet"
	"codeberg.org/xchangeee/syslet/internal/systemd"
	"github.com/spf13/afero"
)

const defaultZipPath = "/etc/syslet/config.zip"

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	zipPath := defaultZipPath
	if len(os.Args) > 1 {
		zipPath = os.Args[1]
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	fs := afero.NewOsFs()

	dbusConn, err := systemd.NewDBusConnection(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to connect to systemd: %v\n", err)
		os.Exit(1)
	}
	defer dbusConn.Close()

	sd := systemd.NewClient(dbusConn, fs)

	if err := syslet.Apply(ctx, logger, fs, sd, zipPath); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
