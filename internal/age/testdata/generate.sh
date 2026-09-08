#!/usr/bin/env bash
# Regenerates test fixtures for internal/age tests.
# Run from the testdata directory: ./generate.sh
set -euo pipefail

ssh-keygen -t ed25519 -f id_ed25519 -N "" -q
echo "Done."
