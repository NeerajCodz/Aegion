#!/usr/bin/env bash
# Enforce Rust-backed crypto/JWT usage conventions across Go sources.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

if command -v python3 >/dev/null 2>&1; then
    PYTHON_CMD=(python3)
elif command -v python >/dev/null 2>&1; then
    PYTHON_CMD=(python)
elif command -v py >/dev/null 2>&1; then
    PYTHON_CMD=(py -3)
else
    echo "python3 (or compatible python) is required to validate Rust FFI usage"
    exit 1
fi

"${PYTHON_CMD[@]}" - <<'PY'
from pathlib import Path
import sys

root = Path(".")
errors = []

excluded_parts = {
    ".git",
    "rust",
    "node_modules",
    "dist",
    "target",
}

disallowed_imports = {
    "golang.org/x/crypto/bcrypt": "use internal/platform/bcryptcompat (Rust-backed)",
    '"crypto/subtle"': "use internal/platform/crypto.ConstantTimeCompare",
}

disallowed_non_test_imports = {
    '"crypto/rand"': "use internal/platform/crypto random helpers (Rust-backed RNG)",
}

allowlisted_non_test_imports = {
    '"crypto/hmac"': {
        "modules/mfa/service/service.go",
    },
    '"crypto/sha1"': {
        "modules/mfa/service/service.go",        # TOTP algorithm compatibility.
        "modules/password/service/service.go",   # HIBP k-anonymity protocol compatibility.
    },
    '"crypto/sha256"': {
        "cmd/aegion/server.go",                  # Deterministic local key derivation.
        "modules/admin/cmd/admin/main.go",       # Deterministic local key derivation.
        "modules/social/cmd/server/main.go",     # Deterministic local key derivation.
        "modules/sso/cmd/server/main.go",        # Deterministic local key derivation.
        "modules/oauth2/service/token/token.go", # OIDC/token spec hash claims.
        "modules/passkeys/service/service.go",   # WebAuthn signature digest.
        "modules/analytics/retention/archival.go", # Streaming archival data-integrity checksum.
    },
}

for path in root.rglob("*.go"):
    if any(part in excluded_parts for part in path.parts):
        continue
    rel_path = path.as_posix()
    text = path.read_text(encoding="utf-8")
    for token, guidance in disallowed_imports.items():
        if token in text:
            errors.append(f"{rel_path}: found forbidden import {token}; {guidance}")
    if path.name.endswith("_test.go"):
        continue
    for token, guidance in disallowed_non_test_imports.items():
        if token in text:
            errors.append(f"{rel_path}: found forbidden non-test import {token}; {guidance}")
    for token, allowlist in allowlisted_non_test_imports.items():
        if token in text and rel_path not in allowlist:
            errors.append(
                f"{rel_path}: non-test import {token} is not allowlisted; "
                "either use Rust-backed helpers or explicitly document and allowlist this protocol requirement"
            )

required_checks = {
    Path("internal/platform/moduleserver/server.go"): "rustRuntimeSelfCheck",
    Path("modules/admin/cmd/admin/main.go"): "rustSelfCheck",
    Path("modules/oauth2/cmd/server/main.go"): "rustSelfCheckHook",
}

for path, marker in required_checks.items():
    if not path.exists():
        errors.append(f"missing required file: {path}")
        continue
    text = path.read_text(encoding="utf-8")
    if marker not in text:
        errors.append(f"{path}: missing required Rust runtime marker '{marker}'")

if errors:
    print("::error::Rust FFI usage validation failed:")
    for issue in errors:
        print(f"  - {issue}")
    sys.exit(1)

print("Rust FFI usage validation passed.")
PY
