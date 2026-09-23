# Preview changes with --diff

Preview every change before you apply it, so a restart, a removal or a deleted volume never comes as a surprise.
`--diff` builds the same plan an apply would carry out, and changes nothing on the host.

## 1. Run the plan

=== "CUE"

    ```sh
    cue cmd plan
    ```

=== "JSON"

    ```sh
    cat hosts/web01/*.json | ssh web01 sudo syslet --diff --stdin
    ```

    To preview against the specs already on the host, run `ssh web01 sudo syslet --diff` for `/etc/syslet/config.json`, or pass the spec directory.

If the output starts with `Validation errors:`, the plan is refused as a whole; see [Troubleshoot a failed apply](troubleshoot-a-failed-apply.md#the-plan-has-errors).

## 2. Check downtime

Read the `CHANGES` column of the summary:

- `restarted` or `stopped`: the container goes down. If that's not what you meant, see [Manage containers](manage-containers.md#change-a-container).
- `reloaded`: the container reloads its config in place, without downtime.

`Services to stop:` lists every unit that stops, including containers restarted because a volume, network, build or secret they use changes.

## 3. Check removals

- Every `removed` unit goes away, and under `reclaimPolicy: "Delete"` its podman resource with it.
- `Podman volumes to delete:` lists volumes whose data is lost. Stop if a volume there should keep its data, and [lock it](lock-resources.md).
- `skipped` means a stale unit is protected and stays. If you meant to remove it, unlock it first (see [Lock resources](lock-resources.md#unlock-an-entry)).

A plan with unexpected `removed` lines usually means the input is incomplete, such as a missing file or a disabled entry. Fix the input instead of applying.

## 4. Keep secrets out of logs

`Secret changes:` prints new values in plain text.
Don't run the plan where its output ends up in shared logs, such as CI.

For every section and status, see [Plan output](../reference/plan-output.md).
