#!/usr/bin/env bash
# Regenerates test fixtures for internal/syslet secret integration tests.
# Run from the testdata directory: ./generate.sh
#
# Keys use [a-z0-9-] names only (the syslet validation rule).
# Values: db-password=hunter2, api-key=s3cr3t
set -euo pipefail

KEY_FILE="keys.txt"
WRONG_KEY_FILE="wrong_keys.txt"
ENCRYPTED_FILE="encrypted.yaml"

# Generate primary age key
AGE_KEY_OUTPUT=$(age-keygen 2>&1)
PRIV=$(echo "$AGE_KEY_OUTPUT" | grep "AGE-SECRET-KEY")
PUB=$(echo "$AGE_KEY_OUTPUT" | grep "^# public key:" | awk '{print $4}')

echo "$PRIV" > "$KEY_FILE"
chmod 600 "$KEY_FILE"

# Generate a second (wrong) key that cannot decrypt the fixture
WRONG_PRIV=$(age-keygen 2>&1 | grep "AGE-SECRET-KEY")
echo "$WRONG_PRIV" > "$WRONG_KEY_FILE"
chmod 600 "$WRONG_KEY_FILE"

# Flat YAML fixture with valid key names (only [a-z0-9-])
printf 'db-password: hunter2\napi-key: s3cr3t\n' | \
  sops --encrypt --input-type yaml --output-type yaml --age "$PUB" /dev/stdin > "$ENCRYPTED_FILE"

echo "Public key: $PUB"
echo "Done."
