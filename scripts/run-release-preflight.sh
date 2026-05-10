#!/usr/bin/env bash
# Run release preflight gates that must pass before publishing a release.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

case "$(uname -s)" in
MINGW*|MSYS*|CYGWIN*)
	if [ -d /c/msys64/ucrt64/bin ]; then
		export PATH="/c/msys64/ucrt64/bin:${PATH}"
	fi
	;;
esac

if ! command -v cargo >/dev/null 2>&1 && command -v cargo.exe >/dev/null 2>&1; then
	cargo() { cargo.exe "$@"; }
fi

echo "==> Validating release metadata and checklists"
bash ./scripts/check-release-manifest.sh
bash ./scripts/check-release-maturity.sh
bash ./scripts/check-release-checklist.sh
bash ./scripts/check-rust-ffi-usage.sh

echo "==> Verifying generated protobuf consistency"
bash ./scripts/check-proto-consistency.sh

echo "==> Building Rust workspace for CGO linkage"
pushd rust >/dev/null
cargo build --release --workspace
popd >/dev/null

echo "==> Running Go test suite"
export CGO_ENABLED=1
export CGO_LDFLAGS="-L${ROOT_DIR}/rust/target/release"
go test ./...

echo "==> Running Go vulnerability and static security scans"
go install golang.org/x/vuln/cmd/govulncheck@latest
go install github.com/securego/gosec/v2/cmd/gosec@latest
export PATH="$(go env GOPATH)/bin:${PATH}"
govulncheck ./...
gosec -severity high -confidence high -exclude-generated ./...

echo "==> Running Rust test and dependency security gates"
pushd rust >/dev/null
cargo test --workspace
cargo install --locked cargo-audit
cargo install --locked cargo-deny
cargo audit --deny warnings
cargo deny check advisories
popd >/dev/null

echo "==> Running Admin SPA quality gates"
pushd modules/admin/spa >/dev/null
npm ci
npm run lint
npm run build
npm run test --if-present
popd >/dev/null

echo "Release preflight checks passed."
