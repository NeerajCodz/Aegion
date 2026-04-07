#!/usr/bin/env bash
# Regenerates protobuf outputs and fails if generated files are stale.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

echo "Regenerating protobuf stubs..."
bash ./scripts/gen-proto.sh

echo "Validating generated protobuf outputs..."
if ! git diff --quiet -- internal/proto; then
    echo "::error::Generated protobuf stubs are stale. Run ./scripts/gen-proto.sh and commit the changes."
    git --no-pager diff -- internal/proto
    exit 1
fi

UNTRACKED_GENERATED="$(git ls-files --others --exclude-standard -- internal/proto)"
if [[ -n "$UNTRACKED_GENERATED" ]]; then
    echo "::error::Untracked generated protobuf files detected under internal/proto:"
    printf '%s\n' "$UNTRACKED_GENERATED"
    exit 1
fi

echo "Proto generation is consistent."
