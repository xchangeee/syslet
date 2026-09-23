# Why syslet

For most single-node deployments, level-triggered systems like Kubernetes don't make much sense.
Even minimal distributions like k3s or k0s carry a control plane's worth of resource cost and configuration surface for a host whose workload is mostly static, and their reconciliation loop keeps running whether or not anything has changed.

syslet keeps a few things that make the Kubernetes user experience good, without the always-on control plane:

- defining desired state in a high-level, declarative spec instead of shell scripts or manually crafted unit files,
- validation that catches bad config before it's pushed,
- pushing that spec to the remote system and having it start/stop containers for you, instead of doing so by hand.

Its niche is a single host (or a small, independently-managed fleet of hosts) running Podman containers via systemd, where a spec repository and `git push`/`ssh` is a better fit than standing up a scheduler and its supporting infrastructure.

See [Reconciliation model](reconciliation-model.md) for how syslet gets this without running continuously.
