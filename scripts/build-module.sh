#!/usr/bin/env bash
# Build a single module image
set -euo pipefail

MODULE="${1:-}"
VERSION="${2:-latest}"

if [[ -z "$MODULE" ]]; then
    echo "Usage: $0 <module> [version]"
    echo "Example: $0 password 2.1.0"
    exit 1
fi

MODULE_DIR="modules/$MODULE"
if [[ ! -d "$MODULE_DIR" ]]; then
    echo "Error: Module directory not found: $MODULE_DIR"
    exit 1
fi

DOCKERFILE_PATH="$MODULE_DIR/Dockerfile"
if [[ ! -f "$DOCKERFILE_PATH" ]]; then
    echo "Error: Dockerfile not found for module '$MODULE' ($DOCKERFILE_PATH)"
    exit 1
fi

IMAGE_MODULE="${MODULE//_/-}"
IMAGE_NAME="aegion/aegion-$IMAGE_MODULE:$VERSION"

echo "Building $IMAGE_NAME..."
docker build -f "$DOCKERFILE_PATH" -t "$IMAGE_NAME" --build-arg VERSION="$VERSION" .

echo "Successfully built $IMAGE_NAME"
