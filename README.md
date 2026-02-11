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

JSON specs can be updated directly via SSH using `syslet-push`.
Alternatively, you can setup an ArgoCD-style workflow with all JSON specs in a git repository and a [webhookd](https://github.com/ncarlier/webhookd) script to pull the git repository and run syslet on the checked out directory.

To prevent accidental deletion, units must be explicitly marked with `"removalAllowed": true` before they can be removed.
If a spec disappears from the input but the unit file on the host does not have this marker, syslet will not perform changes to this unit.
Units already marked with `"removalAllowed": true` and not present in the current input (zip or directory) will be pruned from the host.

Container units scheduled for removal are stopped if they are still running.

For volumes and networks, the spec's `reclaimPolicy` determines if syslet will also remove the podman volume or network in addition to removing the unit files.

## Getting started

### 1. Build

```sh
make build-bin
```

Requires Go 1.25+. This produces to sets of binaries:

- `build/syslet-<arch>` — server-side binary that applies specs
- `build/syslet-push-<arch>` — client-side deployment tool

### 2. Install on the server

Make sure podman is installed on the target server, e.g. run `dnf install podman`.

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
      "mountPath": "/etc/nginx/nginx.conf"
    },
    {
      "content": "APP_ENV=production",
      "mountPath": "/run/env"
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
- For containers, when `desiredState: "running"` is set, `[Install]WantedBy=multi-user.target default.target` will be added to enable auto-start on boot
- For containers, when `desiredState: "running"` is set, `[Service]Restart=Always` will be added so the container is restarted after crashes
- For containers, `configs` define files to be bind-mounted into the container -- syslet writes them to `/etc/containers/config/<name>/` and injects read-only bind mounts into the quadlet

### 4. Deploy

#### with `syslet-push`

```sh
# Deploy from a directory
syslet-push --directory hosts/web01/ web01

# Or pipe JSON specs from stdin (JSON array or newline-delimited JSON)
cat specs.json | syslet-push --stdin web01
```

`syslet-push` zips the spec files, copies them to the remote host, and runs syslet via SSH.
The deployment host must be able to connect to the remote host via ssh public key authentication. password auth is not supported.

#### manually

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

#### GitOps with [webhookd](https://github.com/ncarlier/webhookd)

Example webhookd workflow:

1. **Commit and push** specs to your git repository
2. **Git webhook** triggers webhookd on your server
3. **webhookd script** pulls the latest specs and runs `syslet`
4. **Containers update** automatically to match the desired state

Generate a deploy key on the server:

   ```bash
   mkdir -p /etc/syslet
   ssh-keygen -t ed25519 -f /etc/syslet/deploykey -N "" -C "syslet-deploy"
   chmod 600 /etc/syslet/deploykey
   ```

Add the public key (`/etc/syslet/deploykey.pub`) to your git repository as a deploy key with read-only access.

Test the connection:

  ```bash
  ssh -i /etc/syslet/deploykey -T git@git.example.com
  ```

Add webhookd script (`/etc/webhookd/scripts/deploy-syslet.sh`):

```bash
#!/bin/bash
set -e

# Use SSH URL for private repositories with deploy key authentication
REPO_URL="git@git.example.com:your-org/your-syslet-specs.git"
REPO_DIR="/etc/syslet/repo"
REPO_BRANCH="main"
DEPLOY_KEY="/etc/syslet/deploykey"

# Configure git to use the deploy key
export GIT_SSH_COMMAND="ssh -i $DEPLOY_KEY -o StrictHostKeyChecking=accept-new"

# Clone repo if not present, otherwise pull latest changes
if [ ! -d "$REPO_DIR/.git" ]; then
    mkdir -p "$(dirname "$REPO_DIR")"
    git clone "$REPO_URL" "$REPO_DIR"
fi

cd "$REPO_DIR"
git pull origin "$REPO_BRANCH"
syslet hosts/$(hostname)/
```

#### Using CUE for better ergonomics

While syslet only accepts JSON, writing raw JSON by hand can be verbose and error-prone. [CUE](https://cuelang.org/) provides a better authoring experience.

Example CUE spec:

TODO add examples with local push and git ops

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
      "mountPath": "/path/in/container"
    }
  ],
  "removalAllowed": false
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
  },
  "reclaimPolicy": "Retain",
  "removalAllowed": false
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
  },
  "reclaimPolicy": "Retain",
  "removalAllowed": false
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
