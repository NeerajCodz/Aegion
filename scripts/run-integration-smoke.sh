#!/usr/bin/env bash
# Run a focused smoke slice of end-to-end tests.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

GO_CMD="${GO_CMD:-go}"
if ! command -v "$GO_CMD" >/dev/null 2>&1; then
    if command -v go.exe >/dev/null 2>&1; then
        GO_CMD="go.exe"
    else
        echo "go toolchain is required for integration smoke tests"
        exit 1
    fi
fi

if [[ "$("$GO_CMD" env GOOS)" == "windows" ]]; then
    echo "Skipping integration smoke tests on Windows."
    exit 0
fi

if ! command -v docker >/dev/null 2>&1; then
    if [[ "${CI:-}" == "true" ]]; then
        echo "docker is required for integration smoke tests in CI."
        exit 1
    fi
    echo "Skipping integration smoke tests because docker is unavailable."
    exit 0
fi

"$GO_CMD" test -count=1 -v ./tests/e2e -run '^(TestLoginFlowInitiation|TestSessionValidation|TestAdminAuthentication)$' -timeout 15m
