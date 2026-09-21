# Safety mechanisms

syslet applies changes on a live system without a human confirming each one, so it leans on a few deliberate guardrails rather than asking for confirmation.

## removalAllowed

To prevent accidental deletion, units must be explicitly marked with `"removalAllowed": true` before syslet will remove them.

- If a spec disappears from the input but the corresponding unit on the host does **not** carry this marker, syslet leaves it alone. It will not touch a unit it doesn't have positive confirmation it's allowed to delete.
- A unit already marked `"removalAllowed": true` and no longer present in the input **is** pruned.

In practice this means: mark a unit `removalAllowed: true` once you're confident it's safe to let syslet delete it later, not by default. A container scheduled for removal is stopped first if it's still running.

## reclaimPolicy

For volumes, networks, and builds, `reclaimPolicy` controls what happens to the underlying podman resource (not just the unit file) when the unit is removed:

- `"Retain"` (default) — the unit file is removed, but the podman volume/network/image is left in place.
- `"Delete"` — the podman resource is also deleted.

This is a separate axis from `removalAllowed`: `removalAllowed` gates whether removal happens at all; `reclaimPolicy` decides how thorough that removal is once it does. Reclamation is narrowly scoped: deleting a stale build's image, for instance, must never touch an image tag still referenced by a build unit that remains in the desired set.

## Multi-stage validation

Specs and their rendered output are checked at three points before anything is executed:

1. **Pre-render** — spec-level correctness: no duplicate unit names, no duplicate config paths, `configDirs` require `[Service] ExecReload=`, build Containerfiles are present, secret key names are well-formed, and so on.
2. **Post-render** — checks on the rendered unit file content: every volume/network/build a container references exists as a spec, no conflicting volume mount destinations, build `ImageTag`s use the `localhost/` prefix.
3. **Staging** — the rendered unit files are written to a temporary staging directory and run through podman's own quadlet generator/verifier, catching anything syslet's own checks miss (e.g. unit file syntax podman itself would reject).

A spec that fails any of these three stages is rejected before syslet touches the live system. `syslet --diff` runs all three, so a `--diff` that succeeds means the same apply would also pass validation.
