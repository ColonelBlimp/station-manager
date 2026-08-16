#!/usr/bin/env bash
# Build the Station Manager release RPM — the CGO + PocketFFT (live-FT8) build,
# compiled in an AlmaLinux 8 container (glibc 2.28) so it runs on every RPM distro
# at or newer than RHEL 8: Fedora (incl. 43), RHEL/Rocky/Alma 8+, recent openSUSE.
#
# Why a container: the FT8 build is CGO and dynamically linked to glibc (live
# capture dlopen's the host audio backend, which rules out a fully static binary),
# so the binary requires "target glibc >= build-host glibc". Building on the
# operator's own (newer) Fedora would produce an RPM that fails to start on older
# distros with "GLIBC_2.xx not found". The oldest practical base fixes that floor.
#
# This is THE release entrypoint. scripts/release-rpm.sh is the inner builder; a
# bare run of it stays CGO-free/static on purpose (so an accidental non-container
# build can't ship a glibc-pinned binary). Here we pass SM_FFT=pocketfft.
#
# Usage:   scripts/release.sh <version>
# Example: scripts/release.sh 2.0.0-alpha.2
#
# Requires: podman (or set SM_CONTAINER_ENGINE=docker), Node/npm + Go on the host
# (the SPA is built on the host), and network on the FIRST run to build the
# builder image. The image is cached after that.

set -euo pipefail

if [ $# -lt 1 ]; then
  echo "usage: $0 <version>" >&2
  exit 2
fi
VERSION="$1"

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

# PUBLIC (distributable) release (ST-7): this produces a key-free RPM. .env is still
# loaded (it may carry other build settings) but the ClubLog key is NOT passed into the
# container, and if the host env provides CLUBLOG_API_KEY we FAIL — a public release must
# never bake the shared confidential key. Keyed dogfood builds are scripts/dev-rpm.sh.
if [[ -f .env ]]; then set -a; . ./.env; set +a; fi
if [[ -n "${CLUBLOG_API_KEY:-}" ]]; then
  echo "error: release.sh builds a PUBLIC (distributable) RPM and must not carry a ClubLog" >&2
  echo "       key, but CLUBLOG_API_KEY is set. Unset it (or use scripts/dev-rpm.sh for a" >&2
  echo "       keyed dogfood build)." >&2
  exit 1
fi

ENGINE="${SM_CONTAINER_ENGINE:-podman}"
IMAGE="${SM_RELEASE_IMAGE:-sm-release-builder}"

if ! command -v "$ENGINE" >/dev/null 2>&1; then
  echo "error: container engine '$ENGINE' not found (install podman, or set SM_CONTAINER_ENGINE)" >&2
  exit 1
fi

# Build the builder image once; reused (and cached) on later releases. Rebuild it
# by hand (`$ENGINE rmi $IMAGE`) when the Go version in Containerfile.release moves.
if ! "$ENGINE" image inspect "$IMAGE" >/dev/null 2>&1; then
  echo "── Building release builder image ($IMAGE) — one-time, needs network ──"
  "$ENGINE" build -t "$IMAGE" -f packaging/Containerfile.release packaging/
fi

# 1) SPA + manual on the HOST: fast, uses the operator's cached node/Hugo
#    toolchains, and keeps Node and Hugo out of the build container. The
#    container's go:embed picks up dist/ and manual/public/.
echo "── [1/2] Building SPAs + manual on host → frontend/{config,logbook,app}/dist/ + manual/public/ ──"
# All three embedded SPAs (frontend/embed.go) must be built on the host — the
# container's go:embed picks up each dist/, so a skipped SPA ships a stale bundle.
for spa in config logbook app; do
  echo "  • frontend/${spa}"
  (cd "frontend/${spa}" && npm run build)
done
if ! command -v hugo >/dev/null 2>&1; then
  echo "error: hugo not in PATH (the operator manual is built with Hugo — e.g. 'dnf install hugo')" >&2
  exit 1
fi
(cd manual && hugo --quiet)

# 2) Compile the CGO+PocketFFT binary + package the RPM INSIDE the container,
#    reusing release-rpm.sh with the SPA + manual steps skipped.
#
# Notes:
#   --security-opt label=disable: lets the container read the bind-mounted repo
#     under SELinux without relabeling the working tree on every run (faster and
#     non-destructive vs a :Z mount).
#   GOMODCACHE (read-only): reuse the host module cache so a release doesn't
#     re-download the module graph over a slow link. Drop this -v if a module is
#     missing from the host cache (it'll fall back to the network).
echo "── [2/2] Building CGO+PocketFFT binary + RPM in $IMAGE ──"
HOST_GOMODCACHE="$(go env GOMODCACHE 2>/dev/null || echo "$HOME/go/pkg/mod")"
MOUNT_GOMODCACHE=()
if [ -d "$HOST_GOMODCACHE" ]; then
  MOUNT_GOMODCACHE=(-v "$HOST_GOMODCACHE":/go/pkg/mod:ro -e GOMODCACHE=/go/pkg/mod)
fi

"$ENGINE" run --rm \
  --security-opt label=disable \
  -v "$REPO_ROOT":/src \
  -w /src \
  "${MOUNT_GOMODCACHE[@]}" \
  -e SM_FFT=pocketfft \
  -e SM_SKIP_SPA=1 \
  -e SM_SKIP_MANUAL=1 \
  "$IMAGE" \
  bash -c "scripts/release-rpm.sh '$VERSION'"

echo
echo "Release RPM (CGO + PocketFFT, glibc 2.28 baseline — runs on RHEL 8+ / Fedora incl. 43):"
ls -lh build/release/*.rpm
