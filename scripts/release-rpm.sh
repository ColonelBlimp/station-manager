#!/usr/bin/env bash
# Build a Station Manager v2 RPM end to end.
#
# Steps:
#   1. Build the embedded Svelte SPAs (frontend/{config,logbook,app})
#      so each `dist/` is current — they get embedded via //go:embed.
#   2. Build the daemon binary at build/bin/smd. CGO_ENABLED=0 +
#      modernc-sqlite gives a fully static linux/amd64 binary.
#   3. Hand off to nfpm for the actual packaging.
#
# Usage:   scripts/release-rpm.sh <version>
# Example: scripts/release-rpm.sh 2.0.0
#          scripts/release-rpm.sh v2.0.0-rc.1   (leading 'v' is stripped)
#
# The explicit <version> is the readable build version (-X …/internal/buildinfo.Version →
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

# Load .env (gitignored) so build-time secrets — the ClubLog API key
# (CLUBLOG_API_KEY, baked via -ldflags below) — are available. Covers a bare
# run on the host AND the release container, where release.sh bind-mounts the
# repo (including .env) at /src; release.sh also passes -e CLUBLOG_API_KEY, so
# the key resolves either way. An unset key just bakes an empty string, leaving
# the ClubLog forwarder inert (Submit fails Terminal; the daemon still runs).
if [[ -f .env ]]; then set -a; . ./.env; set +a; fi

if ! command -v nfpm >/dev/null 2>&1; then
  echo "error: nfpm not in PATH (try: go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest)" >&2
  exit 1
fi

# shellcheck source=scripts/version.sh
. "$(dirname "$0")/version.sh"
VERSION="${1#v}" # strip an optional leading 'v' (v2.0.0 → 2.0.0)
RPM_VERSION=$(sm_rpm_version "$VERSION")

echo "── [1/3] Building SPAs → frontend/{config,logbook,app}/dist/ + manual → manual/public/ ──"
# Every embedded SPA (frontend/embed.go //go:embed all:<spa>/dist) must be present
# and current — the Go build embeds whatever is in each dist/, so a missing or
# stale bundle ships a broken/old client for that surface.
# SM_SKIP_SPA=1 reuses already-built dist/ dirs (set by scripts/release.sh, which
# builds the SPAs on the host so the build container needs no Node). Bare runs
# still build them.
if [ "${SM_SKIP_SPA:-}" = "1" ]; then
  for spa in config logbook app; do
    if [ ! -d "frontend/${spa}/dist" ]; then
      echo "error: SM_SKIP_SPA=1 but frontend/${spa}/dist/ is missing — build the SPAs first" >&2
      exit 1
    fi
  done
  echo "  (SM_SKIP_SPA=1 — using pre-built frontend/{config,logbook,app}/dist/)"
else
  for spa in config logbook app; do
    echo "  • frontend/${spa}"
    (cd "frontend/${spa}" && npm run build)
  done
fi
# SM_SKIP_MANUAL=1 reuses an already-built manual/public/ (set by
# scripts/release.sh, which builds the manual on the host so the build
# container needs no Hugo — same rationale as the SPA). Bare runs still build it.
if [ "${SM_SKIP_MANUAL:-}" = "1" ]; then
  if [ ! -f manual/public/index.html ]; then
    echo "error: SM_SKIP_MANUAL=1 but manual/public/index.html is missing — build the manual first" >&2
    exit 1
  fi
  echo "  (SM_SKIP_MANUAL=1 — using pre-built manual/public/)"
else
  if ! command -v hugo >/dev/null 2>&1; then
    echo "error: hugo not in PATH (the operator manual is built with Hugo); build it on the host and pass SM_SKIP_MANUAL=1" >&2
    exit 1
  fi
  (cd manual && hugo --quiet)
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
# -X …/internal/buildinfo.Version injects the build version into the single
# carrier buildinfo.Version (cmd/smd no longer declares its own)
# which feeds both the User-Agent header on outbound HTTP and the
# PROGRAMVERSION field on ADIF exports.
# CLUBLOG_API_KEY (the build-injected ClubLog app key) arrives from the caller's
# env — passed into the release container by scripts/release.sh (-e), or from the
# shell/.env for a bare run. Empty → the ClubLog forwarder stays inert.
CGO_ENABLED=$CGO_VAL go build -trimpath "${TAGS_ARG[@]}" \
    -ldflags="-s -w -X github.com/ColonelBlimp/station-manager/internal/buildinfo.Version=${VERSION} -X github.com/ColonelBlimp/station-manager/internal/buildinfo.Env=release -X github.com/ColonelBlimp/station-manager/internal/forwarding/clublog.InjectedAPIKey=${CLUBLOG_API_KEY:-}" \
    -o build/bin/smd ./cmd/smd

echo "── [3/3] Packaging RPM → build/release/ (RPM version: ${RPM_VERSION}) ──"
mkdir -p build/release
VERSION="$RPM_VERSION" nfpm pkg -f nfpm.yaml -p rpm -t build/release/

echo
echo "Built ${VERSION}:"
ls -lh build/release/*.rpm
