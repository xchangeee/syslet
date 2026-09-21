# Deploy over SSH

Pipe spec files straight into `syslet --stdin` over SSH. No extra tooling, one round-trip:

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

The deployment host must authenticate to the remote host via SSH public-key auth. Password auth is not supported (`syslet` itself has no SSH logic; this is a property of piping through `ssh`).
