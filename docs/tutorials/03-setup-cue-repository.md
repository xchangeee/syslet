# Set up CUE repository

syslet consumes JSON, which is a machine format: writing it by hand means repeating the same fields in every spec and fighting braces, quotes and commas.
[CUE](https://cuelang.org/) adds defaults, comments and validation against a schema, removes the duplication, and still produces the JSON syslet reads.

In this tutorial, you'll set up a git repository that describes a host in CUE instead of raw JSON, and connect it to the host with `cue cmd plan` and `cue cmd apply`.
The following tutorials deploy into it.

## Prerequisites

- An empty server with syslet installed, as in [Installation](01-installation.md).
  This tutorial calls it `web01.example.com`.
- [CUE](https://cuelang.org/docs/introduction/installation/) v0.16 or newer on your workstation.
- SSH public-key auth to the server as root.
  The commands below run `ssh <host> syslet`; if you log in as another user, add `sudo` to the `cmd` lists in `syslet_tool.cue`.

## 1. Create the repository

```sh
mkdir infra && cd infra
git init -q
cue mod init --source=git example.com/infra@v0
```

## 2. Add the base config

Create `syslet.cue`:

```cue title="syslet.cue"
--8<-- "03-setup-cue-repository/syslet.cue"
```

`sysdef` is where you describe the host: containers, networks, volumes, builds and secrets, each keyed by name.

`sysletcore.#Sysdef` on its own is only the schema: it defines which fields exist and requires you to set each of them.
Several of those fields take the same value almost every time, so `sysletcore.#SysdefDefaults` fills in `enabled: true`, `removalAllowed: true`, `desiredState: "running"` and `reclaimPolicy: "Delete"` unless you set them, and you don't have to repeat them in every entry.

## 3. Add the plan and apply commands

Create `syslet_tool.cue`.
Both commands feed `syslet.specRendered` to `syslet --stdin` over SSH.
`plan` runs it with `--diff`, and `apply` shows the same diff, asks for confirmation, then runs syslet for real:

```cue title="syslet_tool.cue"
--8<-- "03-setup-cue-repository/syslet_tool.cue"
```

These two files are a starting point; adapt them to your setup.

## 4. Check the setup

Add the syslet schema to `cue.mod/module.cue`, pinned to the latest `v0` release:

```sh
cue mod tidy
```

Check that it evaluates:

```sh
cue export -e syslet.specRendered --out text
```

This prints `[]`, since nothing is defined yet.
Now check the connection to the host:

```sh
cue cmd plan
```

On an empty server, syslet finds nothing to do:

```text
Summary:
UNIT                                     STATUS     CHANGES

No changes detected. All units are up to date.
```

Commit the setup:

```sh
git add -A && git commit -m "set up syslet repository"
```

The repository is ready.
Continue with [Deploy with CUE](04-deploy-with-cue.md).
