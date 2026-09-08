// Package podman wraps podman CLI operations for managing volumes, networks, and secrets.
// It provides the low-level building blocks for interacting with podman resources
// that are referenced by quadlet units but need to be managed separately from systemd.
package podman

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"codeberg.org/xchangeee/syslet/internal/model"
)

// PodmanSecretMeta holds the name and labels of a single entry in podman's secret store.
// It is used during the plan phase to detect which secrets already exist and whether
// their content has changed (via the syslet/hash label).
type PodmanSecretMeta struct {
	Name   string            `json:"Name"`
	Labels map[string]string `json:"Labels"`
}

// Interface defines the operations for managing podman resources.
// This interface allows for mocking in tests.
type Interface interface {
	DeleteVolume(ctx context.Context, name string) error
	DeleteNetwork(ctx context.Context, name string) error
	DeleteImage(ctx context.Context, tag string) error
	ListSecrets(ctx context.Context) ([]PodmanSecretMeta, error)
	UpsertSecret(ctx context.Context, name string, value model.Plaintext, labels map[string]string) error
	DeleteSecret(ctx context.Context, name string) error
}

// Client wraps podman CLI operations for managing volumes and networks.
// Unlike quadlet units which are managed via systemd, volumes and networks
// are created and managed by podman itself and require direct CLI interaction.
type Client struct{}

// New creates a new podman client.
func New() *Client {
	return &Client{}
}

// DeleteVolume executes 'podman volume rm <name>' to delete a volume.
// This is used to clean up volumes when their quadlet unit is removed
// and ReclaimPolicy is set to "Delete".
func (c *Client) DeleteVolume(ctx context.Context, name string) error {
	cmd := exec.CommandContext(ctx, "podman", "volume", "rm", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("podman volume rm %s failed: %w (output: %s)", name, err, string(output))
	}
	return nil
}

// DeleteNetwork executes 'podman network rm <name>' to delete a network.
// This is used to clean up networks when their quadlet unit is removed
// and ReclaimPolicy is set to "Delete".
func (c *Client) DeleteNetwork(ctx context.Context, name string) error {
	cmd := exec.CommandContext(ctx, "podman", "network", "rm", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("podman network rm %s failed: %w (output: %s)", name, err, string(output))
	}
	return nil
}

// DeleteImage executes 'podman rmi <tag>' to delete an image.
// This is used to clean up built images when their build unit is removed
// and ReclaimPolicy is set to "Delete".
func (c *Client) DeleteImage(ctx context.Context, tag string) error {
	cmd := exec.CommandContext(ctx, "podman", "rmi", tag)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("podman rmi %s failed: %w (output: %s)", tag, err, string(output))
	}
	return nil
}

// ListSecrets returns name and labels for every secret currently in the podman secret store.
// The plan phase calls this once and filters the result in memory by specname prefix, consistent
// with how syslet handles volumes and networks.
func (c *Client) ListSecrets(ctx context.Context) ([]PodmanSecretMeta, error) {
	lsCmd := exec.CommandContext(ctx, "podman", "secret", "ls", "-q")
	lsOutput, err := lsCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("podman secret ls failed: %w", err)
	}

	ids := strings.Fields(string(lsOutput))
	if len(ids) == 0 {
		return nil, nil
	}

	inspectCmd := exec.CommandContext(ctx, "podman", append([]string{"secret", "inspect"}, ids...)...)
	inspectOutput, err := inspectCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("podman secret inspect failed: %w", err)
	}

	var raw []struct {
		Spec PodmanSecretMeta `json:"Spec"`
	}
	if err := json.Unmarshal(inspectOutput, &raw); err != nil {
		return nil, fmt.Errorf("podman secret inspect: parse JSON: %w", err)
	}

	secrets := make([]PodmanSecretMeta, len(raw))
	for i, r := range raw {
		secrets[i] = r.Spec
	}
	return secrets, nil
}

// UpsertSecret creates or replaces a podman secret with the given name, value, and labels.
// It uses 'podman secret create --replace' which is atomic — there is no window where the
// secret is absent. The value is passed via stdin to avoid exposure in process arguments.
// Labels are written as key=value pairs and include syslet/hash for change detection.
func (c *Client) UpsertSecret(ctx context.Context, name string, value model.Plaintext, labels map[string]string) error {
	args := []string{"secret", "create", "--replace"}
	for k, v := range labels {
		args = append(args, "--label", k+"="+v)
	}
	args = append(args, name, "-")

	cmd := exec.CommandContext(ctx, "podman", args...)
	cmd.Stdin = strings.NewReader(string(value))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("podman secret create %s failed: %w (output: %s)", name, err, string(output))
	}
	return nil
}

// DeleteSecret executes 'podman secret rm <name>' to remove a secret from the store.
// Called during apply to remove orphaned secrets (keys no longer present in the spec).
func (c *Client) DeleteSecret(ctx context.Context, name string) error {
	cmd := exec.CommandContext(ctx, "podman", "secret", "rm", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("podman secret rm %s failed: %w (output: %s)", name, err, string(output))
	}
	return nil
}
