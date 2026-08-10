#!/usr/bin/env bash
# Build exactly the separately deployed module images. Embedded modules belong to
# the core image and must never be discovered or built as standalone artifacts.
set -euo pipefail

VERSION="${VERSION:-latest}"
EXTERNAL_MODULES=(admin analytics cli introspection mfa oauth2 passkeys proxy social sso)

if [[ -n "${MODULES:-}" ]]; then
    read -r -a MODULE_LIST <<< "${MODULES}"
else
    MODULE_LIST=("${EXTERNAL_MODULES[@]}")
fi

for module in "${MODULE_LIST[@]}"; do
    supported=false
    for declared in "${EXTERNAL_MODULES[@]}"; do
        if [[ "$module" == "$declared" ]]; then
            supported=true
            break
        fi
    done
    if [[ "$supported" != true ]]; then
        echo "Refusing to build undeclared or embedded module image: $module" >&2
        exit 1
    fi
    if [[ ! -f "modules/$module/Dockerfile" ]]; then
        echo "Dockerfile artifact is missing for declared external module: $module" >&2
        exit 1
    fi
done

echo "Building external module images (version: $VERSION)"
echo "Modules: ${MODULE_LIST[*]}"
for module in "${MODULE_LIST[@]}"; do
    ./scripts/build-module.sh "$module" "$VERSION"
done
