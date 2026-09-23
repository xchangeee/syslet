# Plan output

`syslet --diff` prints the plan to stdout.
An apply logs each operation to stderr and prints only the summary rows to stdout.

## Validation errors

A plan with any error prints only its errors, and no other section:

```text
Validation errors:
  error: <message>
  error [<unit>]: <message>
```

| Line | Source |
| --- | --- |
| `error: pre-render validation: ...` | Checks on the specs. |
| `error: post-render validation: ...` | Checks on the rendered unit files. |
| `error: quadlet generator failed ...`, `error: unit verification failed ...` | Staging checks by podman's generator and `systemd-analyze verify`. |
| `error: secret "<name>": decryption failed: ...`, `error: secrets present in spec but no decryptor configured ...` | The host can't decrypt a secret spec. |
| `error [<unit>]: ...` | A check of one unit against the host, such as a volume change the installed markers don't allow. |

See [Safety mechanisms](../explanation/safety-mechanisms.md#multi-stage-validation) for the stages.

## Sections

A section is printed only when it has entries, in this order:

| Section | Lists |
| --- | --- |
| `Unit file changes:` | Per unit, `--- <unit>` followed by `added:`, `changed:` (with `old:`/`new:`) and `removed:` options. |
| `Unit files to delete:` | Unit files of stale units. |
| `Config file changes:` | Unified diff per `configFiles` entry, as `<container>:<mountPath>`, and mode changes. |
| `Config files to delete:` | Stale `configFiles` entries, as `<container>/<file>`. |
| `Config directories to delete:` | A container's whole config directory, as `<container>/`. |
| `ConfigDir changes:` | Per `configDirs` entry, `<container>:<mountPath> → version <n>`, followed by a diff per file. |
| `ConfigDir groups to delete:` | Stale `configDirs` entries. |
| `Build context file changes:` | Unified diff per `containerfile` and `contextFiles` entry. |
| `Build context files to delete:` | Stale context files, as `<build>/<file>`. |
| `Build context directories to delete:` | A build's whole context directory, as `<build>/`. |
| `Secret changes:` | Per secret spec, `+ <key>=<value>` for new or changed keys, in plain text, and `- <key>=(secret)` for deleted ones. |
| `Services to stop:` | Units stopped before files are written. |
| `Systemd daemon-reload: required` | Printed when any unit file is written or deleted. |
| `Containers to start:` | Containers started at the end of the apply. |
| `Podman volumes to delete:` | Volumes deleted under `reclaimPolicy: "Delete"`, or recreated. |
| `Podman networks to delete:` | Networks deleted under `reclaimPolicy: "Delete"`, or recreated. |
| `Podman images to delete:` | Built images deleted under `reclaimPolicy: "Delete"`. |

Reloads are not listed in a section; they appear as `reloaded` in the summary.

## Summary

```text
Summary:
UNIT                                     STATUS     CHANGES
site.container                           updated    unit updated, restarted (desired: running)
```

One row per unit in the input or on the host.
When no section was printed, the summary is followed by `No changes detected. All units are up to date.`

### STATUS

| Value | Meaning |
| --- | --- |
| `created` | The unit file is new. |
| `updated` | The unit, its files or its runtime state change. |
| `unchanged` | Nothing changes. |
| `removed` | The unit is stale and will be deleted. |
| `skipped` | The unit is stale but its installed unit has `RemovalAllowed=false`; it is left as it is. |
| `error` | The unit failed, during the plan or the apply. |

### CHANGES for containers

A comma-separated list, followed by `(desired: <state>)`:

| Entry | Cause |
| --- | --- |
| `created` | New unit file. |
| `unit updated` | The unit file changed. |
| `config updated` | A `configFiles` entry changed. |
| `configDir updated` | A `configDirs` entry's files changed. |
| `volume recreated`, `network recreated`, `build recreated` | A referenced volume, network or build is recreated in the same apply. |
| `secret updated` | A referenced secret changed. |
| `restarted`, `started`, `stopped`, `reloaded` | The service action. |
| `up to date` | Nothing changes. |

For other types, CHANGES is `created`, `unit updated`, `up to date` or `removed`; builds also report `build context updated` and `unit and build context updated`.
A skipped unit reads `not marked for removal (removalAllowed not set)`.
