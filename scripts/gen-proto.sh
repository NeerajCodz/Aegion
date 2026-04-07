#!/bin/bash
# Generate protobuf stubs from proto/ definitions.
set -euo pipefail

PROTO_DIR="proto"
MODULE_PATH="github.com/aegion/aegion"

echo "Generating protobuf stubs..."

PROTOC_BIN="$(command -v protoc || true)"
if [ -z "$PROTOC_BIN" ]; then
    PROTOC_BIN="$(command -v protoc.exe || true)"
fi
if [ -z "$PROTOC_BIN" ]; then
    echo "protoc is required but was not found in PATH"
    exit 1
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
    "$PROTOC_BIN" \
        --go_out=. \
        --go_opt=module="$MODULE_PATH" \
        --go-grpc_out=. \
        --go-grpc_opt=module="$MODULE_PATH" \
        -I "$PROTO_DIR" \
        "$rel_path"
done

echo "Protobuf generation complete!"
