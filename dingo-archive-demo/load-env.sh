#!/usr/bin/env bash

load_env_defaults() {
  local env_file="$1"
  local name line all_export=false i inherited_count=0
  local -a inherited_names
  local -a inherited_values

  [[ -f "${env_file}" ]] || return 0
  case "$-" in
    *a*) all_export=true ;;
  esac
  while IFS= read -r line || [[ -n "${line}" ]]; do
    if [[ "${line}" =~ ^[[:space:]]*(export[[:space:]]+)?([A-Za-z_][A-Za-z0-9_]*)[[:space:]]*= ]]; then
      name="${BASH_REMATCH[2]}"
      if declare -p "${name}" >/dev/null 2>&1; then
        inherited_names[${inherited_count}]="$name"
        inherited_values[${inherited_count}]="${!name}"
        inherited_count=$((inherited_count + 1))
      fi
    fi
  done < "${env_file}"

  set -a
  # shellcheck disable=SC1090
  source "${env_file}"
  if [[ "${all_export}" != true ]]; then
    set +a
  fi
  for ((i = 0; i < inherited_count; i++)); do
    name="${inherited_names[i]}"
    export "${name}=${inherited_values[i]}"
  done
}
