# Why syslet

syslet manages containers on a single host, or on a small fleet of hosts.
There are several established ways to do that, so the obvious question is: why not one of those?

## Why no plain Docker?

The most familiar option is `docker run` by hand.
Containers started this way survive reboots as long as they aren't deleted, but their configuration lives only in the command that created them.
Compose files add a declarative description of containers, networks and volumes.

But config files and directories still have to be managed separately, including cleaning up stale ones, and volumes dropped from the Compose file stay on the host until removed by hand.
The Compose file itself has to be synced to the host by hand, which becomes annoying sooner or later.

Either way, containers are managed by the Docker daemon rather than systemd, so they don't integrate with things like unit dependencies or `sd_notify` readiness.
A unit wrapping `docker run` only supervises the client, while the container itself runs under the daemon.

## What about Podman then?

Podman is what syslet builds on.
It has the same CLI as Docker, but runs containers without a daemon.
Its containers run as descendants of the process that started them, so when systemd starts one, it lands in that unit's cgroup and systemd supervises it like any other service: restarts, dependencies, resource limits and journal logging all apply.
Podman also passes `sd_notify` readiness through from the container, so dependent units wait until it's actually ready.

Podman can run Compose files too, but that just calls `podman run` and bypasses systemd.
For systemd integration, Podman ships Quadlet, which turns short `.container`, `.volume` and `.network` files into systemd units that run Podman and hook into systemd mechanisms like `sd_notify` readiness, dependencies and restarts.

## So we use Quadlet files?

Writing Quadlet files by hand runs into the same problems as maintaining Compose files with Docker.
Config files, directories and volumes around the units are still yours to manage, including removing stale ones, and every change means editing the right files, reloading systemd and restarting the right units.
`.volume` and `.network` units only create their resource if it doesn't exist yet: changing their options doesn't touch the existing one, and removing the unit leaves it behind.
The unit files and config usually live somewhere else too, and keeping that copy and the host in sync becomes annoying sooner or later.
While Podlet can generate Quadlet files from a Compose file, it has its own problems: not every Compose feature maps to Quadlet, and it's a one-off conversion, so the generated files still have to be deployed and maintained by hand.

## But we can use Ansible, no?

Configuration management tools like Ansible or pyinfra are a common answer to that syncing problem.
They work and are widely used, and their individual modules try hard to be idempotent.
But wiring those modules together is still a sequence of steps you write yourself.
As soon as behavior becomes conditional, like restarting a unit only when its config changed or removing what's no longer declared, the risk of missing an edge case grows quickly.
Infrastructure is better expressed as data than as code: you declare the end state, and the tool works out the steps to get there, including the edge cases.

## Fuck it, let's do Kubernetes

Kubernetes treats infrastructure as data and solves these problems and a lot more, but at the cost of much more complexity, since it's designed for clusters.
Even minimal distributions like k3s or k0s carry a control plane's worth of resource cost and configuration surface for a host whose workload is mostly static, and their reconciliation loop keeps running whether or not anything has changed.
In our experience, on a small VPS, an otherwise idle k3s still takes around 800 MB of memory and 30% of a CPU core while doing basically nothing.

## What syslet does instead

Some aspects of the Kubernetes user experience are still worth having on a single node: an object model that fully describes the desired state of a deployment, previewing changes similar to `kubectl diff`, and declarative updates similar to `kubectl apply`, without a control plane.
syslet keeps the plain systemd and Podman setup and adds just enough of that model on top, tailored to a single node:

- a high-level, declarative [spec](spec-types.md) of the desired state, instead of shell scripts or manually crafted unit files,
- [config files and directories](../how-to/mount-config-files-and-dirs.md) declared as part of the container, similar to a ConfigMap but not a separate object; a changed directory is swapped atomically and reloaded without restarting the container,
- [validation](safety-mechanisms.md#multi-stage-validation) that catches bad config before it's applied,
- a `--diff` preview of exactly what an [apply](how-an-apply-works.md) would change,
- an apply that manages the lifecycle of containers, volumes, networks and builds on the host, including [removing stale ones](removing-specs.md),
- [removal protection](safety-mechanisms.md#removal-protection) declared in advance, inspired by Argo CD's deletion finalizers, so a spec that goes missing by mistake doesn't take a volume's data with it.

systemd supervises and orders the containers, Podman runs them, and specs are written in CUE (or plain JSON) instead of a config language of syslet's own.

In a small fleet, each host is managed independently with its own spec and its own apply.
There's no fleet-wide apply like running an Ansible playbook against an inventory.

syslet also doesn't correct drift between applies.
It assumes it's the sole manager of the host, and that when several people work on it, every change goes through a git repository rather than by hand.

See [Reconciliation model](reconciliation-model.md) for how syslet gets this without running continuously.
