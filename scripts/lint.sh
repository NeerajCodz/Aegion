#!/bin/bash
# Run all linters
set -e

echo "Running Go linter..."
golangci-lint run ./...

echo "Running Rust linter..."
cd rust
cargo clippy --workspace -- -D warnings
cargo fmt --check

echo "All linting passed!"
