// syslet reads JSON spec files, validates them, generates Podman quadlet unit
// files and container config files, prunes outdated files, and restarts systemd
// units as needed.
//
// Usage:
//
//	syslet [--diff] [config.json|config-dir/]
//	cat dir/*.json | syslet [--diff] --stdin
//
// If no path or --stdin is given, defaults to /etc/syslet/config.json.
// A path may be a directory of .json spec files or a single file containing a
// JSON stream of specs. With --stdin the spec stream is read from standard input
// and, on apply, persisted to /etc/syslet/config.json as the host's on-disk
// record so it can be re-applied manually.
//
// Optional daemon config is read from /etc/syslet/syslet.json:
//
//	{"sshKeyPath": "/etc/ssh/ssh_host_ed25519_key"}
//
// When sshKeyPath is set, syslet derives an age identity from the key and uses
// it to decrypt SOPS-encrypted secret specs.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/afero"

	sysletage "codeberg.org/xchangeee/syslet/internal/age"
	"codeberg.org/xchangeee/syslet/internal/api"
	"codeberg.org/xchangeee/syslet/internal/filestore"
	"codeberg.org/xchangeee/syslet/internal/podman"
	"codeberg.org/xchangeee/syslet/internal/sops"
	"codeberg.org/xchangeee/syslet/internal/syslet"
	"codeberg.org/xchangeee/syslet/internal/systemd"
)

const defaultConfigPath = "/etc/syslet/config.json"
const daemonConfigPath = "/etc/syslet/syslet.json"

// ageKeyFilePath is the append-only cache of derived age private keys.
// Old keys are kept across SSH key rotations so previously-encrypted secrets
// remain decryptable.
const ageKeyFilePath = "/var/lib/syslet/key.txt"

// daemonConfig holds optional daemon-level configuration read from daemonConfigPath.
type daemonConfig struct {
	SSHKeyPath string `json:"sshKeyPath"`
}

// defaultSSHKeyPath is used when syslet.json is absent or does not set sshKeyPath.
const defaultSSHKeyPath = "/etc/ssh/ssh_host_ed25519_key"

// loadDaemonConfig reads daemonConfigPath. Returns a config with defaults applied
// when the file does not exist, so syslet works without a config file.
func loadDaemonConfig(fs afero.Fs) (daemonConfig, error) {
	data, err := afero.ReadFile(fs, daemonConfigPath)
	if os.IsNotExist(err) {
		return daemonConfig{SSHKeyPath: defaultSSHKeyPath}, nil
	}
	if err != nil {
		return daemonConfig{}, fmt.Errorf("reading %s: %w", daemonConfigPath, err)
	}
	var cfg daemonConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return daemonConfig{}, fmt.Errorf("parsing %s: %w", daemonConfigPath, err)
	}
	if cfg.SSHKeyPath == "" {
		cfg.SSHKeyPath = defaultSSHKeyPath
	}
	return cfg, nil
}

func main() {
	diffFlag := flag.Bool("diff", false, "Show what would change without applying (dry-run)")
	stdinFlag := flag.Bool("stdin", false, "Read the JSON spec stream from standard input")
	flag.Parse()

	if *stdinFlag && flag.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "error: --stdin and a config path are mutually exclusive\n")
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	configPath := defaultConfigPath
	if flag.NArg() > 0 {
		configPath = flag.Arg(0)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	fs := afero.NewOsFs()

	cfg, err := loadDaemonConfig(fs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	mgrs := filestore.FileManagers{
		Config: filestore.NewContainerConfigFileStore(fs),
		Build:  filestore.NewBuildContextFileStore(fs),
	}

	dbusConn, err := systemd.NewDBusConnection(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to connect to systemd: %v\n", err)
		os.Exit(1)
	}
	defer dbusConn.Close()
	sd := systemd.NewClient(dbusConn, fs)
	jr := systemd.NewJournalReader()
	sq := systemd.NewQuadletGenerator()
	sa := systemd.NewSystemdAnalyze()

	pc := podman.New()

	// Build the decryptor when the SSH key exists. If the key is absent (e.g. the
	// host has no ed25519 key and no config override), we skip silently — the plan
	// phase will emit an error only if secrets are actually present in the spec.
	var decryptor *sops.Decryptor
	if _, statErr := fs.Stat(cfg.SSHKeyPath); statErr == nil {
		ageKey, err := sysletage.IdentityFromSSHKey(fs, cfg.SSHKeyPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: deriving age identity from SSH key: %v\n", err)
			os.Exit(1)
		}
		if _, err := sysletage.AppendToKeyFile(fs, ageKeyFilePath, ageKey); err != nil {
			fmt.Fprintf(os.Stderr, "error: updating age key file: %v\n", err)
			os.Exit(1)
		}
		decryptor = sops.NewDecryptor(ageKeyFilePath)
	}

	// Load the spec stream from stdin or the config path. On a stdin apply we
	// persist the stream to defaultConfigPath as the host's re-applyable on-disk
	// record; a diff is a dry-run and persists nothing (empty persist path).
	var raw api.LoadResult
	if *stdinFlag {
		persistPath := ""
		if !*diffFlag {
			persistPath = defaultConfigPath
		}
		raw, err = api.LoadSpecsStream(fs, os.Stdin, persistPath)
	} else {
		raw, err = api.LoadSpecsFS(fs, configPath)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	plan, err := syslet.BuildPlan(ctx, fs, mgrs, sd, jr, sq, sa, pc, decryptor, raw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if *diffFlag {
		// Show diff without applying changes
		syslet.DisplayPlan(os.Stdout, plan)
	} else {
		// Apply changes
		err := syslet.Apply(ctx, logger, sd, jr, pc, mgrs, plan)
		syslet.DisplayResults(os.Stdout, plan)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	}
}
