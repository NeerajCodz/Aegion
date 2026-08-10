#!/bin/bash
# Generate protobuf stubs from proto/ definitions.
set -euo pipefail

PROTO_DIR="proto"
MODULE_PATH="github.com/aegion/aegion"

echo "Generating protobuf stubs..."

PROTOC_BIN=""
if [ -x ".tools/protoc/bin/protoc" ]; then
    PROTOC_BIN=".tools/protoc/bin/protoc"
fi

if [ -z "$PROTOC_BIN" ]; then
    PROTOC_BIN="$(command -v protoc || true)"
fi
if [ -z "$PROTOC_BIN" ]; then
    PROTOC_BIN="$(command -v protoc.exe || true)"
fi

if [ -z "$PROTOC_BIN" ] && [ -n "${LOCALAPPDATA:-}" ]; then
    local_app_data="$LOCALAPPDATA"
    if command -v cygpath >/dev/null 2>&1; then
        local_app_data="$(cygpath -u "$LOCALAPPDATA" 2>/dev/null || printf '%s' "$LOCALAPPDATA")"
    fi

    winget_packages_dir="$local_app_data/Microsoft/WinGet/Packages"
    if [ -d "$winget_packages_dir" ]; then
        PROTOC_BIN="$(find "$winget_packages_dir" -maxdepth 4 -type f -path '*/bin/protoc.exe' | sort -r | head -n 1 || true)"
    fi
fi

if [ -z "$PROTOC_BIN" ]; then
    echo "protoc is required but was not found in PATH (or common winget install paths)"
    exit 1
fi

# Some POSIX-on-Windows shells resolve executables to a Windows path that this
# shell cannot execute directly. Resolve the executable name through PATH so
# the shell applies its Windows launcher.
if [[ "$PROTOC_BIN" == *.exe ]] && command -v protoc.exe >/dev/null 2>&1; then
    PROTOC_BIN="protoc.exe"
fi
if ! command -v "$PROTOC_BIN" >/dev/null 2>&1; then
    PROTOC_BIN="$(basename "$PROTOC_BIN")"
    if ! command -v "$PROTOC_BIN" >/dev/null 2>&1; then
        echo "resolved protoc executable '$PROTOC_BIN' is not runnable in this shell"
        exit 1
    fi
fi

PROTOC_CMD=("$PROTOC_BIN")
if [[ "$PROTOC_BIN" == *.exe ]] && command -v cmd.exe >/dev/null 2>&1; then
    PROTOC_CMD=(cmd.exe /c "$PROTOC_BIN")
fi

proto_files=()
while IFS= read -r file; do
    proto_files+=("$file")
done < <(find "$PROTO_DIR" -type f -name "*.proto" | sort)

if [ "${#proto_files[@]}" -eq 0 ]; then
    echo "No proto files found under $PROTO_DIR"
    exit 0
fi

for proto_file in "${proto_files[@]}"; do
    rel_path="${proto_file#${PROTO_DIR}/}"
    echo "Generating: $rel_path"
    "${PROTOC_CMD[@]}" \
        --go_out=. \
        --go_opt=module="$MODULE_PATH" \
        --go-grpc_out=. \
        --go-grpc_opt=module="$MODULE_PATH" \
        -I "$PROTO_DIR" \
        "$rel_path"
done

echo "Protobuf generation complete!"
