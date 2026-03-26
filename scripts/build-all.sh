#!/bin/bash
# Build all module images
set -e

MODULES=(password magic_link admin)
VERSION=${VERSION:-latest}

echo "Building all module images (version: $VERSION)"

for module in "${MODULES[@]}"; do
    echo "Building aegion-$module..."
    ./scripts/build-module.sh "$module" "$VERSION"
done

echo "All modules built successfully!"
