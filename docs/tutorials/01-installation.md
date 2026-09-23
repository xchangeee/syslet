# Installation

In this tutorial, you'll install syslet on a server so the following tutorials can deploy to it.

## Prerequisites

- A Linux server with [Podman](https://podman.io/) 6 or newer installed (e.g. `dnf install podman`) and systemd.
  syslet hasn't been tested with earlier Podman versions.
- Root access on that server (syslet needs it for systemd D-Bus operations).
- SSH access to the server.

The tutorials call this server `web01`.

## 1. Install the binary

Releases are published on [GitHub](https://github.com/xchangeee/syslet/releases) as tarballs for `linux/amd64`, `linux/arm64` and `linux/386`.
On the server, download the one matching its architecture and put the binary on `PATH`:

```sh
curl -sSfL https://github.com/xchangeee/syslet/releases/latest/download/syslet_Linux_x86_64.tar.gz \
  | sudo tar -xz -C /usr/local/bin syslet
```

Replace `x86_64` with `arm64` or `i386` as needed.

## 2. Check the installation

```sh
ssh web01 syslet --version
```

This prints the installed release, e.g. `syslet 1.2.3 (commit 8b8dc32…, built 2026-09-23T10:00:00Z)`.

syslet is installed.
Continue with [Deploy container](02-deploy-a-container.md).
