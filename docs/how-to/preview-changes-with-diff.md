# Preview changes with --diff

Pass `--diff` to run syslet's full plan (read, validate, diff against installed state) without executing anything:

```sh
ssh web01 sudo syslet --diff /path/to/specs
cat hosts/web01/*.json | ssh web01 sudo syslet --diff --stdin
ssh web01 sudo syslet --diff   # dry-run against the persisted /etc/syslet/config.json
```

The output lists, per unit, whether it will be created, changed, removed, or left alone, and which container services will be stopped/started/reloaded as a result. Nothing is written to disk and no systemd/podman commands are executed. `--diff` only builds and displays the plan.

Use `--diff` before every apply in a CI pipeline or webhookd script if you want a human to review changes before they land, or just to sanity-check a spec edit locally before pushing it.
