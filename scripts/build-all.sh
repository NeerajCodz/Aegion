#!/usr/bin/env bash
# Build all module images
set -euo pipefail

VERSION="${VERSION:-latest}"

if [[ -n "${MODULES:-}" ]]; then
    read -r -a MODULE_LIST <<< "${MODULES}"
else
    mapfile -t MODULE_LIST < <(
        find modules -mindepth 2 -maxdepth 2 -type f -name "Dockerfile" | sed -E 's#^modules/([^/]+)/Dockerfile$#\1#' | sort
    )
fi

if [[ "${#MODULE_LIST[@]}" -eq 0 ]]; then
    echo "No module Dockerfiles found under modules/"
    exit 1
fi

echo "Building module images (version: $VERSION)"
echo "Modules: ${MODULE_LIST[*]}"

for module in "${MODULE_LIST[@]}"; do
    echo "Building aegion-${module//_/-}..."
    ./scripts/build-module.sh "$module" "$VERSION"
done

echo "All module images built successfully!"
