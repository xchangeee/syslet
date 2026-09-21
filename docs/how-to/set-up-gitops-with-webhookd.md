# Set up GitOps with webhookd

This sets up an ArgoCD-style pull-based workflow: specs live in a git repository, and a [webhookd](https://github.com/ncarlier/webhookd) script pulls the latest commit and runs syslet whenever the repository's webhook fires.

1. **Commit and push** specs to your git repository.
2. **Git webhook** triggers webhookd on your server.
3. **webhookd script** pulls the latest specs and runs `syslet`.
4. **Containers update** automatically to match the desired state.

## 1. Generate a deploy key on the server

```sh
mkdir -p /etc/syslet
ssh-keygen -t ed25519 -f /etc/syslet/deploykey -N "" -C "syslet-deploy"
chmod 600 /etc/syslet/deploykey
```

Add the public key (`/etc/syslet/deploykey.pub`) to your git repository as a deploy key with read-only access.

Test the connection:

```sh
ssh -i /etc/syslet/deploykey -T git@git.example.com
```

## 2. Add the webhookd script

`/etc/webhookd/scripts/deploy-syslet.sh`:

```bash
#!/bin/bash
set -e

# Use SSH URL for private repositories with deploy key authentication
REPO_URL="git@git.example.com:your-org/your-syslet-specs.git"
REPO_DIR="/etc/syslet/repo"
REPO_BRANCH="main"
DEPLOY_KEY="/etc/syslet/deploykey"

# Configure git to use the deploy key
export GIT_SSH_COMMAND="ssh -i $DEPLOY_KEY -o StrictHostKeyChecking=accept-new"

# Clone repo if not present, otherwise pull latest changes
if [ ! -d "$REPO_DIR/.git" ]; then
    mkdir -p "$(dirname "$REPO_DIR")"
    git clone "$REPO_URL" "$REPO_DIR"
fi

cd "$REPO_DIR"
git pull origin "$REPO_BRANCH"
syslet hosts/$(hostname)/
```

The last line applies specs from a per-host subdirectory (`hosts/<hostname>/`); see [Deploy from a directory](deploy-from-a-directory.md). Configure webhookd to call this script from your git host's webhook.

## 3. Wire up the webhook

Point your git repository's webhook at the webhookd endpoint that runs `deploy-syslet.sh`. Refer to the [webhookd documentation](https://github.com/ncarlier/webhookd) for how to expose and secure that endpoint.
