#!/bin/bash
# Run all linters
set -e

echo "Running Go linter..."
golangci-lint run ./...


echo "All linting passed!"
