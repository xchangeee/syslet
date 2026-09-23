# Protect a container

While you set things up, syslet removes anything you drop from the repository, which makes it easy to experiment.
Once a setup works, an accidental deletion or a bad merge shouldn't take it down.

In this tutorial, you'll see what a removal looks like in the plan, then lock the `site` container from [Deploy with CUE](04-deploy-with-cue.md) so syslet leaves it running.

## Prerequisites

- The `infra` repository with the `site` container from [Deploy with CUE](04-deploy-with-cue.md).

## 1. Preview a removal

`enabled: false` leaves an entry out of the spec without deleting it from `web01.cue`, so you can see what a removal would do.
Add this line to `web01.cue`:

```cue
sysdef: containers: site: enabled: false
```

Preview it, but don't apply:

```sh
cue cmd plan
```

syslet would stop the container and delete its unit file and config files:

```text
Unit files to delete:
  - site.container

Config directories to delete:
  - site/

Services to stop:
  - site.container

Systemd daemon-reload: required

Summary:
UNIT                                     STATUS     CHANGES
site.container                           removed    removed
```

syslet removes it because `#SysdefDefaults` set `removalAllowed: true`, which syslet recorded in the unit file as `[X-Syslet] RemovalAllowed=true`.

Remove the `enabled: false` line again.

## 2. Lock the container

Add the tools import below the `package` line of `web01.cue`, and the lock at the end:

```cue title="web01.cue" hl_lines="3 23"
--8<-- "05-protect-a-container/web01.cue"
```

`#SysdefLock` sets `removalAllowed: false` on every container, network and volume listed.
For volumes, it also sets `reclaimPolicy: "Retain"`, so their data survives even when the unit is removed.

Preview it:

```sh
cue cmd plan
```

The plan only changes metadata in the unit file, so nothing is restarted:

```text
Unit file changes:

--- site.container
changed:
  [X-Syslet] RemovalAllowed
    old: true
    new: false

Systemd daemon-reload: required

Summary:
UNIT                                     STATUS     CHANGES
site.container                           updated    unit updated (desired: running)
```

```sh
cue cmd apply
git add -A && git commit -m "lock site"
```

## 3. Check the lock

Add `sysdef: containers: site: enabled: false` to `web01.cue` again and run:

```sh
cue cmd plan
```

This time syslet skips the container and leaves it running:

```text
Summary:
UNIT                                     STATUS     CHANGES
site.container                           skipped    protected (removalAllowed: false)

No changes detected. All units are up to date.
```

The lock is stored on the host, in the unit file.
That's why it still protects the container once the spec is gone.

Remove the `enabled: false` line again.

To remove a locked container on purpose, first drop it from the `#SysdefLock` list and apply, then delete it from `web01.cue` and apply again.
See [Removing specs](../explanation/removing-specs.md) for details.

The `site` container is now safe from accidental removal.
Every change goes through `cue cmd plan`, `cue cmd apply` and a commit.

Continue with [Set up SOPS encryption](06-setup-sops-encryption.md).
