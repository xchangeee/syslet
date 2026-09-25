# Safety mechanisms

Small mistakes in config slip in easily, even past a review.
syslet has your back: validation rejects a bad spec before anything on the host changes, and removal, the one operation with no undo, only happens where the spec allows it.

## Multi-stage validation

Specs are checked at several points before anything is executed, each catching what the earlier ones can't see:

1. **Pre-render**: the specs on their own, such as duplicate names, invalid mount paths, or `configDirs` without `ExecReload=`.
   Since SOPS leaves key names readable, secret references can already be checked here, before anything is decrypted.
2. **Post-render**: the rendered unit files, such as a container referencing a volume, network or build that has no spec.
3. **Host**: the specs against what's installed, such as a secret the host can't decrypt, or a volume change that would need a recreation its markers don't allow.
4. **Staging**: the rendered units run through podman's quadlet generator and `systemd-analyze verify` on the host.
   syslet passes unit options through without knowing which ones the installed podman and systemd versions support, so this is where an unknown or misspelled option is caught.
   Warnings fail too, since an ignored setting means the unit wouldn't run as specified.

A spec that fails any stage is rejected before syslet touches the live system.
`syslet --diff` runs all of them, so a `--diff` that succeeds means the same apply would also pass validation.
See [Validation rules](../reference/validation-rules.md) for every check.

## Removal protection

`removalAllowed` decides whether a stale unit is removed at all, and `reclaimPolicy` decides whether removing it also deletes podman's volume, network or image.
[Removing specs](removing-specs.md) covers both per unit type.
