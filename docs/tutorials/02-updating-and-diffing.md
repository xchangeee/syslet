# Updating and diffing

This tutorial builds on [First deployment](01-first-deployment.md): you'll change the `webapp` spec, preview the change with `--diff`, apply it, and confirm re-applying an unchanged spec does nothing.

## 1. Preview a change

Edit `webapp.json` to publish an extra port:

```json
{
  "name": "webapp",
  "type": "container",
  "desiredState": "running",
  "unit": {
    "Container": {
      "Image": "docker.io/library/nginx:latest",
      "PublishPort": ["8080:80", "8443:443"]
    }
  }
}
```

Before applying, see what syslet would do:

```sh
scp webapp.json web01:/tmp/webapp.json
ssh web01 sudo syslet --diff /tmp/webapp.json
```

`--diff` runs the full plan (validate, diff against installed state) but stops before executing anything. It reports that `webapp.container` changed and that `webapp.service` will be stopped, recreated, and restarted. Nothing on the server is touched yet.

## 2. Apply it

```sh
ssh web01 sudo cp /tmp/webapp.json /etc/syslet/config.json
ssh web01 sudo syslet
```

## 3. Re-apply without changes

Run the exact same command again:

```sh
ssh web01 sudo syslet
```

syslet reports no changes and does not stop or restart `webapp.service`. Applying an unchanged spec is always a no-op. This idempotency is what makes it safe to re-run syslet on every deploy, or on a schedule, without worrying about unnecessary restarts.

## Pushing over SSH directly

Steps 1–2 above used `scp` plus a remote `syslet` invocation. In practice it's more convenient to pipe the spec straight over SSH with `--stdin`, which also persists the applied spec to `/etc/syslet/config.json` for you:

```sh
# Dry-run
cat webapp.json | ssh web01 sudo syslet --diff --stdin

# Apply
cat webapp.json | ssh web01 sudo syslet --stdin
```

See [Deploy over SSH](../how-to/deploy-over-ssh.md) for the full workflow.
