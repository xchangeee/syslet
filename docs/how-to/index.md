# How-to guides

These guides solve specific tasks on a host you already deploy to with syslet.
If you're new to syslet, start with the [Tutorials](../tutorials/index.md).

Most guides show both a CUE repository and plain JSON specs; the CUE guides at the end only apply to a CUE repository.

## Deploy

Getting specs to a host, and what to do when an apply goes wrong.

- [Deploy over SSH](deploy-over-ssh.md)
- [Preview changes with --diff](preview-changes-with-diff.md)
- [Check changes in CI](check-changes-in-ci.md)
- [Roll back a deployment](roll-back-a-deployment.md)
- [Troubleshoot a failed apply](troubleshoot-a-failed-apply.md)
- [Migrate hand-written quadlets](migrate-hand-written-quadlets.md)

## Containers

Changing what runs, and how it restarts.

- [Manage containers](manage-containers.md)
- [Upgrade a container image](upgrade-a-container-image.md)
- [Set environment variables](set-environment-variables.md)
- [Mount config files and dirs](mount-config-files-and-dirs.md)
- [Set systemd service options](set-systemd-service-options.md)
- [Add dependencies between containers](add-dependencies-between-containers.md)
- [Run a oneshot job](run-a-oneshot-job.md)
- [Build a container image](build-a-container-image.md)

## Volumes

Volumes hold data, so these guides are about keeping it.

- [Manage volumes](manage-volumes.md)
- [Rename a volume](rename-a-volume.md)
- [Adopt an existing volume](adopt-an-existing-volume.md)

## Networks

- [Manage networks](manage-networks.md)
- [Connect containers on a network](connect-containers-on-a-network.md)

## Secrets

Using secrets in containers, and managing the keys that encrypt them.

- [Pass a secret to a container](pass-a-secret-to-a-container.md)
- [Rotate a secret](rotate-a-secret.md)
- [Add a SOPS recipient](add-a-sops-recipient.md)
- [Rotate the SSH host key](rotate-the-ssh-host-key.md)
- [Use a dedicated decryption key](use-a-dedicated-decryption-key.md)

## CUE

Tools of the CUE schema for protecting entries, and for larger repositories.

- [Lock resources](lock-resources.md)
- [Disable a unit](disable-a-unit.md)
- [Manage several hosts](manage-several-hosts.md)
- [Load secret files](load-secret-files.md)
