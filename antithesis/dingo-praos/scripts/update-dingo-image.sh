#!/usr/bin/env bash
set -euo pipefail

if [[ "$#" -ne 1 ]]; then
    echo "usage: $0 <compose-file>" >&2
    exit 1
fi

for command in docker jq; do
    if ! command -v "$command" >/dev/null; then
        echo "$command is required to resolve the Dingo image" >&2
        exit 1
    fi
done

IMAGE="ghcr.io/blinklabs-io/dingo:main-antithesis"
COMPOSE_FILE="$1"
if [[ ! -f "$COMPOSE_FILE" ]]; then
    echo "compose file not found: ${COMPOSE_FILE}" >&2
    exit 1
fi

manifest_json="$(
    docker buildx imagetools inspect "$IMAGE" --format '{{json .Manifest}}'
)"
image_json="$(
    docker buildx imagetools inspect "$IMAGE" --format '{{json .Image}}'
)"

digest="$(
    jq -er '.digest | select(test("^sha256:[0-9a-f]{64}$"))' \
        <<<"$manifest_json"
)"
revision="$(
    jq -er '
        [
            .["linux/amd64"].config.Labels["org.opencontainers.image.revision"],
            .["linux/arm64"].config.Labels["org.opencontainers.image.revision"]
        ] as $revisions
        | if all($revisions[];
            type == "string" and test("^[0-9a-f]{40}$")
          ) and ($revisions | unique | length) == 1
          then $revisions[0]
          else error("platforms do not identify one source revision")
          end
    ' <<<"$image_json"
)"
pinned_image="ghcr.io/blinklabs-io/dingo@${digest}"

sed -i -E \
    -e "/^[[:space:]]+# Source: blinklabs-io\/dingo@[0-9a-f]{40}$/d" \
    -e "s~^    image: ghcr.io/blinklabs-io/dingo(:main-antithesis|@sha256:[0-9a-f]{64})$~    # Source: blinklabs-io/dingo@${revision}\n    image: ${pinned_image}~" \
    "$COMPOSE_FILE"

[[ "$(grep -Fxc "    # Source: blinklabs-io/dingo@${revision}" "$COMPOSE_FILE")" -eq 1 ]]
[[ "$(grep -Fxc "    image: ${pinned_image}" "$COMPOSE_FILE")" -eq 1 ]]

echo "Pinned Dingo Antithesis image to ${revision} (${digest})"
