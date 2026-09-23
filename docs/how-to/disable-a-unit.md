# Disable a unit

In a CUE repository, `enabled: false` leaves an entry out of the spec syslet receives, while its definition stays in the file.
For syslet, that's the same as deleting the entry, so disabling is a removal and follows the removal rules.

## Disable an entry

```cue
sysdef: containers: site: enabled: false
```

Preview with `cue cmd plan` before applying.
What happens depends on the type and on whether the entry is locked with `#SysdefLock`:

| Type | Unlocked, with `#SysdefDefaults` | Locked |
| --- | --- | --- |
| container | Stopped; unit and config files deleted. | Skipped, keeps running. |
| volume | Unit deleted, **podman volume and data deleted**. | Skipped, data intact. |
| network | Unit deleted, podman network left behind. | Skipped. |
| build | Unit and build context deleted, built image deleted. | Builds can't be locked. |
| secret | Its podman secrets deleted. | Secrets can't be locked. |

A container that stays enabled can't reference a disabled volume, network, build or secret; the plan fails validation.
Disable the container together with what it uses.

To stop a container but keep everything else, set `desiredState: "stopped"` instead (see [Manage containers](manage-containers.md#stop-a-container-without-removing-it)).

## Enable it again

Remove the `enabled: false` line and apply.
syslet writes the units again and starts containers.
A volume under `reclaimPolicy: "Retain"` reattaches with its data; one under `"Delete"` comes back empty.
