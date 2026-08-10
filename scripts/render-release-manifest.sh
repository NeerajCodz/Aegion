#!/usr/bin/env bash
# Render a digest-pinned release manifest from the selected built image artifacts.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

TEMPLATE_PATH="${1:-build/release-manifest.json}"
OUTPUT_PATH="${2:-dist/release-manifest.json}"
shift $(( $# >= 2 ? 2 : $# ))

if [[ "$#" -eq 0 ]]; then
    echo "usage: $0 [template-path] [output-path] module=sha256:<64-hex> [...]" >&2
    exit 1
fi

if command -v python3 >/dev/null 2>&1; then
    PYTHON_CMD=(python3)
elif command -v python >/dev/null 2>&1; then
    PYTHON_CMD=(python)
elif command -v py >/dev/null 2>&1; then
    PYTHON_CMD=(py -3)
else
    echo "python3 (or compatible python) is required to render the release manifest" >&2
    exit 1
fi

"${PYTHON_CMD[@]}" - "$TEMPLATE_PATH" "$OUTPUT_PATH" "$@" <<'PY'
import json
import re
import sys
from datetime import date
from pathlib import Path

source = Path(sys.argv[1])
destination = Path(sys.argv[2])
hex_digest = re.compile(r"^sha256:[0-9a-f]{64}$")

digests = {}
for assignment in sys.argv[3:]:
    module, separator, digest = assignment.partition("=")
    if not separator or not module or not hex_digest.fullmatch(digest):
        raise SystemExit(f"invalid digest assignment {assignment!r}; expected module=sha256:<64 lowercase hex>")
    if module in digests:
        raise SystemExit(f"duplicate digest assignment for {module!r}")
    digests[module] = digest

try:
    manifest = json.loads(source.read_text(encoding="utf-8"))
except (OSError, json.JSONDecodeError) as exc:
    raise SystemExit(f"cannot read template {source}: {exc}") from exc

if not isinstance(manifest, dict) or manifest.get("kind") != "template":
    raise SystemExit("source manifest must be a kind: template release manifest")
images = manifest.get("images")
if not isinstance(images, dict) or not images:
    raise SystemExit("template images must be a non-empty object")

expected = set(images)
unknown = sorted(set(digests) - expected)
if unknown:
    raise SystemExit(f"unknown digest modules: {', '.join(unknown)}")
if "core" not in digests:
    raise SystemExit("a generated release manifest must include the core digest")

rendered_images = {}
for module, image in images.items():
    if not isinstance(image, dict) or image.get("digest") is not None:
        raise SystemExit(f"images.{module} must be an object with a null template digest")
    if module not in digests:
        continue
    rendered_image = dict(image)
    rendered_image["digest"] = digests[module]
    rendered_images[module] = rendered_image
manifest["images"] = rendered_images

manifest["kind"] = "release"
manifest["release_date"] = date.today().isoformat()
destination.parent.mkdir(parents=True, exist_ok=True)
destination.write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
PY
