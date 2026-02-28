#!/usr/bin/env bash
# Validates compose version defaults for all configured packages.

set -euo pipefail

UPSTREAM_CONFIG="scripts/upstream-versions.json"
COMPOSE_FILE="docker-compose.yaml"

if [[ ! -f "$UPSTREAM_CONFIG" ]]; then
  echo "ERROR: Upstream config not found: $UPSTREAM_CONFIG"
  exit 1
fi

if [[ ! -f "$COMPOSE_FILE" ]]; then
  echo "ERROR: Compose file not found: $COMPOSE_FILE"
  exit 1
fi

ERRORS=0
CHECKED=0

PACKAGES=$(jq -r 'keys[]' "$UPSTREAM_CONFIG")

for PACKAGE in $PACKAGES; do
  COMPOSE_VAR=$(jq -r --arg pkg "$PACKAGE" '.[$pkg].compose_var // ""' "$UPSTREAM_CONFIG")
  CHECKED=$((CHECKED + 1))

  if [[ -z "$COMPOSE_VAR" || "$COMPOSE_VAR" == "null" ]]; then
    echo "ERROR: compose_var not set for $PACKAGE"
    ERRORS=$((ERRORS + 1))
    continue
  fi

  PATTERN='${'"${COMPOSE_VAR}"':-'
  if ! grep -qF "$PATTERN" "$COMPOSE_FILE"; then
    echo "ERROR: ${COMPOSE_VAR} not found in $COMPOSE_FILE"
    ERRORS=$((ERRORS + 1))
    continue
  fi

  DEFAULT_VERSION=$(python3 - "$COMPOSE_FILE" "$COMPOSE_VAR" <<'PY' || echo ""
import re
import sys

compose_file = sys.argv[1]
compose_var = sys.argv[2]

pattern = re.compile(r"\$\{" + re.escape(compose_var) + r":-([^}]+)\}")
with open(compose_file, "r") as fh:
    data = fh.read()

match = pattern.search(data)
if not match:
    sys.exit(1)

print(match.group(1))
PY
)

  if [[ -z "$DEFAULT_VERSION" ]]; then
    echo "ERROR: ${COMPOSE_VAR} has no default version"
    ERRORS=$((ERRORS + 1))
    continue
  fi

done

echo ""
echo "Checked $CHECKED package(s)"

if [[ $ERRORS -gt 0 ]]; then
  echo "Found $ERRORS error(s)"
  exit 1
fi

echo "All versions valid"
