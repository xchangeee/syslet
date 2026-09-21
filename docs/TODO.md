# Docs TODO

Tracked gaps that are deliberately out of scope for the initial docs structure:

- **Secrets management docs** — the SOPS+age `secret` unit type is fully implemented but undocumented. Needs its own tutorial (likely framed as a git-repo/push-over-SSH GitOps workflow), how-to guides, and an explanation page (content-hash-based change detection, orphan detection via the `syslet/hash` podman label), plus a proper threat-model write-up, since secrets end up committed to git as ciphertext.
- **CUE-based spec authoring docs** — plain-JSON spec authoring was found painful in practice; wrapping syslet specs with CUE is the likely direction, but the wrapper itself isn't implemented/decided yet. Document once that lands.
- **Apply phase ordering as a removal guarantee** — dropped from [removing-specs.md](explanation/removing-specs.md) rather than half-explained. The ordering is what keeps reclamation from tripping over a live container in the two cases validation doesn't cover: removing a container together with its volume (neither is in the input, so the reference check is silent, and the container's stop in phase 1 is what lets the volume delete in phase 5 succeed), and recreating a changed volume or network (unit and containers all stay in the input, containers stopped in phase 1 and restarted in phase 7 around the delete). Decide whether this belongs here, in [workflow.md](explanation/workflow.md), or nowhere.

## Implementation gaps

Found while writing the docs; left undocumented on purpose until fixed, so the docs don't describe a hole as if it were a design.

- **Reclamation can strand a skipped container** — `validate.PostRender` checks that every volume/network/build a container references exists, but only across the *desired set*. Nothing checks references from units still installed on the host. So dropping a container spec and its volume spec in one change, where the container is `removalAllowed: false` (skipped, left running) and the volume is `removalAllowed: true` + `reclaimPolicy: "Delete"`, reclaims the podman volume out from under a container that is still mounting it. A host-side reference check over skipped/stale units before emitting `DeletePodmanVolume`/`Network`/`Image` ops would close it. See [removing-specs.md](explanation/removing-specs.md) for the behavior as currently documented.
