// Package podman wraps podman CLI operations for managing volumes and networks.
// It provides the low-level building blocks for interacting with podman resources
// that are referenced by quadlet units but need to be managed separately from systemd.
package podman

import (
	"context"
	"fmt"
	"os/exec"
)

// Interface defines the operations for managing podman resources.
// This interface allows for mocking in tests.
type Interface interface {
	DeleteVolume(ctx context.Context, name string) error
	DeleteNetwork(ctx context.Context, name string) error
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
