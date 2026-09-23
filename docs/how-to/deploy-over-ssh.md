# Deploy over SSH

syslet has no SSH logic of its own: you send it specs through `ssh`, either as a stream on stdin or as files already on the host.
Both need SSH public-key auth to the host.

## Pipe specs to the host

Pipe spec files straight into `syslet --stdin`, with no extra tooling and one round-trip:

```sh
# Deploy a directory of specs
cat hosts/web01/*.json | ssh web01 sudo syslet --stdin

# Dry-run: show what would change without applying
cat hosts/web01/*.json | ssh web01 sudo syslet --diff --stdin
```

`--stdin` accepts a JSON array, newline-delimited JSON, or concatenated JSON objects: exactly what `cat dir/*.json` produces.

On apply (not `--diff`), the received stream is persisted to `/etc/syslet/config.json` as the host's on-disk record, so it can be re-applied later without re-sending it:

```sh
ssh web01 sudo syslet            # re-apply /etc/syslet/config.json
ssh web01 sudo syslet --diff     # dry-run against the persisted config
```

## Deploy from a directory on the host

syslet also reads a directory of `.json` spec files directly, which suits specs copied or checked out onto the host:

```sh
scp -r hosts/web01/ web01:/etc/syslet/hosts/web01/
ssh web01 sudo syslet /etc/syslet/hosts/web01/
```

Each `.json` file in the directory is loaded as one spec; other files and subdirectories are ignored.

Applying from a directory or file path doesn't write `/etc/syslet/config.json`.
The directory you pass is itself the on-disk record.
