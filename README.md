# syslet

syslet is a GitOps-friendly deployment tool for Podman containers using systemd. Instead of manually crafting systemd unit files, you define your infrastructure as JSON specs and let syslet handle the translation to Podman Quadlet units.

**Key characteristics:**

- **JSON-only input** — syslet exclusively consumes JSON specifications
- **Declarative** — describe what you want, not how to get there
- **Idempotent** — safe to run repeatedly; only changes what's necessary
- **SSH-based deployment** — no agents or daemons; deploy over standard SSH
- **GitOps-ready** — designed to be automated with git webhooks (e.g., [webhookd](https://github.com/ncarlier/webhookd))
- **Systemd integration** — leverages Podman Quadlet for robust container management
- **Pruning by default** — removes specs not in the current deployment automatically

## How it works

syslet reads JSON specs from either a zip file or directory, validates them, and applies the desired state:

1. **Read** all JSON specs from the zip file (in memory, no extraction) or directory
2. **Validate** consistency:
   - No duplicate unit names
   - All volumes and networks referenced by containers exist as specs
   - No duplicate config paths within a container
3. **Diff** against the installed state on disk
4. **Execute** in strict order:
   1. Stop containers that changed or are being removed
   2. Write config files to `/etc/containers/config/<name>/`
   3. Write quadlet unit files to `/etc/containers/systemd/`
   4. Remove stale unit files and config dirs (pruning)
   5. Single `systemctl daemon-reload`
   6. Start containers with `desiredState: "running"`
5. **Exit** with status summary

Files not present in the current input (zip or directory) are pruned from the host. This means removing a spec and re-running syslet will stop the container and clean up its files.

## Supported unit types

| Type | Startable | Notes |
|------|-----------|-------|
| container | Yes | Auto-generates `[Install]` section and `ContainerName`, supports config files |
| volume | No | Auto-generates `VolumeName` |
| network | No | Auto-generates `NetworkName` |

## Getting started

### 1. Build

```sh
make build
```

Requires Go 1.25+. This produces two binaries:

- `build/syslet` — server-side binary that applies specs
- `build/syslet-push` — client-side deployment tool

### 2. Install on the server

Copy the `syslet` binary to the target server and ensure it's in PATH. syslet needs root access for systemd D-Bus operations.

### 3. Create your JSON specs

Organize specs by host in your local repository:

```
my-infra/
└── hosts/
    ├── web01/
    │   ├── webapp.json
    │   ├── webapp-data.json
    │   └── webapp-net.json
    └── web02/
        └── ...
```

A container spec:

```json
{
  "name": "webapp",
  "type": "container",
  "desiredState": "running",
  "unit": {
    "Container": {
      "Image": "docker.io/library/nginx:latest",
      "PublishPort": ["8080:80"],
      "Volume": ["webapp-data.volume:/data"],
      "Network": ["webapp-net.network"]
    }
  },
  "configs": [
    {
      "content": "server { listen 80; root /usr/share/nginx/html; }",
      "targetVolumePath": "/etc/nginx/nginx.conf"
    },
    {
      "content": "APP_ENV=production",
      "targetVolumePath": "/run/env"
    }
  ]
}
```

Key points:

- `unit` maps 1:1 to systemd unit file sections and their options
- `desiredState` controls whether syslet starts (`running`) or stops (`stopped`) the container
- `configs` define files mounted into the container -- syslet writes them to `/etc/containers/config/<name>/` and injects the corresponding `Volume=` entries automatically
- The `[Install]` section is auto-generated only when `desiredState: "running"` to enable auto-start on boot
- `ContainerName`, `VolumeName`, and `NetworkName` are automatically set to match the spec `name` if not explicitly specified

A volume spec:

```json
{
  "name": "webapp-data",
  "type": "volume",
  "unit": {
    "Volume": {
      "Label": "app=webapp"
    }
  }
}
```

A network spec:

```json
{
  "name": "webapp-net",
  "type": "network",
  "unit": {
    "Network": {
      "Subnet": "10.89.0.0/24",
      "Gateway": "10.89.0.1"
    }
  }
}
```

### 4. Deploy

Use the `syslet-push` command:

```sh
# Deploy from a directory
syslet-push --directory hosts/web01/ web01

# Or pipe JSON specs from stdin (JSON array or newline-delimited JSON)
cat specs.json | syslet-push --stdin web01
```

`syslet-push` zips the spec files, copies them to the remote host, and runs syslet via SSH. Hosts are managed through your SSH config.

Alternatively, deploy manually:

```sh
# Option 1: Using a zip file
zip -j /tmp/config.zip hosts/web01/*.json
scp /tmp/config.zip web01:/etc/syslet/config.zip
ssh web01 sudo syslet /etc/syslet/config.zip

# Option 2: Using a directory (useful for git repositories)
scp -r hosts/web01/ web01:/etc/syslet/hosts/web01/
ssh web01 sudo syslet /etc/syslet/hosts/web01/
```

Output looks like:

```
webapp-net.network                       changed    created
webapp-data.volume                       changed    created
webapp.container                         changed    created, started
```

#### GitOps automation

syslet is designed for GitOps workflows. Store your specs in version control and automate deployments on git push using [webhookd](https://github.com/ncarlier/webhookd) or similar webhook receivers.

Example webhookd workflow:

1. **Commit and push** specs to your git repository
2. **Git webhook** triggers webhookd on your server
3. **webhookd script** pulls the latest specs and runs `syslet`
4. **Containers update** automatically to match the desired state

Example webhookd script (`/etc/webhookd/scripts/deploy-syslet.sh`):

```bash
#!/bin/bash
cd /etc/syslet/repo
git pull origin main

# Option 1: Use directory directly (simpler, no zipping needed)
syslet hosts/$(hostname)/

# Option 2: Use zip file (if you prefer)
# zip -j /etc/syslet/config.zip hosts/$(hostname)/*.json
# syslet /etc/syslet/config.zip
```

This enables continuous deployment: push to git, containers update automatically. Combined with CUE for validation, you get type-safe infrastructure deployments with full audit history.

#### Using CUE for better ergonomics

While syslet only accepts JSON, writing raw JSON by hand can be verbose and error-prone. [CUE](https://cuelang.org/) provides a better authoring experience with:

- **Type safety and validation** — catch errors before deployment
- **Schema definitions** — define reusable templates for common patterns
- **Reduced boilerplate** — defaults, computed values, and composition
- **Comments and documentation** — unlike JSON

Example CUE workflow:

```cue
// specs.cue
package syslet

#Container: {
    name: string
    type: "container"
    desiredState: "running" | "stopped"
    unit: Container: {
        Image: string
        PublishPort?: [...string]
        Volume?: [...string]
        Network?: [...string]
    }
    configs?: [...{
        content: string
        targetVolumePath: string
    }]
}

webapp: #Container & {
    name: "webapp"
    unit: Container: {
        Image: "docker.io/library/nginx:latest"
        PublishPort: ["8080:80"]
        Volume: ["webapp-data.volume:/data"]
        Network: ["webapp-net.network"]
    }
}
```

Generate JSON and deploy:

```sh
cue export specs.cue | syslet-push --stdin web01
```

This gives you validation, defaults, and better maintainability while still producing the JSON that syslet expects.

## Reference

### JSON spec

#### Container

```json
{
  "name": "webapp",
  "type": "container",
  "desiredState": "running",
  "unit": {
    "Container": {
      "Image": "docker.io/library/nginx:latest",
      "PublishPort": ["8080:80"],
      "Volume": ["webapp-data.volume:/data"],
      "Network": ["webapp-net.network"]
    }
  },
  "configs": [
    {
      "content": "file contents here",
      "targetVolumePath": "/path/in/container"
    }
  ]
}
```

**Note:** `ContainerName` is automatically set to the spec `name` ("webapp") if not specified. You can override it by explicitly setting `"ContainerName": "custom-name"` in the `Container` section.

#### Volume

```json
{
  "name": "webapp-data",
  "type": "volume",
  "unit": {
    "Volume": {
      "Label": "app=webapp"
    }
  }
}
```

**Note:** `VolumeName` is automatically set to the spec `name` ("webapp-data") if not specified. You can override it by explicitly setting `"VolumeName": "custom-name"` in the `Volume` section.

#### Network

```json
{
  "name": "webapp-net",
  "type": "network",
  "unit": {
    "Network": {
      "Subnet": "10.89.0.0/24",
      "Gateway": "10.89.0.1"
    }
  }
}
```

**Note:** `NetworkName` is automatically set to the spec `name` ("webapp-net") if not specified. You can override it by explicitly setting `"NetworkName": "custom-name"` in the `Network` section.

### Server-side file layout

```
/etc/syslet/
└── config.zip                      # uploaded by deploy script

/etc/containers/
├── systemd/                        # quadlet files (generated by syslet)
│   ├── webapp.container
│   ├── webapp-data.volume
│   └── webapp-net.network
└── config/                         # config files for containers
    └── webapp/
        ├── nginx.conf
        └── env
```
