# Troubleshoot a failed apply

An apply fails in one of two places: while syslet builds the plan, before anything on the host changes, or while it carries the plan out.
The output tells you which.

## The plan has errors

A plan with any error is refused as a whole, and nothing on the host changes.
`--diff` prints the errors instead of the diff:

```text
Validation errors:
  error: post-render validation: container "webapp": references undefined volume "webapp-data"
```

The prefix names the stage that failed (see [Safety mechanisms](../explanation/safety-mechanisms.md#multi-stage-validation)):

- `pre-render validation` and `post-render validation`: syslet's own checks on the specs, such as references to missing specs or secret keys, overlapping mount paths, or `configDirs` without `ExecReload=`. The message names the spec; fix it and preview again.
- `error [<unit>]`: a check on one unit against the host, such as a volume change that the installed markers don't allow (see [Manage volumes](manage-volumes.md#change-a-volume)).
- `secret "<name>": decryption failed`: the host can't decrypt the file. Check that it's encrypted to the host's key (see [Add a SOPS recipient](add-a-sops-recipient.md)).
- `quadlet generator failed` and `unit verification failed`: podman's generator or `systemd-analyze verify` rejected the rendered units, usually because of a misspelled option or a value podman doesn't accept.
- `unit verification warnings`: `systemd-analyze verify` accepted the units but would ignore a setting, such as `Invalid memory limit 'asd', ignoring: Invalid argument`. syslet refuses these like failures, since the unit wouldn't run as specified; fix the setting it names.

## Inspect the staged units

For generator and verification errors, syslet writes the rendered units into a staging directory and keeps it.
Its path is in syslet's log on stderr:

```text
2026/09/23 10:12:03 INFO staging unit files for validation dir=/tmp/syslet-stage-2381640597
```

`units/` holds the quadlet files syslet would install, and `out/` what podman's generator made of them.
Run the generator by hand to see its full output:

```sh
sudo QUADLET_UNIT_DIRS=/tmp/syslet-stage-2381640597/units \
  /usr/lib/systemd/system-generators/podman-system-generator --dryrun
```

Messages the generator logged during the plan are in the journal:

```sh
sudo journalctl -b -t quadlet-generator
```

## A unit failed during the apply

When the plan was fine but an operation failed, syslet logs the failure, carries on with the rest of the plan and exits with an error:

```text
time=2026-09-23T10:12:04.114+02:00 level=ERROR msg="starting failed" unit=webapp.container error="starting webapp.service: job result failed"
webapp.container                         error      starting: starting webapp.service: job result failed
error: one or more units failed to apply
```

Most failures are a container that doesn't start, for example because the image can't be pulled or the process exits.
Look at the service:

```sh
sudo systemctl status webapp.service
sudo journalctl -u webapp.service -n 50
```

`job result dependency` means a unit the container requires failed first, such as its volume, network or build; check that unit's service the same way.

The files syslet wrote before the failure stay in place.
Fix the cause and apply again: syslet diffs against what's now installed and starts the container again, since it isn't running.
