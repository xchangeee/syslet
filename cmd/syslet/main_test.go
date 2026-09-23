package main

import (
	"bytes"
	"log/slog"
	"os"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/spf13/afero"

	"github.com/xchangeee/syslet/internal/syslet"
)

func TestVersionString(t *testing.T) {
	tests := []struct {
		name                  string
		version, commit, date string
		info                  *debug.BuildInfo
		want                  string
	}{
		{
			name:    "LdflagsFromGoReleaser",
			version: "1.2.3", commit: "abc123", date: "2026-09-23T10:00:00Z",
			want: "syslet 1.2.3 (commit abc123, built 2026-09-23T10:00:00Z)",
		},
		{
			name: "GoInstallModuleVersion",
			info: &debug.BuildInfo{Main: debug.Module{Version: "v1.2.3"}},
			want: "syslet v1.2.3",
		},
		{
			name: "LocalBuildWithVCSInfo",
			info: &debug.BuildInfo{
				Main: debug.Module{Version: "(devel)"},
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "def456"},
					{Key: "vcs.time", Value: "2026-09-22T08:00:00Z"},
				},
			},
			want: "syslet (devel) (commit def456, built 2026-09-22T08:00:00Z)",
		},
		{
			name: "NoBuildInfo",
			want: "syslet dev",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := versionString(tt.version, tt.commit, tt.date, tt.info)
			if got != tt.want {
				t.Errorf("versionString() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDisplayOptionsFromEnv(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    bool
		wantErr bool
	}{
		{name: "Unset", value: "", want: false},
		{name: "True", value: "1", want: true},
		{name: "False", value: "false", want: false},
		{name: "Invalid", value: "yes please", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(key string) string {
				if key == showSecretsEnv {
					return tt.value
				}
				return ""
			}
			opts, err := displayOptionsFromEnv(getenv)
			if (err != nil) != tt.wantErr {
				t.Fatalf("displayOptionsFromEnv() error = %v, wantErr %v", err, tt.wantErr)
			}
			if opts.ShowSecrets != tt.want {
				t.Errorf("ShowSecrets = %v, want %v", opts.ShowSecrets, tt.want)
			}
		})
	}
}

func TestDiff(t *testing.T) {
	t.Run("PlanWithoutErrors", func(t *testing.T) {
		var out bytes.Buffer
		if err := diff(&out, &syslet.ApplyPlan{}, syslet.DisplayOptions{}); err != nil {
			t.Errorf("diff() error = %v, want nil", err)
		}
		if !strings.Contains(out.String(), "Summary:") {
			t.Errorf("diff() output = %q, want plan summary", out.String())
		}
	})

	t.Run("PlanWithErrors", func(t *testing.T) {
		plan := &syslet.ApplyPlan{}
		plan.RecordGenericError("spec web: image is required")
		var out bytes.Buffer
		if err := diff(&out, plan, syslet.DisplayOptions{}); err == nil {
			t.Error("diff() error = nil, want error for plan with validation errors")
		}
		if !strings.Contains(out.String(), "spec web: image is required") {
			t.Errorf("diff() output = %q, want validation error listed", out.String())
		}
	})
}

// statErrFs fails every Stat with a permission error, standing in for a key
// path syslet may not inspect.
type statErrFs struct{ afero.Fs }

func (statErrFs) Stat(name string) (os.FileInfo, error) {
	return nil, &os.PathError{Op: "stat", Path: name, Err: os.ErrPermission}
}

func TestNewDecryptor(t *testing.T) {
	sshKey, err := os.ReadFile("../../internal/age/testdata/id_ed25519")
	if err != nil {
		t.Fatalf("reading SSH key fixture: %v", err)
	}
	const cachedKey = "AGE-SECRET-KEY-1CACHED\n"
	const explicitPath = "/etc/ssh/custom_key"

	tests := []struct {
		name          string
		cfg           daemonConfig
		files         map[string][]byte
		statErr       bool
		wantDecryptor bool
		wantErr       string
		wantWarn      string
	}{
		{
			name:          "ExplicitKeyPresent",
			cfg:           daemonConfig{SSHKeyPath: explicitPath},
			files:         map[string][]byte{explicitPath: sshKey},
			wantDecryptor: true,
		},
		{
			name:          "DefaultKeyPresent",
			files:         map[string][]byte{defaultSSHKeyPath: sshKey},
			wantDecryptor: true,
		},
		{
			name:    "ExplicitKeyMissingFails",
			cfg:     daemonConfig{SSHKeyPath: explicitPath},
			files:   map[string][]byte{ageKeyFilePath: []byte(cachedKey)},
			wantErr: explicitPath,
		},
		{
			name:     "DefaultKeyMissingWarns",
			wantWarn: defaultSSHKeyPath,
		},
		{
			name:          "DefaultKeyMissingUsesCachedKeys",
			files:         map[string][]byte{ageKeyFilePath: []byte(cachedKey)},
			wantDecryptor: true,
			wantWarn:      defaultSSHKeyPath,
		},
		{
			name:    "StatErrorFails",
			cfg:     daemonConfig{SSHKeyPath: explicitPath},
			statErr: true,
			wantErr: explicitPath,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			for path, data := range tt.files {
				if err := afero.WriteFile(fs, path, data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if tt.statErr {
				fs = statErrFs{fs}
			}
			var logs bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logs, nil))

			d, err := newDecryptor(fs, logger, tt.cfg)

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected an error naming %q, got: %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := d != nil; got != tt.wantDecryptor {
				t.Errorf("decryptor built = %v, want %v", got, tt.wantDecryptor)
			}
			if tt.wantWarn != "" && !strings.Contains(logs.String(), tt.wantWarn) {
				t.Errorf("expected a warning naming %q, got:\n%s", tt.wantWarn, logs.String())
			}
			if tt.wantWarn == "" && logs.Len() > 0 {
				t.Errorf("expected no log output, got:\n%s", logs.String())
			}
		})
	}
}
