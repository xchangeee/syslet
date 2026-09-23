# Safety mechanisms

syslet applies changes on a live system without a human confirming each one, so it leans on a few guardrails rather than asking for confirmation.

## Removal guardrails

Deletion is the one operation with no undo, so it has guardrails of its own: `removalAllowed` gates whether a stale unit is removed at all, and `reclaimPolicy` decides how far that removal reaches into podman's own resources. Both are covered in detail, per unit type, in [Removing specs](removing-specs.md).

## Multi-stage validation

Specs and their rendered output are checked at three points before anything is executed:

1. **Pre-render** — spec-level correctness: no duplicate unit names, no duplicate config paths, `configDirs` require `[Service] ExecReload=`, build Containerfiles are present, secret key names are well-formed, and so on.
2. **Post-render** — checks on the rendered unit file content: every volume/network/build a container references exists as a spec, no conflicting volume mount destinations, build `ImageTag`s use the `localhost/` prefix.
3. **Staging** — the rendered unit files are written to a temporary staging directory and run through podman's own quadlet generator/verifier, catching anything syslet's own checks miss (e.g. unit file syntax podman itself would reject).

A spec that fails any of these three stages is rejected before syslet touches the live system. `syslet --diff` runs all three, so a `--diff` that succeeds means the same apply would also pass validation.
