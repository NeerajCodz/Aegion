#!/usr/bin/env bash
# Validate the release-manifest template or a generated digest-pinned manifest.
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

errors: list[str] = []
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

kind = manifest.get("kind", "release")
if kind not in {"template", "release"}:
    errors.append("kind must be either 'template' or 'release'")
if strict_digests and kind != "release":
    errors.append("strict digest validation requires a generated release manifest, not a template")

schema_version = manifest.get("schema_version")
if schema_version != 2:
    errors.append("schema_version must be 2")

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

# Images are a release concern, not a signal for core to start a module.  The
# only source of an artifact is a Dockerfile that actually exists in this tree.
artifacts = {"core": Path("build/Dockerfile.core")}
for dockerfile in Path("modules").glob("*/Dockerfile"):
    artifacts[dockerfile.parent.name] = dockerfile

embedded_modules = {"password", "magic_link", "policy"}
known_modules = embedded_modules | set(artifacts)

maturity_path = Path("build/release-maturity.json")
try:
    maturity = json.loads(maturity_path.read_text(encoding="utf-8"))
except (OSError, json.JSONDecodeError) as exc:
    errors.append(f"cannot read {maturity_path}: {exc}")
    maturity = {}

maturity_modules = maturity.get("modules", {}) if isinstance(maturity, dict) else {}
if not isinstance(maturity_modules, dict):
    errors.append("release-maturity.modules must be an object")
    maturity_modules = {}

# Every GA workload with a build artifact must be represented.  Non-GA images
# may be present for a release candidate, but no image may claim an embedded or
# absent module artifact.
required_modules = {"core"}
for module, metadata in maturity_modules.items():
    if module == "core" or not isinstance(metadata, dict):
        continue
    deployment = metadata.get("deployment")
    if metadata.get("status") == "GA" and deployment not in {"embedded", "embedded-default"}:
        if module not in artifacts:
            errors.append(f"GA module {module!r} has no Dockerfile artifact")
        else:
            required_modules.add(module)

for module in sorted(images):
    if module in embedded_modules:
        errors.append(f"images.{module} must not exist because {module} is embedded in core")
    elif module not in known_modules:
        errors.append(f"images.{module} is not a known release artifact")
    elif module not in artifacts:
        errors.append(f"images.{module} has no Dockerfile artifact")

for module, entry in sorted(images.items()):
    if not isinstance(entry, dict):
        errors.append(f"images.{module} must be an object")

normalized_version = version[1:] if isinstance(version, str) and version.startswith("v") else version
for module in sorted(required_modules):
    entry = images.get(module)
    if not isinstance(entry, dict):
        errors.append(f"images.{module} is required for a GA release and must be an object")
        continue

    tag = entry.get("tag")
    digest = entry.get("digest")
    if not isinstance(tag, str) or not tag:
        errors.append(f"images.{module}.tag must be a non-empty string")

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

    if kind == "template":
        if digest is not None:
            errors.append(f"images.{module}.digest must be null in a release manifest template")
    elif not isinstance(digest, str) or not HEX_DIGEST_RE.match(digest):
        errors.append(f"images.{module}.digest must be a sha256 hex digest in a generated release manifest")
    elif digest == "sha256:" + ("0" * 64):
        errors.append(f"images.{module}.digest cannot be all zeroes")

# Validate optional candidate images using the same repository/tag/digest
# contract. This keeps the template useful without making every configured
# development module a mandatory production artifact.
for module, entry in sorted(images.items()):
    if module in required_modules or not isinstance(entry, dict):
        continue
    tag = entry.get("tag")
    digest = entry.get("digest")
    expected_module_segment = "core" if module == "core" else module.replace("_", "-")
    expected_repo = f"aegion/aegion-{expected_module_segment}"
    if not isinstance(tag, str) or not tag.startswith(f"{expected_repo}:"):
        errors.append(f"images.{module}.tag must start with '{expected_repo}:'")
    elif isinstance(normalized_version, str):
        tag_version = tag.rsplit(":", 1)[-1]
        if tag_version not in {normalized_version, f"v{normalized_version}"}:
            errors.append(
                f"images.{module}.tag version '{tag_version}' must match manifest version '{normalized_version}'"
            )
    if kind == "template":
        if digest is not None:
            errors.append(f"images.{module}.digest must be null in a release manifest template")
    elif not isinstance(digest, str) or not HEX_DIGEST_RE.match(digest):
        errors.append(f"images.{module}.digest must be a sha256 hex digest in a generated release manifest")
    elif digest == "sha256:" + ("0" * 64):
        errors.append(f"images.{module}.digest cannot be all zeroes")

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
    f"Release manifest validation passed ({mode} mode) with {len(images)} declared images and {len(required_modules)} GA artifacts."
)
PY
