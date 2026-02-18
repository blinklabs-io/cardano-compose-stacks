#!/usr/bin/env bash
# Checks upstream image versions and updates docker-compose defaults.
# Usage: ./scripts/check-versions.sh
# Optional: DRY_RUN=1 to report changes without writing.

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

PACKAGES=$(jq -r 'keys[]' "$UPSTREAM_CONFIG")
UPDATED=0

for PACKAGE in $PACKAGES; do
  echo "=========================================="
  echo "Checking package: $PACKAGE"
  echo "=========================================="

  COMPOSE_VAR=$(jq -r --arg pkg "$PACKAGE" '.[$pkg].compose_var' "$UPSTREAM_CONFIG")
  GHCR_IMAGE=$(jq -r --arg pkg "$PACKAGE" '.[$pkg].ghcr_image // ""' "$UPSTREAM_CONFIG")
  DOCKER_IMAGE=$(jq -r --arg pkg "$PACKAGE" '.[$pkg].docker_image // ""' "$UPSTREAM_CONFIG")
  TAG_PREFIX=$(jq -r --arg pkg "$PACKAGE" '.[$pkg].tag_prefix // ""' "$UPSTREAM_CONFIG")
  TAG_REGEX=$(jq -r --arg pkg "$PACKAGE" '.[$pkg].tag_regex // ""' "$UPSTREAM_CONFIG")

  if [[ "$COMPOSE_VAR" == "null" || -z "$COMPOSE_VAR" ]]; then
    echo "  WARNING: compose_var not set for $PACKAGE, skipping"
    continue
  fi

  if [[ -z "$GHCR_IMAGE" && -z "$DOCKER_IMAGE" ]]; then
    echo "  WARNING: No image source defined for $PACKAGE, skipping"
    continue
  fi

  if [[ -z "$TAG_REGEX" ]]; then
    TAG_REGEX='^[0-9]+(\.[0-9]+){2,3}$'
  fi

  if [[ -n "$GHCR_IMAGE" ]]; then
    echo "  Using GHCR image: $GHCR_IMAGE"
    GHCR_ORG="${GHCR_IMAGE%%/*}"
    GHCR_PKG="${GHCR_IMAGE#*/}"

    UPSTREAM_TAG=$(gh api "/orgs/${GHCR_ORG}/packages/container/${GHCR_PKG}/versions" --paginate \
      --jq '.[].metadata.container.tags[]' 2>/dev/null \
      | grep -E "$TAG_REGEX" \
      | sort -V | tail -1 || echo "")

    if [[ -z "$UPSTREAM_TAG" ]]; then
      echo "  WARNING: Could not fetch tags from GHCR for $GHCR_IMAGE, skipping"
      continue
    fi
  else
    echo "  Using Docker Hub image: $DOCKER_IMAGE"

    UPSTREAM_TAG=$(python3 - "$DOCKER_IMAGE" "$TAG_REGEX" <<'PY' || echo ""
import json
import re
import sys
import urllib.request

image = sys.argv[1]
regex = re.compile(sys.argv[2])

page = 1
page_size = 100
found = []

while True:
    url = f"https://hub.docker.com/v2/repositories/{image}/tags?page={page}&page_size={page_size}"
    with urllib.request.urlopen(url) as resp:
        data = json.loads(resp.read().decode("utf-8"))

    for item in data.get("results", []):
        tag = item.get("name", "")
        if regex.match(tag):
            found.append(tag)

    if not data.get("next"):
        break
    page += 1

if not found:
    sys.exit(1)

def normalize(tag):
    if tag.startswith("v"):
        tag = tag[1:]
    parts = re.split(r"[.-]", tag)
    key = []
    for part in parts:
        if part.isdigit():
            key.append(int(part))
        else:
            key.append(part)
    return key

found.sort(key=normalize)
print(found[-1])
PY
)

    if [[ -z "$UPSTREAM_TAG" ]]; then
      echo "  WARNING: Could not fetch tags from Docker Hub for $DOCKER_IMAGE, skipping"
      continue
    fi
  fi

  # Construct full image reference for verification
  if [[ -n "$GHCR_IMAGE" ]]; then
    IMAGE_REF="ghcr.io/${GHCR_IMAGE}:${UPSTREAM_TAG}"
  else
    IMAGE_REF="docker.io/${DOCKER_IMAGE}:${UPSTREAM_TAG}"
  fi

  if [[ -n "$TAG_PREFIX" && "$UPSTREAM_TAG" == "$TAG_PREFIX"* ]]; then
    UPSTREAM_VERSION="${UPSTREAM_TAG#${TAG_PREFIX}}"
  else
    UPSTREAM_VERSION="$UPSTREAM_TAG"
  fi

  echo "  Upstream tag: $UPSTREAM_TAG"
  echo "  Upstream version: $UPSTREAM_VERSION"

  LOCAL_VERSION=$(python3 - "$COMPOSE_FILE" "$COMPOSE_VAR" <<'PY' || echo ""
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

  if [[ -z "$LOCAL_VERSION" ]]; then
    echo "  WARNING: Could not find ${COMPOSE_VAR} default in $COMPOSE_FILE, skipping"
    continue
  fi

  echo "  Local version: $LOCAL_VERSION"

  if [[ "$UPSTREAM_VERSION" == "$LOCAL_VERSION" ]]; then
    echo "  Already up to date"
    continue
  fi

  echo "  New version available: $UPSTREAM_VERSION"

  # Verify image exists before updating
  echo "  Verifying image: $IMAGE_REF"
  if ! docker manifest inspect "$IMAGE_REF" > /dev/null 2>&1; then
    echo "  WARNING: Image not found or not accessible: $IMAGE_REF, skipping"
    continue
  fi

  if [[ -n "${DRY_RUN:-}" ]]; then
    echo "  DRY RUN: would update ${COMPOSE_VAR} to ${UPSTREAM_VERSION}"
    continue
  fi

  if ! ./scripts/add-version.sh "$PACKAGE" "$UPSTREAM_VERSION"; then
    echo "  WARNING: Failed to update $COMPOSE_FILE for $PACKAGE, skipping"
    continue
  fi

  echo "  Updated ${COMPOSE_VAR} -> ${UPSTREAM_VERSION}"
  UPDATED=$((UPDATED + 1))

done

echo ""
echo "=========================================="
if [[ "$UPDATED" -gt 0 ]]; then
  echo "Updated $UPDATED package(s) in $COMPOSE_FILE"
else
  echo "No updates applied"
fi
echo "=========================================="
