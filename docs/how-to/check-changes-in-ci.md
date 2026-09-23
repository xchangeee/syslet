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

<!-- TODO: add "Plan against the host" once --diff exits non-zero on validation errors and can redact secrets, see docs/TODO.md -->
