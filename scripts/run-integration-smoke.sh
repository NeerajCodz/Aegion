#!/usr/bin/env bash
# Run a focused smoke slice of end-to-end tests.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

case "$(uname -s)" in
    MINGW*|MSYS*|CYGWIN*)
        echo "Skipping integration smoke tests on Windows."
        exit 0
        ;;
esac

GO_CMD="${GO_CMD:-go}"
if ! command -v "$GO_CMD" >/dev/null 2>&1; then
    if command -v go.exe >/dev/null 2>&1; then
        GO_CMD="go.exe"
    else
        echo "go toolchain is required for integration smoke tests"
        exit 1
    fi
fi
resolved_go_cmd="$(command -v "$GO_CMD" || true)"
if [[ "$resolved_go_cmd" == *.exe ]]; then
    case "$resolved_go_cmd" in
        /mnt/*|[A-Za-z]:\\*)
            GO_CMD="$(basename "$resolved_go_cmd")"
            ;;
    esac
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

if [[ -z "${AEGION_E2E_DATABASE_URL:-}" ]]; then
    echo "Skipping integration smoke tests because AEGION_E2E_DATABASE_URL is not set."
    exit 0
fi

"$GO_CMD" test -count=1 -v ./tests/e2e -run '^(TestLoginFlowInitiation|TestSessionValidation|TestAdminAuthentication)$' -timeout 15m
