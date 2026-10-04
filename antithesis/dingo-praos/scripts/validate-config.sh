#!/usr/bin/env bash
set -euo pipefail

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

  grep -Eq 'CARDANO_PRIVATE_BIND_ADDR:[[:space:]]*"?0\.0\.0\.0"?' "$rendered"
  grep -Eq 'TXPUMP_STARTUP_TIMEOUT:[[:space:]]*"?0"?' "$rendered"
  grep -Eq 'TXPUMP_TYPES:[[:space:]]*"?payment,delegation,governance,plutus"?' "$rendered"
  if [[ "$moog" == "true" ]]; then
    grep -Eq 'TXPUMP_NODE_ADDR:[[:space:]]*"?p1\.example:3002"?' "$rendered"
    grep -Eq 'TXPUMP_FALLBACK_ADDR:[[:space:]]*"?p2\.example:3002"?' "$rendered"
    grep -Fq 'UNIX-CONNECT:/ipc/node.socket' "$rendered"
    [[ "$(grep -c 'com.antithesis.exclude_from_faults' "$rendered")" -eq 2 ]]
  else
    grep -Eq 'TXPUMP_NODE_ADDR:[[:space:]]*"?/ipc/dingo\.socket"?' "$rendered"
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
