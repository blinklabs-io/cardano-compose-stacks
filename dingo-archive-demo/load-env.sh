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

archive_demo_minio_console_host() {
  local bind_address="${ARCHIVEDEMO_MINIO_BIND_ADDRESS:-127.0.0.1}"
  local host
  case "${bind_address}" in
    127.0.0.1|localhost)
      printf 'localhost\n'
      ;;
    0.0.0.0)
      host="${ARCHIVEDEMO_HOST:-}"
      if [[ -z "${host}" ]] && command -v ip >/dev/null 2>&1; then
        host="$(ip route get 1.1.1.1 2>/dev/null | awk '{print $7; exit}' || true)"
      fi
      printf '%s\n' "${host:-localhost}"
      ;;
    *)
      printf '%s\n' "${bind_address}"
      ;;
  esac
}

archive_demo_validate_minio_binding() {
  local bind_address="${ARCHIVEDEMO_MINIO_BIND_ADDRESS:-127.0.0.1}"
  local user="${ARCHIVEDEMO_MINIO_ROOT_USER:-demo}"
  local password="${ARCHIVEDEMO_MINIO_ROOT_PASSWORD:-demodemo}"
  case "${bind_address}" in
    127.*|localhost|::1)
      return 0
      ;;
  esac
  if [[ "${user}" == demo || "${password}" == demodemo ]]; then
    echo "MinIO needs non-default credentials when bound outside loopback." >&2
    return 1
  fi
  return 0
}
