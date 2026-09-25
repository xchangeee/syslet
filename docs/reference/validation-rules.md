# Validation rules

syslet checks the input in stages, in the order below.
Each stage stops at its first error, and a failed stage stops the plan before anything on the host changes.
`syslet --diff` runs every stage, so its errors are the same as an apply's.

See [Safety mechanisms](../explanation/safety-mechanisms.md#multi-stage-validation) for why the stages exist, and [Plan output](plan-output.md#validation-errors) for how errors are printed.

## Load

Checks while reading the specs.
A failure exits syslet with the error before a plan is built.

- Every spec sets a supported `apiVersion`.
- `type` is `container`, `volume`, `network`, `build` or `secret`.
- The input isn't empty. An empty array `[]` is valid and declares no specs, so every unit whose removal is allowed is removed.
- Every spec except `secret` has a `unit` object; `{}` is allowed.
- Unit option values are strings, arrays of strings, or maps of strings for the options that take a map; see [Unit options](spec-schema/unit-options.md#value-forms).
- `desiredState` is `running`, `stopped` or `oneshot`.
- `reclaimPolicy` is `Delete` or `Retain`.
- File `mode`s are octal strings.
- A secret's `ciphertext` is SOPS YAML whose key names can be read.

## Pre-render

Checks on the specs, printed as `pre-render validation: ...`.

| Spec | Check |
| --- | --- |
| all | No two specs of the same type share a name. |
| all | `name` isn't empty. |
| all | `unit` has no `X-Syslet` section; syslet reserves it for its markers. |
| container | `configFiles[].mountPath` is absolute, not empty and has no `..`. |
| container | `configDirs[].mountPath` is absolute, not empty and has no `..`. |
| container | No `configFiles` or `configDirs` mount path equals or lies under another. |
| container | `configDirs[].files[].name` is a plain file name without `/`. |
| container | A container with `configDirs` sets `[Service] ExecReload=`. |
| container | `[Service] Type=oneshot` isn't combined with `desiredState: "running"`. |
| container | Every `Secret=` names a key of a `secret` spec, as `<secret>-<key>`. |
| build | `containerfile` is set. |
| build | `contextFiles[].filename` is relative, not empty and has no `..`. |
| build | No two context files share a name, and none is named `Containerfile`. |
| secret | Key names match `[a-z0-9-]+`. |

Key names are readable without decrypting, so the secret checks run before any decryption.

## Post-render

Checks on the rendered unit files, printed as `post-render validation: ...`.

- Every `Volume=<name>.volume` in a container names a `volume` spec.
- Every `Network=<name>.network` in a container names a `network` spec.
- Every `Image=<name>.build` in a container names a `build` spec.
- No two `Volume=` options of a container share a destination path.
- A build's `ImageTag` is set and starts with `localhost/<build name>:`.

## Host

Checks against the state on the host, while the plan is built.

- The host has an age key when the input contains `secret` specs.
- Every secret decrypts with the host's key.
- Every secret value is a string, not nested YAML.
- A volume whose options changed may be recreated, which takes `removalAllowed: true` and `reclaimPolicy: Delete` on the installed unit.

## Staging

Runs only when the plan writes unit files.
syslet writes the rendered units to a temporary `syslet-stage-*` directory, runs podman's quadlet generator on them, and runs `systemd-analyze verify` on the generated units.
The directory is kept after the run, so you can inspect what was checked.

- The quadlet generator accepts every unit.
- `systemd-analyze verify` passes without output. Warnings fail too.
