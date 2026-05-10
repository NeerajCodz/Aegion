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

if ! command -v go >/dev/null 2>&1 && command -v go.exe >/dev/null 2>&1; then
	go() { go.exe "$@"; }
fi

if ! command -v npm >/dev/null 2>&1 && command -v npm.cmd >/dev/null 2>&1; then
	npm() { npm.cmd "$@"; }
fi

echo "==> Validating release metadata and checklists"
bash ./scripts/check-release-manifest.sh
bash ./scripts/check-release-maturity.sh
bash ./scripts/check-release-checklist.sh
bash ./scripts/check-go-crypto-usage.sh

echo "==> Verifying generated protobuf consistency"
bash ./scripts/check-proto-consistency.sh

echo "==> Running Go test suite"
go test ./...

echo "==> Running Go vulnerability and static security scans"
go install golang.org/x/vuln/cmd/govulncheck@latest
go install github.com/securego/gosec/v2/cmd/gosec@latest
GO_BIN_DIR="$(go env GOPATH)/bin"
if command -v cygpath >/dev/null 2>&1; then
	GO_BIN_DIR="$(cygpath -u "$GO_BIN_DIR")"
fi
export PATH="${GO_BIN_DIR}:${PATH}"
if ! command -v govulncheck >/dev/null 2>&1 && command -v govulncheck.exe >/dev/null 2>&1; then
	govulncheck() { govulncheck.exe "$@"; }
fi
if ! command -v gosec >/dev/null 2>&1 && command -v gosec.exe >/dev/null 2>&1; then
	gosec() { gosec.exe "$@"; }
fi
govulncheck ./...
gosec -severity high -confidence high -exclude-generated ./...

echo "==> Running Admin SPA quality gates"
pushd modules/admin/spa >/dev/null
npm ci
npm run lint
npm run build
npm run test --if-present
popd >/dev/null

echo "Release preflight checks passed."
