// syslet reads a zip file or directory containing JSON spec files, validates them,
// generates Podman quadlet unit files and container config files, prunes
// outdated files, and restarts systemd units as needed.
//
// Usage:
//
//	syslet [config.zip|config-dir/]
//
// If no path is given, defaults to /etc/syslet/config.zip.
// The path can be either a zip file or a directory containing .json spec files.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"codeberg.org/xchangeee/syslet/internal/podman"
	"codeberg.org/xchangeee/syslet/internal/syslet"
	"codeberg.org/xchangeee/syslet/internal/systemd"
	"github.com/spf13/afero"
)

const defaultConfigPath = "/etc/syslet/config.zip"

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	configPath := defaultConfigPath
	if len(os.Args) > 1 {
		configPath = os.Args[1]
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
	pc := podman.New()

	if err := syslet.Apply(ctx, logger, fs, sd, pc, configPath); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
