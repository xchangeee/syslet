# syslet

syslet is a GitOps-friendly deployment tool for Podman containers using systemd and quadlets. It reads JSON spec files describing containers, builds, networks, and volumes, and reconciles them onto a single host by generating quadlet unit files and driving systemd/podman, once per invocation rather than running as a long-lived controller.

## Why syslet

For most single-node deployments, level-triggered systems like Kubernetes don't make much sense. Even minimal distributions like k3s or k0s carry a control plane's worth of resource cost and configuration surface for a host whose workload is mostly static, and their reconciliation loop keeps running whether or not anything has changed.

syslet keeps a few things that make the Kubernetes user experience good, without the always-on control plane:

- defining desired state in a high-level, declarative spec instead of shell scripts or manually crafted unit files,
- validation that catches bad config before it's pushed,
- pushing that spec to the remote system and having it start/stop containers for you, instead of doing so by hand.

Its niche is a single host (or a small, independently-managed fleet of hosts) running Podman containers via systemd, where a spec repository and `git push`/`ssh` is a better fit than standing up a scheduler and its supporting infrastructure. See [Reconciliation model](explanation/reconciliation-model.md) for how syslet gets this without running continuously, and [Alternatives and comparisons](explanation/alternatives-and-comparisons.md) for how it stacks up against the other tools in this space.

## Where to go next

- **[Tutorials](tutorials/index.md)** — learn syslet hands-on, starting from a first deployment.
- **[How-to guides](how-to/index.md)** — task-oriented recipes for specific deployment scenarios.
- **[Explanation](explanation/index.md)** — background and design rationale.
- **[Reference](reference/index.md)** — CLI flags, spec schemas, file layout, and more.
