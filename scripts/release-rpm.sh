#!/usr/bin/env bash
# Build a Station Manager v2 RPM end to end.
#
# Steps:
#   1. Build the Svelte SPA (frontend/logging) so that `dist/` is
#      current — it gets embedded into the daemon via //go:embed.
#   2. Build the daemon binary at build/bin/smd. CGO_ENABLED=0 +
#      modernc-sqlite gives a fully static linux/amd64 binary.
#   3. Hand off to nfpm for the actual packaging.
#
# Usage:   scripts/release-rpm.sh <version>
# Example: scripts/release-rpm.sh 2.0.0
#          scripts/release-rpm.sh v2.0.0-rc.1   (leading 'v' is stripped)
#
# The explicit <version> is the readable build version (-X main.Version →
# daemon User-Agent + ADIF PROGRAMVERSION). It is sanitised for the RPM
# Version field, which cannot contain '-' (see scripts/version.sh). For the
# git-derived dogfood version, use scripts/dev-rpm.sh instead.

set -euo pipefail

if [ $# -lt 1 ]; then
  echo "usage: $0 <version>" >&2
  exit 2
fi

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

if ! command -v nfpm >/dev/null 2>&1; then
  echo "error: nfpm not in PATH (try: go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest)" >&2
  exit 1
fi

# shellcheck source=scripts/version.sh
. "$(dirname "$0")/version.sh"
VERSION="${1#v}" # strip an optional leading 'v' (v2.0.0 → 2.0.0)
RPM_VERSION=$(sm_rpm_version "$VERSION")

echo "── [1/3] Building SPA → frontend/logging/dist/ ──"
# SM_SKIP_SPA=1 reuses an already-built dist/ (set by scripts/release.sh, which
# builds the SPA on the host so the build container needs no Node). Bare runs
# still build it.
if [ "${SM_SKIP_SPA:-}" = "1" ]; then
  if [ ! -d frontend/logging/dist ]; then
    echo "error: SM_SKIP_SPA=1 but frontend/logging/dist/ is missing — build the SPA first" >&2
    exit 1
  fi
  echo "  (SM_SKIP_SPA=1 — using pre-built frontend/logging/dist/)"
else
  (cd frontend/logging && npm run build)
fi

# FFT backend selection. Default is gonum (pure Go) → CGO-free, fully
# static binary, cross-platform by default. Set SM_FFT=pocketfft to build
# the CGO PocketFFT backend (~2x faster FT8 decode, but dynamically linked
# against libc and not statically linked). The backend is go-ft8's own
# //go:build pocketfft gate; SM just passes it through. See docs/licensing.md.
CGO_VAL=0
TAGS_ARG=()
FFT_BACKEND="gonum (pure Go, static)"
if [ "${SM_FFT:-}" = "pocketfft" ]; then
  CGO_VAL=1
  TAGS_ARG=(-tags pocketfft)
  FFT_BACKEND="PocketFFT (CGO, dynamically linked)"
fi

echo "── [2/3] Building daemon → build/bin/smd (version: ${VERSION}, FFT: ${FFT_BACKEND}) ──"
mkdir -p build/bin
# -X main.Version injects the build version into cmd/smd's `var Version`
# which feeds both the User-Agent header on outbound HTTP and the
# PROGRAMVERSION field on ADIF exports.
CGO_ENABLED=$CGO_VAL go build -trimpath "${TAGS_ARG[@]}" \
    -ldflags="-s -w -X main.Version=${VERSION}" \
    -o build/bin/smd ./cmd/smd

echo "── [3/3] Packaging RPM → build/release/ (RPM version: ${RPM_VERSION}) ──"
mkdir -p build/release
VERSION="$RPM_VERSION" nfpm pkg -f nfpm.yaml -p rpm -t build/release/

echo
echo "Built ${VERSION}:"
ls -lh build/release/*.rpm
