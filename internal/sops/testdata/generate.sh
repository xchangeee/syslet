#!/usr/bin/env bash
# Regenerates test fixtures for internal/sops tests.
# Run from the testdata directory: ./generate.sh
set -euo pipefail

# Fixed age key for reproducible tests (generated once via age-keygen).
# Re-running this script overwrites all fixtures with the same key.

KEY_FILE="keys.txt"
WRONG_KEY_FILE="wrong_keys.txt"
ENCRYPTED_FILE="encrypted.yaml"
NESTED_FILE="encrypted_nested.yaml"

# Generate primary key
AGE_KEY_OUTPUT=$(age-keygen 2>&1)
PRIV=$(echo "$AGE_KEY_OUTPUT" | grep "AGE-SECRET-KEY")
PUB=$(echo "$AGE_KEY_OUTPUT" | grep "^# public key:" | awk '{print $4}')

echo "$PRIV" > "$KEY_FILE"
chmod 600 "$KEY_FILE"

# Generate a second (wrong) key that cannot decrypt the fixture
AGE_WRONG_OUTPUT=$(age-keygen 2>&1)
WRONG_PRIV=$(echo "$AGE_WRONG_OUTPUT" | grep "AGE-SECRET-KEY")
echo "$WRONG_PRIV" > "$WRONG_KEY_FILE"
chmod 600 "$WRONG_KEY_FILE"

# Flat YAML fixture
printf 'db_password: hunter2\napi_key: s3cr3t\n' | \
  sops --encrypt --input-type yaml --output-type yaml --age "$PUB" /dev/stdin > "$ENCRYPTED_FILE"

# Nested YAML fixture (sops encrypts it fine; our Decrypt should reject it)
printf 'nested:\n  key: value\n' | \
  sops --encrypt --input-type yaml --output-type yaml --age "$PUB" /dev/stdin > "$NESTED_FILE"

echo "Public key (embed in tests as expectedPub): $PUB"
echo "Done."
