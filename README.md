# syslet

syslet is a GitOps-friendly deployment tool for [Podman](https://podman.io/) containers using [systemd](https://systemd.io/) and [quadlets](https://docs.podman.io/en/latest/markdown/podman-systemd.unit.5.html). Instead of manually crafting unit files on the remote host and running `systemctl daemon-reload`, `start`, `stop`, etc, you manually craft your container definitions as JSON specs in a local directory and let syslet handle the translation to Podman Quadlet units and`systemctl`/`podman` interaction.

For most single-node deployments, level-triggered systems (e.g., Kubernetes) do not make much sense. Even minimal distributions like k3s or k0s still require a considerable amount of resources to run, introduce a ton of configuration complexity even though single node deployments are mostly-static, and continuous reconciliation increases the system load for no good reason.

syslet keeps a few things that I like about the Kubernetes user experience: Defining the desired state in a high-level JSON spec, a bit of validation to prevent bad config from being pushed, being able to push JSON specs to the remote system and not having to manually start or stop containers. It's implemented as an **edge-triggered desired-state reconciler**, e.g. syslet executes once per invocation and delegates the hard work to systemd and podman.

Supported unit types: `container`, `volume,` `network`

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

JSON specs can be updated either directly via SSH using `syslet-push`.

Alternatively you can setup an ArgoCD-style workflow with all JSON specs in a git repository and a [webhookd](https://github.com/ncarlier/webhookd) script to pull the git repository and run syslet on the checked out directory.

Files not present in the current input (zip or directory) are pruned from the host. This means removing a spec and re-running syslet will stop the container and clean up its files.

For volumes and networks, the spec's `reclaimPolicy` determins if syslet will also remove the podman volume or network from in addition to removing the unit files.

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

Key points:

- `desiredState` controls whether syslet starts (`running`) or stops (`stopped`) the container
- `unit` maps 1:1 to systemd unit file sections and their options
- unit properties `ContainerName`, `VolumeName`, and `NetworkName` are automatically set to match the spec `name` if not explicitly specified
- For containers the `[Install]` section will be added when `desiredState: "running"` to enable auto-start on boot
- For containers, `configs` define files to be bind-mounted into the container -- syslet writes them to `/etc/containers/config/<name>/` and injects the corresponding `Volume=` entries

### 4. Deploy

The deployment host must be able to connect to the remote host via ssh public key authentication. password auth is not supported.

Use the `syslet-push` command:

```sh
# Deploy from a directory
syslet-push --directory hosts/web01/ web01

# Or pipe JSON specs from stdin (JSON array or newline-delimited JSON)
cat specs.json | syslet-push --stdin web01
```

`syslet-push` zips the spec files, copies them to the remote host, and runs syslet via SSH.

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
