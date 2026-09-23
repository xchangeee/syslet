#!/bin/bash
set -euo pipefail

REPO_URL="git@git.example.com:you/infra.git"
REPO_DIR="/var/lib/webhookd/infra"
SPEC_FILE="/var/lib/webhookd/spec.json"

export GIT_SSH_COMMAND="ssh -i /etc/webhookd/deploykey -o StrictHostKeyChecking=accept-new"
export CUE_CACHE_DIR="/var/cache/webhookd/cue"

if [ ! -d "$REPO_DIR/.git" ]; then
	git clone "$REPO_URL" "$REPO_DIR"
fi

# Match the remote exactly, the checkout is never edited on the server
cd "$REPO_DIR"
git fetch origin main
git reset --hard origin/main

# Export to a file first, so a CUE error stops the script before syslet runs
cue export -e syslet.specRendered --out text > "$SPEC_FILE"
syslet --stdin < "$SPEC_FILE"
