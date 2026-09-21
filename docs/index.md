# syslet

syslet is a GitOps-friendly deployment tool for Podman containers using systemd and quadlets. It reads JSON spec files describing containers, builds, networks, and volumes, and reconciles them onto a single host by generating quadlet unit files and driving systemd/podman — once per invocation, rather than running as a long-lived controller.

- **[Tutorials](tutorials/index.md)** — learn syslet hands-on, starting from a first deployment.
- **[How-to Guides](how-to/index.md)** — task-oriented recipes for specific deployment scenarios.
- **[Explanation](explanation/index.md)** — background and design rationale.
- **[Reference](reference/index.md)** — CLI flags, spec schemas, file layout, and more.
