// syslet reads JSON spec files, validates them, generates Podman quadlet unit
// files and container config files, prunes outdated files, and restarts systemd
// units as needed.
//
// Usage:
//
//	syslet [--diff] [config.json|config-dir/]
//	cat dir/*.json | syslet [--diff] --stdin
//	syslet --version
//
// If no path or --stdin is given, defaults to /etc/syslet/config.json.
// --diff hides secret values unless SYSLET_SHOW_SECRETS is set to a true value.
// A path may be a directory of .json spec files or a single file containing a
// JSON stream of specs. With --stdin the spec stream is read from standard input
// and, on apply, persisted to /etc/syslet/config.json as the host's on-disk
// record so it can be re-applied manually.
//
// Optional daemon config is read from /etc/syslet/syslet.json:
//
//	{"sshKeyPath": "/etc/ssh/ssh_host_ed25519_key"}
//
// syslet derives an age identity from the key at sshKeyPath (default
// /etc/ssh/ssh_host_ed25519_key) and uses it to decrypt SOPS-encrypted secret
// specs. An explicitly configured sshKeyPath must exist; a missing default key
// only draws a warning, and keys cached from earlier runs are still used.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"
	"strconv"
	"syscall"

	"github.com/spf13/afero"

	sysletage "github.com/xchangeee/syslet/internal/age"
	"github.com/xchangeee/syslet/internal/api"
	"github.com/xchangeee/syslet/internal/filestore"
	"github.com/xchangeee/syslet/internal/podman"
	"github.com/xchangeee/syslet/internal/sops"
	"github.com/xchangeee/syslet/internal/syslet"
	"github.com/xchangeee/syslet/internal/systemd"
)

const defaultConfigPath = "/etc/syslet/config.json"
const daemonConfigPath = "/etc/syslet/syslet.json"

// ageKeyFilePath is the append-only cache of derived age private keys.
// Old keys are kept across SSH key rotations so previously-encrypted secrets
// remain decryptable.
const ageKeyFilePath = "/var/lib/syslet/key.txt"

// daemonConfig holds optional daemon-level configuration read from daemonConfigPath.
// An empty SSHKeyPath means it was not configured, so newDecryptor falls back to
// defaultSSHKeyPath and treats a missing key more leniently.
type daemonConfig struct {
	SSHKeyPath string `json:"sshKeyPath"`
}

// defaultSSHKeyPath is used when syslet.json is absent or does not set sshKeyPath.
const defaultSSHKeyPath = "/etc/ssh/ssh_host_ed25519_key"

// loadDaemonConfig reads daemonConfigPath. Returns an empty config when the file
// does not exist, so syslet works without a config file.
func loadDaemonConfig(fs afero.Fs) (daemonConfig, error) {
	data, err := afero.ReadFile(fs, daemonConfigPath)
	if os.IsNotExist(err) {
		return daemonConfig{}, nil
	}
	if err != nil {
		return daemonConfig{}, fmt.Errorf("reading %s: %w", daemonConfigPath, err)
	}
	var cfg daemonConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return daemonConfig{}, fmt.Errorf("parsing %s: %w", daemonConfigPath, err)
	}
	return cfg, nil
}

// newDecryptor builds the SOPS decryptor that BuildPlan uses for secret specs.
// It derives an age identity from the SSH key, appends it to the append-only
// cache at ageKeyFilePath, and decrypts with every cached key, so secrets
// encrypted to a rotated-out host key keep working.
//
// A missing key is only tolerated when sshKeyPath was not configured: a host
// without an ed25519 key can still run specs without secrets, so syslet warns
// and falls back to the cached keys, or returns nil when there are none and
// lets the plan reject any secret spec. An explicitly configured path that is
// missing, or any stat error other than not-exist, is an error naming the path.
func newDecryptor(fs afero.Fs, logger *slog.Logger, cfg daemonConfig) (*sops.Decryptor, error) {
	sshKeyPath := cfg.SSHKeyPath
	if sshKeyPath == "" {
		sshKeyPath = defaultSSHKeyPath
	}
	_, statErr := fs.Stat(sshKeyPath)
	switch {
	case statErr == nil:
		ageKey, err := sysletage.IdentityFromSSHKey(fs, sshKeyPath)
		if err != nil {
			return nil, fmt.Errorf("deriving age identity from SSH key: %w", err)
		}
		if _, err := sysletage.AppendToKeyFile(fs, ageKeyFilePath, ageKey); err != nil {
			return nil, fmt.Errorf("updating age key file: %w", err)
		}
		return sops.NewDecryptor(ageKeyFilePath), nil
	case !errors.Is(statErr, os.ErrNotExist):
		return nil, fmt.Errorf("checking SSH key %s: %w", sshKeyPath, statErr)
	case cfg.SSHKeyPath != "":
		return nil, fmt.Errorf("SSH key %s (sshKeyPath in %s) does not exist", sshKeyPath, daemonConfigPath)
	}

	cached, err := afero.Exists(fs, ageKeyFilePath)
	if err != nil {
		return nil, fmt.Errorf("checking age key file %s: %w", ageKeyFilePath, err)
	}
	if !cached {
		logger.Warn("SSH key not found and no cached age keys; secret specs cannot be decrypted", "sshKeyPath", sshKeyPath)
		return nil, nil
	}
	logger.Warn("SSH key not found; decrypting secrets with cached age keys only", "sshKeyPath", sshKeyPath, "keyFile", ageKeyFilePath)
	return sops.NewDecryptor(ageKeyFilePath), nil
}

// Build metadata injected by GoReleaser's default ldflags
// (-X main.version=... -X main.commit=... -X main.date=...). Empty in builds
// that don't set them; versionString then falls back to the Go build info.
var (
	version string
	commit  string
	date    string
)

// versionString formats the --version output. Release builds report the
// GoReleaser ldflags; `go install module@vX` and local builds report the module
// version and VCS stamp that the Go toolchain embeds. info is nil when the
// binary carries no build info.
func versionString(version, commit, date string, info *debug.BuildInfo) string {
	if version == "" && info != nil {
		version = info.Main.Version
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				commit = s.Value
			case "vcs.time":
				date = s.Value
			}
		}
	}
	if version == "" {
		version = "dev"
	}
	out := "syslet " + version
	if commit != "" {
		out += fmt.Sprintf(" (commit %s, built %s)", commit, date)
	}
	return out
}

// showSecretsEnv names the environment variable that makes --diff print
// decrypted secret values. Unset, the plan hides them so it is safe for shared
// CI logs; setting it is meant for inspecting a plan locally.
const showSecretsEnv = "SYSLET_SHOW_SECRETS"

// displayOptionsFromEnv builds the plan display options from the environment.
// An unparseable value is an error rather than a silent default, so a typo
// can't leave someone guessing why values are (or aren't) shown.
func displayOptionsFromEnv(getenv func(string) string) (syslet.DisplayOptions, error) {
	var opts syslet.DisplayOptions
	if v := getenv(showSecretsEnv); v != "" {
		show, err := strconv.ParseBool(v)
		if err != nil {
			return opts, fmt.Errorf("%s: %w", showSecretsEnv, err)
		}
		opts.ShowSecrets = show
	}
	return opts, nil
}

// diff prints the plan for a --diff dry-run and reports whether it would be
// refused. It mirrors the error check in syslet.Apply so a dry-run and a real
// apply agree on the exit code, which lets CI gate on `syslet --diff`.
func diff(w io.Writer, plan *syslet.ApplyPlan, opts syslet.DisplayOptions) error {
	syslet.DisplayPlan(w, plan, opts)
	if plan.HasErrors() {
		return fmt.Errorf("plan has errors")
	}
	return nil
}

func main() {
	diffFlag := flag.Bool("diff", false, "Show what would change without applying (dry-run)")
	stdinFlag := flag.Bool("stdin", false, "Read the JSON spec stream from standard input")
	versionFlag := flag.Bool("version", false, "Print the version and exit")
	flag.Parse()

	if *versionFlag {
		info, _ := debug.ReadBuildInfo()
		fmt.Println(versionString(version, commit, date, info))
		return
	}

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
	sa := systemd.NewAnalyze()

	pc := podman.New()

	// A nil decryptor is fine as long as no spec carries a secret; the plan
	// phase reports an error only if one does.
	decryptor, err := newDecryptor(fs, logger, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
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
		opts, err := displayOptionsFromEnv(os.Getenv)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		if err := diff(os.Stdout, plan, opts); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
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
