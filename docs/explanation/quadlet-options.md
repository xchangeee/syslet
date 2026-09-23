# Quadlet options

A spec's `unit` field maps 1:1 onto [Podman quadlet](https://docs.podman.io/en/latest/markdown/podman-systemd.unit.5.html) unit file sections and options. syslet passes these through largely unvalidated (beyond the few checks in [Safety mechanisms](safety-mechanisms.md)) and lets podman's own quadlet generator reject anything it doesn't understand at staging time.

For the full set of supported sections and options (`[Container]`, `[Volume]`, `[Network]`, `[Build]`, plus the standard systemd `[Unit]`, `[Service]`, `[Install]` sections), see the upstream man page vendored in this repository at [`hack/reference/podman-systemd.unit.5.md`](https://github.com/xchangeee/syslet/src/branch/main/hack/reference/podman-systemd.unit.5.md), or `man podman-systemd.unit` on a host with podman installed.

A few options syslet sets or requires itself, layered on top of what you write in `unit`:

- `ContainerName` / `VolumeName` / `NetworkName` — auto-set from the spec's `name` if not given explicitly.
- `[Install] WantedBy=` and `[Service] Restart=Always` — added automatically to a container when `desiredState: "running"`.
- `[Service] ExecReload=` — not set automatically; required if the container declares any `configDirs` (see [Reload config without restart](../how-to/reload-config-without-restart.md)).
- `[Build] ImageTag=` — must use the `localhost/` prefix (enforced by syslet, not a general quadlet requirement).
