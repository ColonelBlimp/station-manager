#!/usr/bin/env bash
# ST-7 build-boundary test (docs/reviews/internal-security-trust-boundary-audit.md).
#
# Proves the ClubLog build-key boundary holds in the build SYSTEM (not the Go code):
#   1. the PRIVATE build path bakes the key into the binary;
#   2. the PUBLIC build path bakes NO key;
#   3. the PUBLIC path REFUSES to build when a key is present (after .env load), before
#      producing any artifact, and never echoes the key;
#   4. the PRIVATE-BUILD-DO-NOT-DISTRIBUTE marker lives only in the private nfpm spec, and
#      no spec/marker contains a key.
#
# A plain shell test on purpose (no godog/BDD): it inspects real built binaries and the
# real build scripts. Invoked by `task ci:local` and CI. Uses a UNIQUE dummy sentinel that
# is never a real key; the sentinel is chosen so a stray match cannot be a coincidence.
set -euo pipefail

cd "$(dirname "$0")/.."

SENTINEL="DUMMY-CLUBLOG-KEY-buildboundary-9f3c1a2b4d6e"
KEY_LD="github.com/ColonelBlimp/station-manager/internal/forwarding/clublog.InjectedAPIKey"
SCOPE_LD="github.com/ColonelBlimp/station-manager/internal/buildinfo.BuildScope"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "BUILD-BOUNDARY FAIL: $*" >&2; exit 1; }

echo "── [1/4] private binary bakes the key ──"
go build -ldflags="-X ${SCOPE_LD}=private -X ${KEY_LD}=${SENTINEL}" -o "$tmp/private_smd" ./cmd/smd
grep -qa -- "$SENTINEL" "$tmp/private_smd" || fail "sentinel key not found in the private binary"

echo "── [2/4] public binary bakes NO key ──"
go build -ldflags="-X ${SCOPE_LD}=public" -o "$tmp/public_smd" ./cmd/smd
if grep -qa -- "$SENTINEL" "$tmp/public_smd"; then
  fail "sentinel key leaked into the public binary"
fi

echo "── [3/4] public build path refuses a present key, after .env load, without leaking it ──"
# release-rpm.sh is pure bash and fails fast at its key guard (right after .env load),
# before any SPA/manual build or nfpm pack — so no artifact is produced. Setting the key in
# the environment models the .env injection path the guard must cover.
out="$tmp/release-out.txt"
if CLUBLOG_API_KEY="$SENTINEL" bash scripts/release-rpm.sh 2.0.0-boundary-test >"$out" 2>&1; then
  fail "release-rpm.sh produced a result with CLUBLOG_API_KEY set (a public build must refuse the key)"
fi
grep -qai "public" "$out" || fail "release-rpm.sh failed, but not at the public-build key guard: $(cat "$out")"
if grep -qa -- "$SENTINEL" "$out"; then
  fail "release-rpm.sh echoed the sentinel key in its output"
fi
# release.sh (the container wrapper) must carry the same host-side guard after its .env load.
grep -q 'CLUBLOG_API_KEY' scripts/release.sh && grep -q 'exit 1' scripts/release.sh \
  || fail "release.sh is missing the public-build key guard"

echo "── [4/4] the do-not-distribute marker is private-only and key-free ──"
grep -q 'PRIVATE-BUILD-DO-NOT-DISTRIBUTE' nfpm.private.yaml || fail "private nfpm spec lacks the marker"
if grep -q 'PRIVATE-BUILD-DO-NOT-DISTRIBUTE' nfpm.yaml; then
  fail "public nfpm spec contains the private marker"
fi
if grep -qa -- "$SENTINEL" nfpm.private.yaml packaging/PRIVATE-BUILD-DO-NOT-DISTRIBUTE; then
  fail "a spec/marker contains a key-like sentinel"
fi

echo "build-boundary: OK — private binary keyed, public binary clean, public path refuses the key, marker private-only"
