#!/usr/bin/env bash
# Ensure a module only imports shared/internal packages and itself.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

MODULE="${1:-}"
if [[ -z "$MODULE" ]]; then
    echo "Usage: $0 <module>"
    exit 1
fi

MODULE_DIR="modules/$MODULE"
if [[ ! -d "$MODULE_DIR" ]]; then
    echo "Error: Module directory not found: $MODULE_DIR"
    exit 1
fi

GO_CMD="${GO_CMD:-go}"
if ! command -v "$GO_CMD" >/dev/null 2>&1; then
    if command -v go.exe >/dev/null 2>&1; then
        GO_CMD="go.exe"
    else
        echo "go toolchain is required for module boundary checks"
        exit 1
    fi
fi

if ! "$GO_CMD" list "./${MODULE_DIR}/..." >/dev/null 2>&1; then
    echo "No Go packages detected under $MODULE_DIR, skipping boundary check."
    exit 0
fi

IMPORTS="$(
    "$GO_CMD" list -f '{{join .Imports "\n"}}' "./${MODULE_DIR}/..." | grep '^github.com/aegion/aegion/modules/' | sort -u || true
)"

if [[ -z "$IMPORTS" ]]; then
    echo "No cross-module imports detected in $MODULE_DIR."
    exit 0
fi

VIOLATIONS="$(
    printf '%s\n' "$IMPORTS" | grep -Ev "^github.com/aegion/aegion/modules/${MODULE}(/|$)" || true
)"

# Modules that are allowed to have cross-module imports
# admin: manages configuration of all other modules
# introspection: needs access to OAuth2 implementation details for schema generation
ALLOWED_CROSS_MODULE_MODULES="admin introspection"

if [[ " $ALLOWED_CROSS_MODULE_MODULES " == *" $MODULE "* ]]; then
    echo "Module $MODULE is allowed to have cross-module imports (management/introspection plane)."
    echo "Cross-module imports detected:"
    printf '%s\n' "$VIOLATIONS"
    exit 0
fi

if [[ -n "$VIOLATIONS" ]]; then
    echo "Cross-module import violations found in $MODULE_DIR:"
    printf '%s\n' "$VIOLATIONS"
    exit 1
fi

echo "Module boundary check passed for $MODULE."
