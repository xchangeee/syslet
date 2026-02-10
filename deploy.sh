#!/usr/bin/env bash
# Deploy syslet specs to a remote host via SSH.
#
# Usage:
#   ./deploy.sh <host> <spec-dir>
#
# Example:
#   ./deploy.sh web01 hosts/web01/
#
# This zips all .json files from <spec-dir>, copies the zip to the remote
# host at /etc/syslet/config.zip, and runs syslet via SSH.
# Hosts are resolved through your SSH config.
set -euo pipefail

if [ $# -ne 2 ]; then
	echo "Usage: $0 <host> <spec-dir>" >&2
	exit 1
fi

host="$1"
spec_dir="$2"

if [ ! -d "$spec_dir" ]; then
	echo "Error: $spec_dir is not a directory" >&2
	exit 1
fi

# Create a temporary zip file, clean up on exit.
tmpzip=$(mktemp /tmp/syslet-XXXXXX.zip)
trap 'rm -f "$tmpzip"' EXIT

# Zip all .json files from the spec directory (flat, no directory structure).
zip -j "$tmpzip" "$spec_dir"/*.json

# Copy zip to remote host and run syslet.
ssh "$host" "mkdir -p /etc/syslet"
scp "$tmpzip" "$host:/etc/syslet/config.zip"
ssh "$host" "syslet /etc/syslet/config.zip"
