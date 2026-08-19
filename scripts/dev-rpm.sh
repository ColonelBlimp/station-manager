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
#   sudo dnf install -y build/private/station-manager-dev.x86_64.rpm
# Re-run after edits — nfpm overwrites the target file; rebuilding the same
# commit dirty keeps the same NVR, so deploy-local-dev.sh uses --replacepkgs.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

# Load .env (gitignored) so build-time secrets — the ClubLog API key
# (CLUBLOG_API_KEY, baked via -ldflags below) — are available when this script
# is invoked DIRECTLY (e.g. by deploy-local-dev.sh, which unlike `task` does not
# auto-load .env).
if [[ -f .env ]]; then set -a; . ./.env; set +a; fi

# PRIVATE (keyed dogfood) build path (ST-7). The ClubLog application key is baked
# into the binary, so this RPM carries the shared confidential key (extractable with
# `strings`, ADR 0054) and MUST NOT be published — its output lives under
# build/private/ and it embeds a PRIVATE-BUILD-DO-NOT-DISTRIBUTE marker. A private
# build therefore REQUIRES the key; a keyless, distributable RPM is the PUBLIC path,
# scripts/release-rpm.sh.
if [[ -z "${CLUBLOG_API_KEY:-}" ]]; then
  echo "error: dev-rpm.sh is the PRIVATE (keyed) build path and requires a non-empty" >&2
  echo "       CLUBLOG_API_KEY (set it in .env). For a keyless, distributable RPM use" >&2
  echo "       scripts/release-rpm.sh." >&2
  exit 1
fi

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
OUTPUT=build/private/station-manager-dev.x86_64.rpm

echo "── [1/3] Building the app SPA → frontend/app/dist/ + manual → manual/public/ ──"
# Every embedded SPA (frontend/embed.go //go:embed all:<spa>/dist) must be rebuilt
# here — the Go build embeds whatever is already in each dist/, so skipping one
# silently ships a STALE app bundle.
# `app` (ADR 0044) was missed when it was embedded (2026-07-11) — a day of /app/
# dogfood deploys silently shipped whatever dist/ last held (found 2026-07-13).
for spa in app; do
  # Keep the advertised one-command deploy usable on a fresh checkout (and
  # after node_modules has been cleaned) without paying for `npm ci` on every
  # dogfood rebuild. All SPAs commit package-lock.json, but retain the fallback
  # so a newly scaffolded SPA fails usefully before its first lockfile lands.
  if [ ! -x "frontend/${spa}/node_modules/.bin/vite" ]; then
    echo "  • frontend/${spa} dependencies missing — installing"
    if [ -f "frontend/${spa}/package-lock.json" ]; then
      (cd "frontend/${spa}" && npm ci)
    else
      (cd "frontend/${spa}" && npm install)
    fi
  fi
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
    -ldflags="-s -w -X github.com/ColonelBlimp/station-manager/internal/buildinfo.Version=${VERSION} -X github.com/ColonelBlimp/station-manager/internal/buildinfo.Env=release -X github.com/ColonelBlimp/station-manager/internal/buildinfo.BuildScope=private -X github.com/ColonelBlimp/station-manager/internal/forwarding/clublog.InjectedAPIKey=${CLUBLOG_API_KEY:-}" \
    -o build/bin/smd ./cmd/smd

echo "── [3/3] Packaging RPM → ${OUTPUT} (RPM version: ${RPM_VERSION}) ──"
mkdir -p build/private
VERSION="${RPM_VERSION}" nfpm pkg -f nfpm.private.yaml -p rpm -t "${OUTPUT}"

echo
echo "Built ${VERSION}:"
ls -lh "${OUTPUT}"
