#!/usr/bin/env bash
# Build a development RPM for local testing.
#
# Same pipeline as scripts/release-rpm.sh (SPA build → static Go build
# → nfpm pack) but pinned to a fixed filename
# (station-manager-dev.x86_64.rpm) and a fixed version string (`dev`)
# so the developer doesn't have to invent a tag for every iteration.
# The `dev` version string also feeds the daemon's built-in
# User-Agent (`station-manager/dev`) and ADIF PROGRAMVERSION header,
# which makes dev-vs-release identification trivial on the network.
#
# Usage: scripts/dev-rpm.sh
#
# Install the result with:
#   sudo dnf install -y build/release/station-manager-dev.x86_64.rpm
# Re-run this script after edits — nfpm overwrites the target file,
# Go build is incremental, dnf reinstall via the same filename works.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

if ! command -v nfpm >/dev/null 2>&1; then
  echo "error: nfpm not in PATH (try: go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest)" >&2
  exit 1
fi

VERSION=dev
OUTPUT=build/release/station-manager-dev.x86_64.rpm

echo "── [1/3] Building SPA → frontend/logging/dist/ ──"
(cd frontend/logging && npm run build)

echo "── [2/3] Building daemon → build/bin/smd ──"
mkdir -p build/bin
CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -w -X main.Version=${VERSION}" \
    -o build/bin/smd ./cmd/smd

echo "── [3/3] Packaging RPM → ${OUTPUT} ──"
mkdir -p build/release
VERSION="${VERSION}" nfpm pkg -f nfpm.yaml -p rpm -t "${OUTPUT}"

echo
echo "Built:"
ls -lh "${OUTPUT}"
