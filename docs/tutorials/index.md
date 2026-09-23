# Tutorials

These tutorials take you from an empty server to a host where config changes pushed to git are applied automatically.
Work through them in order; each one builds on the last.

First, install syslet and deploy a container from a hand-written JSON spec, to see how a plan and an apply work:

- [Installation](01-installation.md)
- [Deploy container](02-deploy-a-container.md)

Then describe the host in a CUE repository, which the rest of the tutorials build on:

- [Set up CUE repository](03-setup-cue-repository.md)
- [Deploy with CUE](04-deploy-with-cue.md)
- [Protect a container](05-protect-a-container.md)

Finally, add encrypted secrets and apply every change pushed to git automatically:

- [Set up SOPS encryption](06-setup-sops-encryption.md)
- [Add nginx basic auth](07-add-nginx-basic-auth.md)
- [Set up GitOps with webhookd](08-setup-gitops-with-webhookd.md)

At the end, `web01` runs an nginx container with a config file and a password-protected path, all described in git.
