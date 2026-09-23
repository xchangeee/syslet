# Deploy with CUE

In this tutorial, you'll deploy an nginx container with its own config file from the repository you set up in [Set up CUE repository](03-setup-cue-repository.md).
You'll preview the change, apply it, and let the schema catch a mistake before it reaches the host.

## Prerequisites

- The `infra` repository and `web01.example.com` from [Set up CUE repository](03-setup-cue-repository.md).

## 1. Add the container

Everything deployed to `web01` goes into one file, `web01.cue`.
Create it with a container and a config file for it:

```cue title="web01.cue"
--8<-- "04-deploy-with-cue/web01.cue"
```

The key `site` becomes the spec name, and `unit` maps onto the quadlet sections, as in the [container spec](../reference/spec-schema/container.md).
`configFiles` are written to the host and bind-mounted into the container (see [Mount config files and dirs](../how-to/mount-config-files-and-dirs.md)).
Without a `Network` option, the container runs on podman's default bridge network.

## 2. Preview it

```sh
cue cmd plan
```

The plan lists the unit file syslet will write, including the options it adds on its own, such as `ContainerName`, `Restart=always` and the bind mount for the config file, followed by the config file itself:

```text
Unit file changes:

--- site.container
added:
  [Container] ContainerName=site
  [Container] Image=docker.io/library/nginx:1.27
  [Container] PublishPort=8080:80
  [Container] Volume=/etc/containers/config/site/default.conf-22f656b2:/etc/nginx/conf.d/default.conf:ro,Z
  [Install] WantedBy=multi-user.target default.target
  [Service] Restart=always
  [Unit] Description=site container
  [X-Syslet] RemovalAllowed=true

Config file changes:
--- site:/etc/nginx/conf.d/default.conf (current)
+++ site:/etc/nginx/conf.d/default.conf (new)
@@ -0,0 +1,5 @@
+server {
+    listen 80;
+    root /usr/share/nginx/html;
+    location /healthz { return 200 "ok"; }
+}
\ No newline at end of file

Systemd daemon-reload: required

Containers to start:
  - site.container

Summary:
UNIT                                     STATUS     CHANGES
site.container                           created    created, config updated, started (desired: running)
```

## 3. Apply it

```sh
cue cmd apply
```

`apply` prints the same diff, then asks `Continue? (yes/no)`.
Answer `yes`, and syslet logs each action as it runs:

```text
Continue? (yes/no) yes
time=2026-09-23T08:34:52.267+02:00 level=INFO msg="writing config" container=site mountPath=/etc/nginx/conf.d/default.conf
time=2026-09-23T08:34:52.267+02:00 level=INFO msg="writing unit file" unit=site.container
time=2026-09-23T08:34:52.267+02:00 level=INFO msg=daemon-reload
time=2026-09-23T08:34:52.267+02:00 level=INFO msg=starting unit=site.container
```

Commit the working state:

```sh
git add -A && git commit -m "add site"
```

Verify it on the server:

```sh
ssh web01.example.com systemctl status site.service
curl http://web01.example.com:8080/healthz
```

Change the config file and run `cue cmd plan` again: the plan shows the diff of the file and a restart of `site.container`, since config files are static bind mounts.

## 4. Let the schema catch mistakes

Set `desiredState: "runing"` on the container, then run:

```sh
cue vet -c ./...
```

CUE reports the conflict against the syslet schema before anything reaches the server, and you can run the same command in CI on every commit.
The schema doesn't check option names under `unit`, because the valid options depend on the host's podman version.
A typo like `Imag:` therefore passes `cue vet`, and the quadlet validation on the host rejects it during `cue cmd plan` (see [Safety mechanisms](../explanation/safety-mechanisms.md)).

Revert the mistake before moving on.

The container is now running and described in git.
Continue with [Protect a container](05-protect-a-container.md).
