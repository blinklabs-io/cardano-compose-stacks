#!/usr/bin/env bash
set -euo pipefail

if ! command -v jq >/dev/null; then
  echo "jq is required to validate Moog image constraints" >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
TMP_CONFIG_DIR="$(mktemp -d "${TMPDIR:-/tmp}/antithesis-config.XXXXXX")"
trap 'rm -rf "$TMP_CONFIG_DIR"' EXIT

validate_compose() {
  local compose_file="$1"
  local moog="$2"
  local rendered="${TMP_CONFIG_DIR}/rendered-${moog}"
  if [[ "$moog" == "true" ]]; then
    grep -Eq '^[[:space:]]+internal: \$\{INTERNAL_NETWORK\}$' "$compose_file"
  fi
  INTERNAL_NETWORK="${INTERNAL_NETWORK:-false}" \
    docker compose -f "$compose_file" config >"$rendered"

  if [[ "$moog" == "true" ]]; then
    local rendered_json="${TMP_CONFIG_DIR}/rendered-moog.json"
    INTERNAL_NETWORK="${INTERNAL_NETWORK:-false}" \
      docker compose -f "$compose_file" config --format json >"$rendered_json"
    jq -e '.services | to_entries | all(.[];
      .value.platform == "linux/amd64" and .value.pull_policy == "never")' \
      "$rendered_json" >/dev/null || {
      echo "Moog services must use preloaded linux/amd64 images without registry pulls" >&2
      return 1
    }
  fi

  grep -Eq 'CARDANO_PRIVATE_BIND_ADDR:[[:space:]]*"?0\.0\.0\.0"?' "$rendered"
  grep -Eq 'TXPUMP_STARTUP_TIMEOUT:[[:space:]]*"?0"?' "$rendered"
  grep -Eq 'TXPUMP_TYPES:[[:space:]]*"?payment,peer_payment,delegation,governance,plutus,escrow,vesting"?$' "$rendered"
  grep -Eq 'TXPUMP_TYPES:[[:space:]]*"?payment,peer_payment,delegation,governance"?$' "$rendered"
  grep -Eq 'TXPUMP_USERS:[[:space:]]*"?0-2"?$' "$rendered"
  grep -Eq 'TXPUMP_USERS:[[:space:]]*"?3-4"?$' "$rendered"
  grep -Eq 'TXPUMP_LOG_DIR:[[:space:]]*"?/logs/txpump-cn"?' "$rendered"
  if [[ "$moog" == "true" ]]; then
    grep -Eq 'TXPUMP_NODE_ADDR:[[:space:]]*"?p1\.example:3002"?' "$rendered"
    grep -Eq 'TXPUMP_FALLBACK_ADDR:[[:space:]]*"?p2\.example:3002"?' "$rendered"
    [[ "$(grep -Ec 'TXPUMP_NODE_ADDR:[[:space:]]*"?p2\.example:3002"?' "$rendered")" -eq 1 ]]
    grep -Eq 'TXPUMP_FALLBACK_ADDR:[[:space:]]*"?p1\.example:3002"?' "$rendered"
    grep -Fq 'UNIX-CONNECT:/ipc/node.socket' "$rendered"
    [[ "$(grep -c 'com.antithesis.exclude_from_faults' "$rendered")" -eq 3 ]]
  else
    grep -Eq 'TXPUMP_NODE_ADDR:[[:space:]]*"?/ipc/dingo\.socket"?' "$rendered"
    grep -Eq 'TXPUMP_NODE_ADDR:[[:space:]]*"?/ipc/node\.socket"?' "$rendered"
    grep -Eq 'TXPUMP_FALLBACK_ADDR:[[:space:]]*"?/ipc-backup/node\.socket"?' "$rendered"
    grep -Eq 'TXPUMP_FALLBACK_ADDR:[[:space:]]*"?/ipc-backup/dingo\.socket"?' "$rendered"
  fi
  if [[ "$moog" == "true" ]]; then
    for pool in 1 2 3 4 5; do
      grep -Fq "/logs/p${pool}.log" "$rendered"
    done
  fi
}

validate_compose "${SCRIPT_DIR}/../docker-compose.yaml" false
validate_compose "${SCRIPT_DIR}/../testnets/dingo-praos/docker-compose.yaml" true

echo "Antithesis compose invariants are valid"
