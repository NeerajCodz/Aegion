#!/bin/bash
# Generate protobuf stubs from proto/ definitions
set -e

PROTO_DIR="proto"
OUT_DIR="internal/proto"

echo "Generating protobuf stubs..."

# Ensure output directories exist
mkdir -p "$OUT_DIR/core"

# Generate Go code for core protos
for proto_file in "$PROTO_DIR/core"/*.proto; do
    if [ -f "$proto_file" ]; then
        echo "Generating: $proto_file"
        protoc \
            --go_out="$OUT_DIR" \
            --go_opt=paths=source_relative \
            --go-grpc_out="$OUT_DIR" \
            --go-grpc_opt=paths=source_relative \
            -I "$PROTO_DIR" \
            "$proto_file"
    fi
done

echo "Protobuf generation complete!"
