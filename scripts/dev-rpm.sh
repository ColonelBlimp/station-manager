#!/usr/bin/env bash
# Build a development RPM for local testing.
#
# Same pipeline as scripts/release-rpm.sh (SPA build → static Go build
# → nfpm pack). The OUTPUT FILENAME stays fixed
# (station-manager-dev.x86_64.rpm) so the dogfood tooling
# (deploy-local-dev.sh) has a stable path, but the VERSION is now derived
# from git (`git describe` off the v2.* tags via scripts/version.sh) rather
# than a fixed `dev` string. So each commit produces a distinct, traceable
# version — e.g. 2.0.0-alpha.1-7-gabc1234 — which feeds the daemon
# User-Agent, the ADIF PROGRAMVERSION stamped on forwarded/exported QSOs,
# and the RPM's internal Version field (so `dnf`/`rpm -U` see real upgrades).
# A -dirty suffix marks a build with uncommitted changes.
#
# Usage: scripts/dev-rpm.sh   (SM_FFT=pocketfft for the CGO backend)
#
# Install the result with:
#   sudo dnf install -y build/release/station-manager-dev.x86_64.rpm
# Re-run after edits — nfpm overwrites the target file; rebuilding the same
# commit dirty keeps the same NVR, so deploy-local-dev.sh uses --replacepkgs.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

if ! command -v nfpm >/dev/null 2>&1; then
  echo "error: nfpm not in PATH (try: go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest)" >&2
  exit 1
fi

if ! command -v hugo >/dev/null 2>&1; then
  echo "error: hugo not in PATH (the operator manual is built with Hugo — e.g. 'dnf install hugo')" >&2
  exit 1
fi

# shellcheck source=scripts/version.sh
. "$(dirname "$0")/version.sh"
VERSION=$(sm_git_version)
RPM_VERSION=$(sm_rpm_version "$VERSION")
OUTPUT=build/release/station-manager-dev.x86_64.rpm

echo "── [1/3] Building SPAs → frontend/{logging,config,logbook}/dist/ + manual → manual/public/ ──"
# Every embedded SPA (frontend/embed.go //go:embed all:<spa>/dist) must be rebuilt
# here — the Go build embeds whatever is already in each dist/, so skipping one
# silently ships a STALE bundle for that client (e.g. config-SPA toggles missing).
for spa in logging config logbook; do
  echo "  • frontend/${spa}"
  (cd "frontend/${spa}" && npm run build)
done
(cd manual && hugo --quiet)

# FFT backend selection — see scripts/release-rpm.sh for the rationale.
# Default gonum (pure Go, static); SM_FFT=pocketfft builds the CGO backend.
# The dev RPM always carries the `dev` build tag, which registers the test-only
# "stub" forwarder (review 2026-06-19 M3) — the shipped release (release-rpm.sh)
# does NOT, so production can't select type:"stub".
CGO_VAL=0
BUILD_TAGS="dev"
FFT_BACKEND="gonum (pure Go, static)"
if [ "${SM_FFT:-}" = "pocketfft" ]; then
  CGO_VAL=1
  BUILD_TAGS="dev pocketfft"
  FFT_BACKEND="PocketFFT (CGO, dynamically linked)"
fi

echo "── [2/3] Building daemon → build/bin/smd (version: ${VERSION}, FFT: ${FFT_BACKEND}) ──"
mkdir -p build/bin
CGO_ENABLED=$CGO_VAL go build -trimpath -tags "${BUILD_TAGS}" \
    -ldflags="-s -w -X main.Version=${VERSION}" \
    -o build/bin/smd ./cmd/smd

echo "── [3/3] Packaging RPM → ${OUTPUT} (RPM version: ${RPM_VERSION}) ──"
mkdir -p build/release
VERSION="${RPM_VERSION}" nfpm pkg -f nfpm.yaml -p rpm -t "${OUTPUT}"

echo
echo "Built ${VERSION}:"
ls -lh "${OUTPUT}"
