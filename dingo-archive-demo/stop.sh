#!/usr/bin/env bash

# Copyright 2026 Blink Labs Software
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Tear down the archive-node demo and remove its volumes and bind-mount.
#
# Usage: ./stop.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/scripts/teardown.sh"

echo "Stopping archive-demo..."
status=0
docker compose -f "${SCRIPT_DIR}/docker-compose.yml" down -v || status=$?
if [[ "${status}" -eq 0 ]]; then
  archive_demo_wipe_tmp "${SCRIPT_DIR}/tmp" || status=$?
else
  echo "Compose teardown failed; preserving the pruning bind mount." >&2
fi
if [[ "${status}" -eq 0 ]]; then
  echo "Stopped."
else
  echo "Stop failed (exit ${status})." >&2
fi
exit "${status}"
