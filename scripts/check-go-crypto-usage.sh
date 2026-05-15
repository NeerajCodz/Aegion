#!/usr/bin/env bash
# Enforce Go-native crypto/JWT usage conventions across Go sources.
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
    echo "python3 (or compatible python) is required to validate Go crypto usage"
    exit 1
fi

"${PYTHON_CMD[@]}" - <<'PY'
from pathlib import Path
import sys

root = Path(".")
errors = []

excluded_parts = {
    ".git",
    "node_modules",
    "dist",
    "target",
    "rust",
}

forbidden_tokens = {
    'import "C"': "CGO is not allowed in shipped Go crypto/JWT paths",
    "#cgo": "CGO linker directives are not allowed in shipped Go crypto/JWT paths",
    "libaegion_crypto": "Rust crypto libraries must stay inactive",
    "libaegion_jwt": "Rust JWT libraries must stay inactive",
}

disallowed_imports = {
    "golang.org/x/crypto/bcrypt": "use internal/platform/bcryptcompat",
}

allowlisted_crypto_imports = {
    '"crypto/rand"': {
        "internal/platform/crypto/crypto.go",
        "internal/platform/jwt/jwt.go",
    },
    '"crypto/subtle"': {
        "internal/platform/crypto/crypto.go",
    },
    '"crypto/hmac"': {
        "internal/platform/crypto/crypto.go",
        "modules/mfa/service/service.go",
    },
    '"crypto/sha1"': {
        "modules/mfa/service/service.go",        # TOTP algorithm compatibility.
        "modules/password/service/service.go",   # HIBP k-anonymity protocol compatibility.
    },
    '"crypto/sha256"': {
        "internal/platform/crypto/crypto.go",
        "internal/platform/jwt/jwt.go",
        "cmd/aegion/server.go",                  # Deterministic local key derivation.
        "modules/admin/cmd/admin/main.go",       # Deterministic local key derivation.
        "modules/social/cmd/server/main.go",     # Deterministic local key derivation.
        "modules/sso/cmd/server/main.go",        # Deterministic local key derivation.
        "modules/oauth2/service/token/token.go", # OIDC/token spec hash claims.
        "modules/passkeys/service/service.go",   # WebAuthn signature digest.
        "modules/analytics/retention/archival.go", # Streaming archival checksum.
    },
}

for path in root.rglob("*.go"):
    if any(part in excluded_parts for part in path.parts):
        continue
    rel_path = path.as_posix()
    text = path.read_text(encoding="utf-8")
    for token, guidance in forbidden_tokens.items():
        if token in text:
            errors.append(f"{rel_path}: found forbidden token {token!r}; {guidance}")
    for token, guidance in disallowed_imports.items():
        if token in text:
            errors.append(f"{rel_path}: found forbidden import {token}; {guidance}")
    if path.name.endswith("_test.go"):
        continue
    for token, allowlist in allowlisted_crypto_imports.items():
        if token in text and rel_path not in allowlist:
            errors.append(
                f"{rel_path}: non-test import {token} is not allowlisted; "
                "use internal/platform/crypto or document this protocol requirement in the allowlist"
            )

required_markers = {
    Path("internal/platform/crypto/crypto.go"): "Package crypto provides Go-native",
    Path("internal/platform/jwt/jwt.go"): "Package jwt provides Go-native",
    Path("internal/platform/moduleserver/server.go"): "cryptoRuntimeSelfCheck",
}

for path, marker in required_markers.items():
    if not path.exists():
        errors.append(f"missing required file: {path}")
        continue
    text = path.read_text(encoding="utf-8")
    if marker not in text:
        errors.append(f"{path}: missing required Go-native crypto marker {marker!r}")

if errors:
    print("::error::Go crypto usage validation failed:")
    for issue in errors:
        print(f"  - {issue}")
    sys.exit(1)

print("Go crypto usage validation passed.")
PY
