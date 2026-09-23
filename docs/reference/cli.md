# CLI

```
syslet [--diff] [config.json|config-dir/]
cat dir/*.json | syslet [--diff] --stdin
syslet --version
```

## Flags

| Flag | Description |
|---|---|
| `--diff` | Show what would change without applying (dry-run). Runs the full plan (read, validate, diff) but performs no writes and no systemd/podman operations. |
| `--stdin` | Read the JSON spec stream from standard input, instead of a path argument. Mutually exclusive with a positional path; passing both is an error. |
| `--version` | Print the version and exit. Release builds show the version, commit and build date; `go install` builds show the module version. Reads no config and needs no root. |

## Positional argument

An optional path to a spec source:

- A directory of `.json` spec files (see [Deploy over SSH](../how-to/deploy-over-ssh.md#deploy-from-a-directory-on-the-host)).
- A single file containing a JSON stream of specs (an array, newline-delimited JSON, or concatenated objects).

If omitted and `--stdin` is not set, defaults to `/etc/syslet/config.json`.

## Input persistence

With `--stdin` and no `--diff`, the received stream is persisted to `/etc/syslet/config.json` after being read, so it becomes the host's re-applyable on-disk record (`ssh host syslet` later re-applies it). A `--stdin --diff` dry-run and a plain path/directory argument persist nothing.

## Exit behavior

- Exit code `0`: applied (or diffed) successfully.
- Exit code `1`: any error, whether spec loading/parsing, validation failure, or an apply-time error. Errors are printed to stderr as `error: <message>`.
- A `--diff` that succeeds prints the plan to stdout; a normal apply prints a results summary to stdout after execution.
- A `--diff` whose plan has validation errors prints them to stdout and exits `1`, like an apply would refuse the same plan.

## Environment

| Variable | Description |
|---|---|
| `SYSLET_SHOW_SECRETS` | Set to `1` or `true` to print decrypted secret values in `--diff` output. Unset, they print as `(secret)`. Any value [`strconv.ParseBool`](https://pkg.go.dev/strconv#ParseBool) rejects is an error. Meant for local use; don't set it in CI. |

## Daemon configuration

syslet reads optional daemon-level settings from `/etc/syslet/syslet.json` on every invocation. See [Daemon config](daemon-config.md).
