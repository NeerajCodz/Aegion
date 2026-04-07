#!/usr/bin/env bash
# Regenerates protobuf outputs and fails if generated files are stale.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

echo "Regenerating protobuf stubs..."
bash ./scripts/gen-proto.sh

echo "Validating generated protobuf outputs..."
if ! git diff --quiet -- internal/proto; then
    PROTO_DIFF="$(git --no-pager diff --unified=0 -- internal/proto || true)"
    CHANGED_LINES="$(
        printf '%s\n' "$PROTO_DIFF" | grep -E '^[+-]' | grep -Ev '^\+\+\+|^---' || true
    )"
    IGNORABLE_VERSION_RE='^[+-]//[[:space:]-]*(protoc|protoc-gen-go|protoc-gen-go-grpc)[[:space:]]+v?[0-9][0-9A-Za-z.+-]*$'
    MATERIAL_CHANGES="$(printf '%s\n' "$CHANGED_LINES" | grep -Ev "$IGNORABLE_VERSION_RE" || true)"

    if [[ -z "$MATERIAL_CHANGES" && -n "$CHANGED_LINES" ]]; then
        echo "Only protobuf generator version metadata changed; ignoring those differences."
        if ! git restore --worktree --source=HEAD -- internal/proto 2>/dev/null; then
            git checkout -- internal/proto
        fi
    else
        echo "::error::Generated protobuf stubs are stale. Run ./scripts/gen-proto.sh and commit the changes."
        printf '%s\n' "$PROTO_DIFF"
        exit 1
    fi
fi

UNTRACKED_GENERATED="$(git ls-files --others --exclude-standard -- internal/proto)"
if [[ -n "$UNTRACKED_GENERATED" ]]; then
    echo "::error::Untracked generated protobuf files detected under internal/proto:"
    printf '%s\n' "$UNTRACKED_GENERATED"
    exit 1
fi

echo "Proto generation is consistent."
