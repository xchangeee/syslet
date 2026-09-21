# CLI

```
syslet [--diff] [config.json|config-dir/]
cat dir/*.json | syslet [--diff] --stdin
```

## Flags

| Flag | Description |
|---|---|
| `--diff` | Show what would change without applying (dry-run). Runs the full plan (read, validate, diff) but performs no writes and no systemd/podman operations. |
| `--stdin` | Read the JSON spec stream from standard input, instead of a path argument. Mutually exclusive with a positional path; passing both is an error. |

## Positional argument

An optional path to a spec source:

- A directory of `.json` spec files (see [Deploy from a directory](../how-to/deploy-from-a-directory.md)).
- A single file containing a JSON stream of specs (an array, newline-delimited JSON, or concatenated objects).

If omitted and `--stdin` is not set, defaults to `/etc/syslet/config.json`.

## Input persistence

With `--stdin` and no `--diff`, the received stream is persisted to `/etc/syslet/config.json` after being read, so it becomes the host's re-applyable on-disk record (`ssh host syslet` later re-applies it). A `--stdin --diff` dry-run and a plain path/directory argument persist nothing.

## Exit behavior

- Exit code `0`: applied (or diffed) successfully.
- Exit code `1`: any error, whether spec loading/parsing, validation failure, or an apply-time error. Errors are printed to stderr as `error: <message>`.
- A `--diff` that succeeds prints the plan to stdout; a normal apply prints a results summary to stdout after execution.

## Daemon configuration

syslet reads optional daemon-level settings from `/etc/syslet/syslet.json` on every invocation. See [Daemon config](daemon-config.md).
