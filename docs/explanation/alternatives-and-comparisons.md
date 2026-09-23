# Alternatives and comparisons

!!! note
    Positioning and content for this page are still being worked out (tbd).

Rough axes to compare syslet against, for whoever fills this in:

- **Kubernetes / k3s / k0s** — level-triggered, continuously reconciling; brings a scheduler, API server, and cluster networking model that a single-node deployment doesn't need. syslet targets the case where that overhead isn't justified. See [Why syslet](why-syslet.md).
- **Hand-rolled quadlets** — writing `.container`/`.volume`/`.network` unit files directly and managing `systemctl daemon-reload`/start/stop by hand. syslet adds a validated, declarative spec layer and safe apply/prune semantics on top, at the cost of an extra tool in the loop.
- **Ansible / other config-management tools** — general-purpose, procedural, and not specific to quadlets; can express far more than container deployment but without syslet's typed spec validation or diff/plan model for this specific use case.
- **Nomad / other single-binary schedulers** — closer in spirit (lightweight, no control-plane cluster required for a single node) but still a long-running agent; syslet is edge-triggered and does not run continuously.
