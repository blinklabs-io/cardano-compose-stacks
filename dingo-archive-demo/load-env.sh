#!/usr/bin/env bash

load_env_defaults() {
  local env_file="$1"
  local name line
  declare -A inherited=()

  [[ -f "${env_file}" ]] || return 0
  while IFS= read -r line || [[ -n "${line}" ]]; do
    if [[ "${line}" =~ ^[[:space:]]*(export[[:space:]]+)?([A-Za-z_][A-Za-z0-9_]*)[[:space:]]*= ]]; then
      name="${BASH_REMATCH[2]}"
      if [[ -v "${name}" ]]; then
        inherited["${name}"]="${!name}"
      fi
    fi
  done < "${env_file}"

  set -a
  # shellcheck disable=SC1090
  source "${env_file}"
  set +a
  for name in "${!inherited[@]}"; do
    export "${name}=${inherited[${name}]}"
  done
}
