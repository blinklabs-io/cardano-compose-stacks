#!/usr/bin/env bash

archive_demo_make_readable() {
  local data_dir="$1"
  docker run --rm --user 0 -v "${data_dir}:/data" alpine:latest \
    sh -c 'chmod -R a+rwX /data'
}

archive_demo_wait_for_lock_release() {
  local lock_file="$1"
  local timeout="${2:-30}"
  local deadline=$((SECONDS + timeout))
  while [[ -e "${lock_file}" ]]; do
    if ((SECONDS >= deadline)); then
      return 1
    fi
    sleep 0.1
  done
}

archive_demo_wipe_tmp() {
  local tmp_dir="$1"
  local status=0
  [[ -d "${tmp_dir}" ]] || return 0
  docker run --rm --user 0 -v "${tmp_dir}:/cleanup" alpine:latest \
    sh -c 'rm -rf /cleanup/* /cleanup/.[!.]* 2>/dev/null || true' \
    >/dev/null 2>&1 || status=$?
  rm -rf "${tmp_dir}" 2>/dev/null || status=$?
  return "${status}"
}
