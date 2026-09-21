# Docs TODO

Tracked gaps that are deliberately out of scope for the initial docs structure:

- **Secrets management docs** — the SOPS+age `secret` unit type is fully implemented but undocumented. Needs its own tutorial (likely framed as a git-repo/push-over-SSH GitOps workflow), how-to guides, an explanation page (content-hash-based change detection, orphan detection via the `syslet/hash` podman label), and a reference page for the secret spec schema — plus a proper threat-model write-up, since secrets end up committed to git as ciphertext.
- **CUE-based spec authoring docs** — plain-JSON spec authoring was found painful in practice; wrapping syslet specs with CUE is the likely direction, but the wrapper itself isn't implemented/decided yet. Document once that lands.
