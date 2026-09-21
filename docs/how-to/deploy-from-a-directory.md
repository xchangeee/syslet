# Deploy from a directory

`syslet` can read a directory of `.json` spec files directly, which suits specs checked out from a git repository rather than streamed in:

```sh
scp -r hosts/web01/ web01:/etc/syslet/hosts/web01/
ssh web01 sudo syslet /etc/syslet/hosts/web01/
```

Each `.json` file in the directory is loaded as one spec (non-`.json` files and subdirectories are ignored). This is the mode used by the [GitOps with webhookd](set-up-gitops-with-webhookd.md) workflow, where a checked-out repository directory is passed directly as the path argument.

Unlike `--stdin`, applying from a directory or an explicit file path does **not** persist anything to `/etc/syslet/config.json`. The directory (or file) you pass is itself the on-disk record.
