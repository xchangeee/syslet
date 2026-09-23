# Removing specs

syslet has no delete command. You drop a spec from the input, apply, and syslet works out what on the host is now unaccounted for. This page covers how stale units are found, what syslet does to each kind, and what happens to the podman resources they leave behind.

## Units absent from the input

At plan time syslet lists every unit file in the quadlet directory (`/etc/containers/systemd/`, see [File layout](../reference/file-layout.md)) for all four unit types, and compares that list against the units the current input renders to. Anything on the host the input does not name is stale.

That sweep covers the whole directory, including unit files syslet never wrote. A hand-placed `.container` file counts as stale on the first apply that doesn't name it.

## Fail-safe protections

When absence is the trigger, an input that omits units by mistake reads as a request to tear them down, and the blast radius is a stopped service or a deleted volume rather than a failed apply. syslet is built to run unattended in GitOps pipelines, so it is the one operation that requires you to declare intent in advance, in the spec rather than at the moment of deletion.

That declaration is `removalAllowed`, a boolean on `container`, `volume`, and `network` specs. syslet defaults it to `false`, and setting it to `true` is what permits syslet to delete that unit once it goes missing from the input. Build units have no such field and are always removed.

The flag exists because "absent from the input" is a state your tooling can reach by mistake. A bug in whatever generates your specs, a templating run that renders an empty list, a truncated file, a filter that matched nothing: any of these produces an input that is syntactically fine and simply omits half your units. Without a guard, the next apply would faithfully delete a working deployment, and the failure would look exactly like a successful reconciliation. This mechanism is borrowed from Argo CD's finalizers, where deleting an application only cascades to the resources it created if the opt-in was placed on the application ahead of time.

The `[X-Syslet]` section is where that declaration lives. syslet writes this metadata block into every unit file it generates, carrying `RemovalAllowed` and, for the types that have one, `ReclaimPolicy`.

Without the marker, a stale unit is left exactly as it is: the unit file stays, the service keeps running, and the plan records

```text
skipped: not marked for removal (removalAllowed not set)
```

A skip is not an error. The apply succeeds and the unit is reported again on every subsequent run, until you either mark it removable or put its spec back. This is also what protects unit files syslet never wrote: a hand-placed unit has no `[X-Syslet]` block at all, so it can never satisfy the marker.

!!! warning "The markers come from the installed unit, not from your spec"

    Once a unit is stale its spec is gone, so there is nothing left to read the markers from. What counts is the unit file written by the **last successful apply**. Turning on `removalAllowed` therefore takes two applies: first with `removalAllowed: true` and the spec still present, which rewrites the unit file with the new marker, then again with the spec deleted. Setting the flag and deleting the spec in one change achieves nothing, because the flag never reaches the host.

    This is the declare-it-in-advance rule showing its teeth: a compromised or buggy input cannot grant itself deletion rights in the same pass in which it deletes.

A second guard catches the common way a removal goes wrong: dropping a volume, network, or build that something still uses. Post-render validation requires every volume, network, and build a container references to be present in the same input, so removing `webapp-data` while the `webapp` container still mounts it fails the whole plan before anything is touched:

```text
container "webapp": references undefined volume "webapp-data"
```

Because a diff builds the same plan as an apply, every `removed` and `skipped` line in it is a removal decision made in advance; see [Preview changes with --diff](../how-to/preview-changes-with-diff.md#3-check-removals).

## Behavior per unit type

This section describes what happens once removal goes ahead. Whether it goes ahead at all is covered in [Fail-safe protections](#fail-safe-protections), and what happens to the podman resource is covered in [Reclaiming what the unit leaves behind](#reclaiming-what-the-unit-leaves-behind).

### container

For a stale container unit that is permitted to go, syslet:

1. Checks the unit's runtime state and stops it if it is running. A oneshot container (`Type=oneshot`) is not stopped, since there is nothing running to stop.
2. Deletes the `.container` unit file.
3. Deletes `/etc/containers/config/<name>/`, which holds every `configFiles` file and every `configDirs` group, including all retained versions and their symlinks.
4. Leaves the rest to the single `daemon-reload` at the end of the apply, after which podman's quadlet generator no longer produces the `.service`.

Containers have no `reclaimPolicy` because the container itself is disposable: stopping the unit disposes of it, and anything that needed to survive lives in the volumes it mounted. Those volumes are separate units with their own markers, and a container's removal does not touch them.

Config files also disappear in a smaller case that has nothing to do with unit removal. Delete a `configFiles` entry or a `configDirs` group from a spec that otherwise stays, and the matching host file or directory group is pruned as stale config. That is ordinary reconciliation of a live unit, and `removalAllowed` does not gate it.

### volume

The `.volume` unit file is deleted. What happens to the podman volume, and to the data inside it, depends on `reclaimPolicy`.

### network

The `.network` unit file is deleted, and the podman network follows the same `reclaimPolicy` rule as a volume.

### build

Build units carry no `removalAllowed` marker and are always removed once stale. A build unit is a Containerfile plus its context files, all of it regenerated from the spec, so a mistaken removal costs you one rebuild and nothing else. The built image is the part that could be expensive to lose, and that is governed by `reclaimPolicy` instead.

For a stale build unit, syslet:

1. Deletes the `.build` unit file.
2. Deletes the build context directory `/etc/containers/builds/<name>/`, Containerfile and context files together. The context comes entirely from the spec, so it goes with the unit rather than lingering.
3. Deletes the built image from podman, but only under `reclaimPolicy: "Delete"`.

Image deletion is narrow on purpose. syslet reads the `ImageTag` recorded in the stale unit's own `[Build]` section and deletes that one tag. A build still in the desired set keeps its image even when another build is pruned in the same pass, nothing is guessed from naming, and untagged images are never pruned.

Rebuilding is not removal. When a build unit changes, the old image survives: syslet stops the build service so it re-runs, and `podman build` overwrites the tag in place.

### secrets

Secrets are not quadlet units and never appear in the quadlet directory, so they are reconciled by ownership label instead. syslet labels every secret it creates with `syslet/hash`. On each apply, any podman secret carrying that label but missing from the desired set is deleted, including keys left behind by a secret spec dropped from the input entirely.

There is no `removalAllowed` here. The label is the guard: a podman secret without it is never touched, so secrets created by hand or by another tool survive reconciliation untouched.

## Changing a volume is destructive too

Podman volumes cannot be reconfigured in place, so a meaningful change to a volume unit (anything outside the `[X-Syslet]` metadata and `[Unit] Description`) means tearing the volume down and building it again. syslet does that only when the installed unit carries both `removalAllowed: true` and `reclaimPolicy: "Delete"`. It then stops the volume service, deletes the podman volume, and writes the new unit.

Without both markers, the change is a planning error rather than a quiet skip:

```text
volume has meaningful changes but recreation is not permitted:
set removalAllowed=true and reclaimPolicy=delete to allow
```

The whole apply is refused. Writing a unit file that disagrees with the volume actually on the host would leave the host misrepresenting itself, so syslet makes you grant both permissions or revert the change.

Networks are immutable after creation as well, but syslet recreates a changed network with no markers required. A network holds no data, so recreating it costs a short interruption and nothing else. Containers attached to it are restarted in the same pass to pick up the new network.

## Reclaiming what the unit leaves behind

A quadlet container service removes its container when it stops, so stopping the unit disposes of the podman object too. The volume, network, and build services only create their resource if it is missing; stopping them leaves the resource in place, and so does deleting their unit file.

Without something managing both ends, that is how hosts accumulate junk. `reclaimPolicy` is how syslet manages that second end:

- `Retain` (syslet's default) leaves the podman object in place.
- `Delete` deletes it along with the unit.

It applies to `volume`, `network`, and `build`, the three types with a podman resource that outlives the unit file. The name and the semantics are borrowed from Kubernetes persistent volumes, where a `persistentVolumeReclaimPolicy` of `Retain` or `Delete` decides the same question about the storage behind a claim.

 For volumes and networks this combines with `removalAllowed`:

| `removalAllowed` | `reclaimPolicy` | Result for a stale volume or network |
| --- | --- | --- |
| `false` | either | Nothing happens; the unit is reported as skipped. |
| `true` | `Retain` | Unit file deleted; podman resource left behind, untracked. |
| `true` | `Delete` | Unit file deleted; podman resource deleted with it. |

Builds skip the first column, since they are always removed when stale.

Every syslet-managed volume, network, and build is `Retain` unless you say otherwise, on the fail-safe assumption that the data matters: the declaration goes, the bytes stay, and disposing of the resource becomes a manual podman operation. Setting `Delete` is how you tell syslet this one is not worth keeping.

!!! warning "The CUE schema flips these defaults"

    `#SysdefDefaults` sets `removalAllowed: true` on containers, networks, and volumes, and `reclaimPolicy: "Delete"` on volumes and builds, so experimenting in a CUE repository cleans up after itself. Dropping an unlocked CUE volume therefore deletes its data. List every volume worth keeping in `#SysdefLock`, which sets `removalAllowed: false` and `reclaimPolicy: "Retain"`. See [Manage volumes](../how-to/manage-volumes.md#know-your-defaults).

Put a deleted spec back and syslet writes the unit file again, but that only restores the declaration. Under `Retain` the volume is still there and the unit reattaches to it; under `Delete` the recreated volume comes back empty and the deleted image has to be rebuilt.
