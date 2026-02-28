#!/usr/bin/env bash
# Updates the docker-compose default version for a package.
# Usage: ./scripts/add-version.sh <package-name> <new-version>
# Example: ./scripts/add-version.sh cardano-node 10.6.2

set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "Usage: $0 <package-name> <new-version>"
  echo "Example: $0 cardano-node 10.6.2"
  exit 1
fi

PACKAGE_NAME="$1"
NEW_VERSION="$2"
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

COMPOSE_VAR=$(jq -r --arg pkg "$PACKAGE_NAME" '.[$pkg].compose_var // ""' "$UPSTREAM_CONFIG")
if [[ -z "$COMPOSE_VAR" || "$COMPOSE_VAR" == "null" ]]; then
  echo "ERROR: compose_var not set for $PACKAGE_NAME"
  exit 1
fi

OLD_VERSION=$(python3 - "$COMPOSE_FILE" "$COMPOSE_VAR" <<'PY' || echo ""
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

if [[ -z "$OLD_VERSION" ]]; then
  echo "ERROR: Could not find ${COMPOSE_VAR} default in $COMPOSE_FILE"
  exit 1
fi

if [[ "$OLD_VERSION" == "$NEW_VERSION" ]]; then
  echo "No change: ${COMPOSE_VAR} already set to ${NEW_VERSION}"
  exit 0
fi

if ! python3 - "$COMPOSE_FILE" "$COMPOSE_VAR" "$NEW_VERSION" <<'PY'
import re
import sys

compose_file = sys.argv[1]
compose_var = sys.argv[2]
new_version = sys.argv[3]

pattern = re.compile(r"(\$\{" + re.escape(compose_var) + r":-)([^}]+)(\})")
with open(compose_file, "r") as fh:
    data = fh.read()

if not pattern.search(data):
    print("compose var not found", file=sys.stderr)
    sys.exit(1)

updated = pattern.sub(r"\g<1>" + new_version + r"\g<3>", data, count=1)

with open(compose_file, "w") as fh:
    fh.write(updated)
PY
then
  echo "ERROR: Failed to update $COMPOSE_FILE for $PACKAGE_NAME"
  exit 1
fi

echo "Updated ${COMPOSE_VAR}: ${OLD_VERSION} -> ${NEW_VERSION}"
