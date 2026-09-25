# Check changes in CI

Run checks on every push or pull request, so a broken change is caught before it reaches a host.

## Check the CUE config

Add these commands to your pipeline, with the CUE version your repository uses:

```sh
cue vet -c ./...
cue export -e syslet.specRendered --out text > /dev/null
```

`cue vet` checks every entry against the syslet schema, and `cue export` checks that the spec renders.
Neither needs an age key, since secret files stay encrypted.
In a repository with [several hosts](manage-several-hosts.md), check `./hosts/...` and export each host directory.

The schema doesn't check option names under `unit`; only the host's podman can do that.

## Plan against the host

A plan catches what the schema can't: invalid unit options, secrets the host can't decrypt, and changes to a volume that isn't allowed to be recreated.
It needs the host, since syslet diffs against the installed units and decrypts with the host's key.

### Give CI a plan-only key

Create a user for CI on the host, and allow it to run exactly the dry-run as root, in `/etc/sudoers.d/syslet-ci`:

```text
ci ALL=(root) NOPASSWD: /usr/local/bin/syslet --diff --stdin
```

Pin the CI key to that command in `~ci/.ssh/authorized_keys`:

```text
command="sudo /usr/local/bin/syslet --diff --stdin",restrict ssh-ed25519 AAAA... ci
```

With both in place, the key can't apply, even if the pipeline is compromised.

### Run the plan

Add the plan to your pipeline, once per host:

=== "CUE"

    ```sh
    cue export -e syslet.specRendered --out text ./hosts/web01 | ssh ci@web01 sudo syslet --diff --stdin
    ```

=== "JSON"

    ```sh
    cat hosts/web01/*.json | ssh ci@web01 sudo syslet --diff --stdin
    ```

The step fails when the host would refuse the plan, and the log shows the errors.
Secret values print as `(secret)`, so the output is safe for shared logs.

The plan reflects the host as it is now, so review it again before you apply: see [Preview changes with --diff](preview-changes-with-diff.md).
