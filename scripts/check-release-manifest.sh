#!/usr/bin/env bash
# Validate build/release-manifest.json schema and module coverage.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

MANIFEST_PATH="${1:-build/release-manifest.json}"
RELEASE_TAG="${RELEASE_TAG:-}"
STRICT_DIGESTS="${STRICT_DIGESTS:-false}"

if command -v python3 >/dev/null 2>&1; then
    PYTHON_CMD=(python3)
elif command -v python >/dev/null 2>&1; then
    PYTHON_CMD=(python)
elif command -v py >/dev/null 2>&1; then
    PYTHON_CMD=(py -3)
else
    echo "python3 (or compatible python) is required to validate the release manifest"
    exit 1
fi

"${PYTHON_CMD[@]}" - "$MANIFEST_PATH" "$RELEASE_TAG" "$STRICT_DIGESTS" <<'PY'
import json
import re
import sys
from pathlib import Path

manifest_path = Path(sys.argv[1])
release_tag = sys.argv[2].strip()
strict_digests = sys.argv[3].strip().lower() == "true"

errors = []

SEMVER_RE = re.compile(r"^(?:v)?\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$")
DATE_RE = re.compile(r"^\d{4}-\d{2}-\d{2}$")
HEX_DIGEST_RE = re.compile(r"^sha256:[0-9a-f]{64}$")

if not manifest_path.exists():
    print(f"::error::{manifest_path} does not exist")
    sys.exit(1)

try:
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
except json.JSONDecodeError as exc:
    print(f"::error::Invalid JSON in {manifest_path}: {exc}")
    sys.exit(1)

if not isinstance(manifest, dict):
    print("::error::Release manifest must be a JSON object")
    sys.exit(1)

version = manifest.get("version")
if not isinstance(version, str) or not SEMVER_RE.match(version):
    errors.append("version must be a semantic version string (e.g. 1.2.3 or v1.2.3)")

release_date = manifest.get("release_date")
if not isinstance(release_date, str) or not DATE_RE.match(release_date):
    errors.append("release_date must be in YYYY-MM-DD format")

images = manifest.get("images")
if not isinstance(images, dict) or not images:
    errors.append("images must be a non-empty object")
    images = {}

compatibility = manifest.get("compatibility")
if not isinstance(compatibility, dict):
    errors.append("compatibility must be an object")
    compatibility = {}

for key in ("min_core_version", "postgres_versions", "redis_versions"):
    if key not in compatibility:
        errors.append(f"compatibility.{key} is required")

if "postgres_versions" in compatibility and not isinstance(compatibility.get("postgres_versions"), list):
    errors.append("compatibility.postgres_versions must be a list")
if "redis_versions" in compatibility and not isinstance(compatibility.get("redis_versions"), list):
    errors.append("compatibility.redis_versions must be a list")
if "min_core_version" in compatibility and (
    not isinstance(compatibility.get("min_core_version"), str)
    or not SEMVER_RE.match(compatibility["min_core_version"])
):
    errors.append("compatibility.min_core_version must be a semantic version string")


def extract_module_versions(config_path: Path) -> set[str]:
    modules: set[str] = set()
    if not config_path.exists():
        return modules

    in_section = False
    section_indent = 0
    for raw_line in config_path.read_text(encoding="utf-8").splitlines():
        stripped = raw_line.strip()
        if not stripped or stripped.startswith("#"):
            continue

        indent = len(raw_line) - len(raw_line.lstrip(" "))
        if not in_section:
            if stripped == "module_versions:":
                in_section = True
                section_indent = indent
            continue

        if indent <= section_indent:
            in_section = False
            if stripped == "module_versions:":
                in_section = True
                section_indent = indent
            continue

        key_match = re.match(r"^([A-Za-z0-9_]+)\s*:", stripped)
        if key_match:
            modules.add(key_match.group(1))

    return modules


required_modules = {"core"}
for config_file in (
    Path("configs/aegion.yaml"),
    Path("configs/aegion.example.yaml"),
    Path("configs/aegion.test.yaml"),
    Path("configs/aegion.staging.yaml"),
    Path("configs/aegion.production.yaml"),
):
    required_modules.update(extract_module_versions(config_file))

normalized_version = version[1:] if isinstance(version, str) and version.startswith("v") else version

for module in sorted(required_modules):
    entry = images.get(module)
    if not isinstance(entry, dict):
        errors.append(f"images.{module} is required and must be an object")
        continue

    tag = entry.get("tag")
    digest = entry.get("digest")
    if not isinstance(tag, str) or not tag:
        errors.append(f"images.{module}.tag must be a non-empty string")
    if not isinstance(digest, str) or not digest:
        errors.append(f"images.{module}.digest must be a non-empty string")

    expected_module_segment = "core" if module == "core" else module.replace("_", "-")
    expected_repo = f"aegion/aegion-{expected_module_segment}"
    if isinstance(tag, str) and not tag.startswith(f"{expected_repo}:"):
        errors.append(
            f"images.{module}.tag must start with '{expected_repo}:' (found '{tag}')"
        )

    if isinstance(tag, str) and isinstance(normalized_version, str):
        tag_version = tag.rsplit(":", 1)[-1]
        accepted_versions = {normalized_version, f"v{normalized_version}"}
        if tag_version not in accepted_versions:
            errors.append(
                f"images.{module}.tag version '{tag_version}' must match manifest version '{normalized_version}'"
            )

    if isinstance(digest, str):
        if strict_digests:
            if not HEX_DIGEST_RE.match(digest):
                errors.append(
                    f"images.{module}.digest must be a sha256 hex digest in strict mode"
                )
            elif digest == "sha256:" + ("0" * 64):
                errors.append(
                    f"images.{module}.digest cannot be all zeroes in strict mode"
                )
        elif not digest.startswith("sha256:"):
            errors.append(f"images.{module}.digest must start with 'sha256:'")

if release_tag:
    cleaned_tag = release_tag.removeprefix("refs/tags/")
    cleaned_tag = cleaned_tag[1:] if cleaned_tag.startswith("v") else cleaned_tag
    if isinstance(normalized_version, str) and cleaned_tag != normalized_version:
        errors.append(
            f"Manifest version '{normalized_version}' must match release tag '{cleaned_tag}'"
        )

if errors:
    print("::error::Release manifest validation failed:")
    for item in errors:
        print(f"  - {item}")
    sys.exit(1)

mode = "strict" if strict_digests else "standard"
print(
    f"Release manifest validation passed ({mode} mode) with {len(required_modules)} required modules."
)
PY
