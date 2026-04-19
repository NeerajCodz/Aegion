#!/usr/bin/env bash
# Validate build/release-checklist.json structure and required approvals.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

CHECKLIST_PATH="${1:-build/release-checklist.json}"

if command -v python3 >/dev/null 2>&1; then
    PYTHON_CMD=(python3)
elif command -v python >/dev/null 2>&1; then
    PYTHON_CMD=(python)
elif command -v py >/dev/null 2>&1; then
    PYTHON_CMD=(py -3)
else
    echo "python3 (or compatible python) is required to validate the release checklist"
    exit 1
fi

"${PYTHON_CMD[@]}" - "$CHECKLIST_PATH" <<'PY'
import json
import re
import sys
from pathlib import Path

checklist_path = Path(sys.argv[1])
errors = []

DATE_RE = re.compile(r"^\d{4}-\d{2}-\d{2}$")
ALLOWED_STATUS = {"pass", "waived", "fail"}
REQUIRED_IDS = {
    "migrations-verified",
    "docs-updated",
    "dashboards-present",
    "smoke-tests-green",
    "rollback-tested",
    "backup-restore-drill",
    "staging-gate-passed",
    "perf-baseline-recorded",
    "no-ga-not-implemented",
}

if not checklist_path.exists():
    print(f"::error::{checklist_path} does not exist")
    sys.exit(1)

try:
    payload = json.loads(checklist_path.read_text(encoding="utf-8"))
except json.JSONDecodeError as exc:
    print(f"::error::Invalid JSON in {checklist_path}: {exc}")
    sys.exit(1)

if not isinstance(payload, dict):
    print("::error::Release checklist must be a JSON object")
    sys.exit(1)

schema_version = payload.get("schema_version")
if not isinstance(schema_version, int) or schema_version <= 0:
    errors.append("schema_version must be a positive integer")

last_updated = payload.get("last_updated")
if not isinstance(last_updated, str) or not DATE_RE.match(last_updated):
    errors.append("last_updated must be in YYYY-MM-DD format")

items = payload.get("items")
if not isinstance(items, list) or len(items) == 0:
    errors.append("items must be a non-empty array")
    items = []

by_id = {}
for idx, item in enumerate(items):
    location = f"items[{idx}]"
    if not isinstance(item, dict):
        errors.append(f"{location} must be an object")
        continue

    item_id = item.get("id")
    if not isinstance(item_id, str) or not item_id.strip():
        errors.append(f"{location}.id must be a non-empty string")
        continue
    if item_id in by_id:
        errors.append(f"duplicate checklist item id: {item_id}")
        continue
    by_id[item_id] = item

    description = item.get("description")
    if not isinstance(description, str) or not description.strip():
        errors.append(f"items[{item_id}].description must be a non-empty string")

    required = item.get("required")
    if not isinstance(required, bool):
        errors.append(f"items[{item_id}].required must be a boolean")

    status = item.get("status")
    if status not in ALLOWED_STATUS:
        errors.append(
            f"items[{item_id}].status must be one of: {', '.join(sorted(ALLOWED_STATUS))}"
        )

    evidence = item.get("evidence")
    if not isinstance(evidence, str) or not evidence.strip():
        errors.append(f"items[{item_id}].evidence must be a non-empty string")

missing_required = sorted(REQUIRED_IDS - set(by_id.keys()))
if missing_required:
    errors.append(f"missing required checklist items: {', '.join(missing_required)}")

for item_id in sorted(REQUIRED_IDS):
    item = by_id.get(item_id)
    if not isinstance(item, dict):
        continue
    if item.get("required") is not True:
        errors.append(f"items[{item_id}] must set required=true")
    if item.get("status") != "pass":
        errors.append(f"items[{item_id}] must have status=pass")

if errors:
    print("::error::Release checklist validation failed:")
    for issue in errors:
        print(f"  - {issue}")
    sys.exit(1)

print(f"Release checklist validation passed with {len(items)} items.")
PY
