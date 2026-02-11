// syslet reads a zip file or directory containing JSON spec files, validates them,
// generates Podman quadlet unit files and container config files, prunes
// outdated files, and restarts systemd units as needed.
//
// Usage:
//
//	syslet [--diff] [config.zip|config-dir/]
//
// If no path is given, defaults to /etc/syslet/config.zip.
// The path can be either a zip file or a directory containing .json spec files.
package main

import (
	"context"
	"flag"
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
	diffFlag := flag.Bool("diff", false, "Show what would change without applying (dry-run)")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	configPath := defaultConfigPath
	if flag.NArg() > 0 {
		configPath = flag.Arg(0)
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

	// Build the plan once
	plan, err := syslet.BuildPlan(ctx, fs, sd, configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if *diffFlag {
		// Show diff without applying changes
		syslet.Diff(plan)
	} else {
		// Apply changes
		if err := syslet.Apply(ctx, logger, fs, sd, pc, plan); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	}
}
