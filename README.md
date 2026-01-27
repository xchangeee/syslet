# rsystemd

Declarative container management for Podman. Define your containers, volumes, and networks as unit files, push them to a server, and `rsystemd` keeps them running. Designed for gitops pipelines.

```
rsctl apply -f ./hosts/web01/ ---gRPC---> rsystemd daemon ---D-Bus---> systemd
```

## Getting started

### 1. Build

```sh
make tools   # one-time: install protoc-gen-go and protoc-gen-go-grpc
make build   # generate proto + build rsystemd and rsctl
```

Requires Go and `protoc` (Protocol Buffers compiler).

### 2. Set up the daemon on your server

Copy the `rsystemd` binary to the server and run it:

```sh
sudo rsystemd
```

It listens on `:7233` by default. Override with `RSYSTEMD_LISTEN=:9000 sudo rsystemd`.

### 3. Connect rsctl to the server

On your workstation, add the server as a context:

```sh
rsctl context add web01 --server 10.0.0.5:7233
```

This saves the connection to `~/.config/rsctl/config.yaml`. The first context you add becomes the active one automatically.

### 4. Create your unit files

Create a directory for the server with your Podman quadlet files:

```
hosts/web01/
├── webapp.container
├── webapp-data.volume
├── webapp-net.network
└── configs/
    └── webapp/
        ├── app.conf
        └── env
```

A container unit file looks like this:

```ini
[Container]
Image=docker.io/library/nginx:latest
PublishPort=8080:80
Volume=webapp-data.volume:/data
Volume=/etc/containers/config/webapp/app.conf:/etc/nginx/nginx.conf:ro
Volume=/etc/containers/config/webapp/env:/run/env:ro
Network=webapp-net.network

[Install]
WantedBy=multi-user.target default.target

[X-Rsystemd]
DesiredState=running
```

The `[X-Rsystemd]` section tells the daemon what state to maintain. `DesiredState=running` means keep it running; `DesiredState=stopped` means keep it stopped.

Any files under `configs/<unit-name>/` in the same directory are automatically included when you run `rsctl apply`. For example, the tree above includes `configs/webapp/app.conf` and `configs/webapp/env` — these are synced to `/etc/containers/config/webapp/` on the server, where the container can mount them via `Volume=` directives as shown above.

### 5. Apply

```sh
rsctl apply -f hosts/web01/
```

The daemon receives the files, installs them, and reconciles toward the desired state. You'll see output like:

```
webapp.container               changed  unit file updated, restarting
webapp-data.volume             unchanged
webapp-net.network             unchanged
```

### 6. Manage

```sh
rsctl list                          # list all managed units
rsctl list --type=container         # filter by type
rsctl status webapp.container       # detailed unit status
rsctl logs webapp.container         # recent logs
rsctl logs webapp.container -f      # follow logs
rsctl delete webapp.container       # stop and remove
```

## Managing multiple servers

rsctl uses a context system (similar to kubectl) to manage connections to multiple servers.

### Config file

Stored at `~/.config/rsctl/config.yaml` (respects `XDG_CONFIG_HOME`):

```yaml
current-context: web01
contexts:
  web01:
    server: 10.0.0.5:7233
  web02:
    server: 10.0.0.6:7233
  staging:
    server: staging.example.com:7233
```

### Context commands

```sh
rsctl context add web02 --server 10.0.0.6:7233   # add a server
rsctl context list                                 # show all contexts
rsctl context use web02                            # switch active context
rsctl context remove staging                       # remove a context
```

### Overrides

Use `--context` to target a specific server without switching:

```sh
rsctl --context web02 list
```

Use `-s` to bypass contexts entirely with a one-off address:

```sh
rsctl -s 10.0.0.99:7233 list
```

Priority: `-s` flag > `--context` flag > `current-context` from config > `localhost:7233`.

## Directory layout

### On the server

```
/etc/rsystemd/
└── units/                          # managed unit files
    ├── webapp.container
    ├── webapp-data.volume
    └── webapp-net.network

/etc/containers/
├── systemd/                        # quadlet files (installed by rsystemd)
│   ├── webapp.container
│   ├── webapp-data.volume
│   └── webapp-net.network
└── config/                         # config files for containers
    └── webapp/
        ├── app.conf
        └── env
```

### Local gitops repository

```
my-infra/
└── hosts/
    ├── web01/
    │   ├── webapp.container
    │   ├── webapp-data.volume
    │   └── configs/
    │       └── webapp/
    │           └── app.conf
    └── web02/
        └── ...
```

## Unit file reference

### Container

```ini
[Container]
Image=docker.io/library/nginx:latest
PublishPort=8080:80
Volume=webapp-data.volume:/data
Network=webapp-net.network

[Install]
WantedBy=multi-user.target default.target

[X-Rsystemd]
DesiredState=running
```

### Volume (immutable after creation)

```ini
[Volume]
Label=app=webapp

[X-Rsystemd]
```

### Network (immutable after creation)

```ini
[Network]
Subnet=10.89.0.0/24
Gateway=10.89.0.1

[X-Rsystemd]
```

## Supported unit types

| Type | Extension | Startable | Mutable | Notes |
|------|-----------|-----------|---------|-------|
| Container | `.container` | Yes | Yes | Enablement via `[Install] WantedBy=` |
| Volume | `.volume` | No | Immutable | Created once, changes rejected |
| Network | `.network` | No | Immutable | Created once, changes rejected |

All files are Podman quadlets installed to `/etc/containers/systemd/`.

## How reconciliation works

The daemon runs a reconciliation loop every 10 seconds (configurable). It operates in two phases:

**Phase 1 -- Diff:** Read all managed units, compare checksums against installed state, check active state against desired state.

**Phase 2 -- Execute** (strict order):

1. Stop units that need updating (uses old config for clean shutdown)
2. Write changed config files
3. Write changed unit files
4. Single `daemon-reload` if any unit files changed
5. Start units that should be running

Volumes and networks are immutable after creation -- the reconciler rejects changes and logs an error.

## gRPC API

The daemon exposes a gRPC API. See [proto/rsystemd.proto](proto/rsystemd.proto) for the full definition.

| RPC | Description |
|-----|-------------|
| `Apply` | Push unit files and configs, trigger reconciliation |
| `Status` | Get status of one or all managed units |
| `List` | List managed units with optional type filter |
| `Logs` | Stream journal logs for a unit |
| `Delete` | Stop and remove a unit |
