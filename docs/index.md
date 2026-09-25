# syslet

syslet (systemd + kubelet) enables GitOps-based deployments of Podman containers using systemd and quadlets.
It reads JSON spec files describing containers, builds, networks, and volumes, and reconciles them onto a single host by generating quadlet unit files and driving systemd/podman.

It brings some aspects of the Kubernetes user experience (declarative specs, validation and a diff preview before anything changes, cleanup of what's no longer declared) to a single host or a small fleet, without a control plane running on it.
See [Why syslet](explanation/why-syslet.md) for the reasoning.

## Where to go next

- **[Tutorials](tutorials/index.md)** — learn syslet hands-on, starting from a first deployment.
- **[How-to guides](how-to/index.md)** — task-oriented recipes for specific deployment scenarios.
- **[Explanation](explanation/index.md)** — background and design rationale.
- **[Reference](reference/index.md)** — CLI flags, spec schemas, file layout, and more.
