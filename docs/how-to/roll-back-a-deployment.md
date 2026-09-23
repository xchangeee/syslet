# Roll back a deployment

syslet has no rollback command and keeps no history.
To roll back, apply the specs of the last good state again; syslet diffs them against the host like any other change.

## Revert in git

With the specs in a git repository, revert the commit that broke the host:

=== "CUE"

    ```sh
    git revert <commit>
    cue cmd plan
    ```

=== "JSON"

    ```sh
    git revert <commit>
    cat hosts/web01/*.json | ssh web01 sudo syslet --diff --stdin
    ```

Check the plan, then apply it, or push it with GitOps.

<!-- TODO: say whether /etc/syslet/config.json can serve as a fallback once --stdin persists only after validation, see docs/TODO.md -->

## What a rollback can't restore

Applying old specs restores unit files, config files and secrets, but not what the bad apply deleted:

- A volume removed under `reclaimPolicy: "Delete"`, or recreated by a change, comes back empty.
- A built image deleted under `"Delete"` is rebuilt on start, and a pulled image is pulled again if it's gone.
- A podman network or volume left behind under `"Retain"` is reattached as it is.

Check the plan of a risky change for `Podman volumes to delete:` before applying it, and lock volumes whose data matters (see [Lock resources](lock-resources.md)).
