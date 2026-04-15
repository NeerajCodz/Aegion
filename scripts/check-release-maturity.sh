#!/usr/bin/env bash
# Validate build/release-maturity.json structure and module coverage.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

MATRIX_PATH="${1:-build/release-maturity.json}"

if command -v python3 >/dev/null 2>&1; then
    PYTHON_CMD=(python3)
elif command -v python >/dev/null 2>&1; then
    PYTHON_CMD=(python)
elif command -v py >/dev/null 2>&1; then
    PYTHON_CMD=(py -3)
else
    echo "python3 (or compatible python) is required to validate the release maturity matrix"
    exit 1
fi

"${PYTHON_CMD[@]}" - "$MATRIX_PATH" <<'PY'
import json
import sys
from pathlib import Path

matrix_path = Path(sys.argv[1])
errors = []

if not matrix_path.exists():
    print(f"::error::{matrix_path} does not exist")
    sys.exit(1)

try:
    payload = json.loads(matrix_path.read_text(encoding="utf-8"))
except json.JSONDecodeError as exc:
    print(f"::error::Invalid JSON in {matrix_path}: {exc}")
    sys.exit(1)

if not isinstance(payload, dict):
    print("::error::Release maturity matrix must be a JSON object")
    sys.exit(1)

modules = payload.get("modules")
if not isinstance(modules, dict) or not modules:
    errors.append("modules must be a non-empty object")
    modules = {}

expected_modules = {"core"}
modules_dir = Path("modules")
if modules_dir.exists():
    expected_modules.update(p.name for p in modules_dir.iterdir() if p.is_dir())

actual_modules = set(modules.keys())
missing = sorted(expected_modules - actual_modules)
extra = sorted(actual_modules - expected_modules)
if missing:
    errors.append(f"missing module entries: {', '.join(missing)}")
if extra:
    errors.append(f"unexpected module entries: {', '.join(extra)}")

allowed_statuses = {"GA", "Beta", "Not GA"}
allowed_gates = {
    "ci",
    "security",
    "release-manifest",
    "release-maturity",
    "release-checklist",
    "auth-e2e",
    "admin-e2e",
    "oauth2-e2e",
    "enterprise-e2e",
    "proxy-simulation",
    "policy-simulation",
}

ga_count = 0
for module, entry in modules.items():
    if not isinstance(entry, dict):
        errors.append(f"modules.{module} must be an object")
        continue

    status = entry.get("status")
    if status not in allowed_statuses:
        errors.append(f"modules.{module}.status must be one of: {', '.join(sorted(allowed_statuses))}")
        continue

    if status == "GA":
        ga_count += 1

    deployment = entry.get("deployment")
    if not isinstance(deployment, str) or not deployment.strip():
        errors.append(f"modules.{module}.deployment must be a non-empty string")

    summary = entry.get("summary")
    if not isinstance(summary, str) or not summary.strip():
        errors.append(f"modules.{module}.summary must be a non-empty string")

    caveats = entry.get("caveats")
    if not isinstance(caveats, list):
        errors.append(f"modules.{module}.caveats must be an array")
    elif status != "GA" and len(caveats) == 0:
        errors.append(f"modules.{module}.caveats must include at least one caveat for non-GA modules")

    gates = entry.get("required_gates")
    if not isinstance(gates, list):
        errors.append(f"modules.{module}.required_gates must be an array")
    else:
        if status in {"GA", "Beta"} and len(gates) == 0:
            errors.append(f"modules.{module}.required_gates must not be empty for GA/Beta modules")
        if status == "Not GA" and len(gates) > 0:
            errors.append(f"modules.{module}.required_gates must be empty for Not GA modules")
        invalid_gates = [g for g in gates if g not in allowed_gates]
        if invalid_gates:
            errors.append(
                f"modules.{module}.required_gates contains unknown gates: {', '.join(sorted(set(invalid_gates)))}"
            )

if ga_count == 0:
    errors.append("at least one module must be marked GA")

if errors:
    print("::error::Release maturity validation failed:")
    for issue in errors:
        print(f"  - {issue}")
    sys.exit(1)

print(f"Release maturity validation passed with {len(actual_modules)} modules.")
PY
