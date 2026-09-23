# Reconciliation model

syslet is an **edge-triggered desired-state reconciler**: it runs once per invocation, computes a plan from the difference between the desired specs and the state already on disk, applies that plan, and exits. There is no long-running syslet process or control loop watching for drift.

This contrasts with **level-triggered** systems like Kubernetes, which run a continuous reconciliation loop that repeatedly compares desired vs. actual state, even when nothing has changed, so that the system self-heals from drift caused by anything other than the tool itself (a node dying, a pod being killed out of band, etc.). That loop is valuable at fleet scale, but on a single, mostly-static host it mostly adds resource overhead and complexity for very little benefit. See [Why syslet](why-syslet.md).

syslet gets the properties people want from that model (declarative desired state, validation before changes land, "converges to what I asked for") without the loop, by delegating the parts that benefit from continuous supervision to systemd:

- **Ongoing health/liveness**: systemd's `Restart=Always` (which syslet sets automatically for `desiredState: "running"` containers) restarts a crashed container without syslet needing to be involved.
- **Boot-time startup**: `WantedBy=multi-user.target default.target` (also set automatically) makes containers come up on boot without syslet running at boot time.
- **Applying a specific change**: happens when you invoke syslet (over SSH, from a webhookd-triggered git pull, or manually) and not on any other schedule.

The tradeoff is explicit: syslet does not detect or correct drift introduced between invocations by something other than syslet itself (e.g. someone manually editing a unit file, or Podman state diverging in a way not deducible from the previous apply). Re-running syslet re-establishes the desired state, but nothing does that for you automatically. You decide when reconciliation happens.
