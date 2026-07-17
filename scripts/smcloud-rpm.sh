#!/usr/bin/env bash
# Build the smcloud RPM — the deployable artifact for the SM Cloud backup
# service (docs/smcloud-deploy.md). Mirrors scripts/dev-rpm.sh's pipeline
# minus everything smcloud doesn't have: no SPAs, no manual, no CGO. The
# binary is fully static pure Go, so this builds on any dev box (no
# AlmaLinux glibc-floor container needed) and the RPM installs on any
# RPM distro/arch it was built for.
#
# Usage: scripts/smcloud-rpm.sh                       (amd64 → x86_64 RPM)
#        SMCLOUD_ARCH=arm64 scripts/smcloud-rpm.sh    (Pi-class → aarch64 RPM)
#
# The output FILENAME is fixed (build/release/smcloud.<rpmarch>.rpm) so the
# deploy instructions stay stable; the internal Version is git-derived via
# scripts/version.sh, same as the smd packages, so dnf/rpm see real
# upgrades. Rebuilding the same dirty commit keeps the same NVR — install
# that with `rpm -Uvh --replacepkgs`.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

if ! command -v nfpm >/dev/null 2>&1; then
  echo "error: nfpm not in PATH (try: go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest)" >&2
  exit 1
fi

# shellcheck source=scripts/version.sh
. "$(dirname "$0")/version.sh"
VERSION=$(sm_git_version)
RPM_VERSION=$(sm_rpm_version "$VERSION")

SMCLOUD_ARCH="${SMCLOUD_ARCH:-amd64}"
case "$SMCLOUD_ARCH" in
  amd64) RPM_ARCH=x86_64 ;;
  arm64) RPM_ARCH=aarch64 ;;
  *) echo "error: unsupported SMCLOUD_ARCH '${SMCLOUD_ARCH}' (amd64|arm64)" >&2; exit 1 ;;
esac
OUTPUT="build/release/smcloud.${RPM_ARCH}.rpm"

echo "── [1/2] Building smcloud → build/bin/smcloud (version: ${VERSION}, ${SMCLOUD_ARCH}, static) ──"
mkdir -p build/bin
CGO_ENABLED=0 GOOS=linux GOARCH="${SMCLOUD_ARCH}" go build -trimpath \
    -ldflags="-s -w -X main.Version=${VERSION}" \
    -o build/bin/smcloud ./cmd/smcloud

echo "── [2/2] Packaging RPM → ${OUTPUT} (RPM version: ${RPM_VERSION}) ──"
mkdir -p build/release
VERSION="${RPM_VERSION}" SMCLOUD_ARCH="${SMCLOUD_ARCH}" \
    nfpm pkg -f nfpm-smcloud.yaml -p rpm -t "${OUTPUT}"

echo
echo "Built ${VERSION}:"
ls -lh "${OUTPUT}"