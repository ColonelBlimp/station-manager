#!/usr/bin/env bash
# ClubLog build-key boundary test — ADR 0083 (which superseded ST-7's key-free rule,
# docs/reviews/internal-security-trust-boundary-audit.md).
#
# Proves the boundary holds in the build SYSTEM (not the Go code):
#   1. the PRIVATE (dogfood) build path bakes the key into the binary;
#   2. a bare keyless build (go build / CI) carries no key;
#   3. the RELEASE path REFUSES to build WITHOUT a key, before any artifact;
#   4. with a key (from .env or the environment) the release path passes its guard
#      without echoing the key, hands it to the container BY NAME, and the inner builder
#      injects it with the same -X flag;
#   5. .env — where the key lives — is ignored by Git (the key is never published);
#   6. the PRIVATE-BUILD-DO-NOT-DISTRIBUTE marker lives only in the private nfpm spec,
#      and no spec/marker contains a key.
#
# A plain shell test on purpose (no godog/BDD): it inspects real built binaries and the
# real build scripts. Invoked by `task ci:local` and CI. Uses a UNIQUE dummy sentinel that
# is never a real key; the sentinel is chosen so a stray match cannot be a coincidence.
# The release scripts run from a SCRATCH copy with its own .env, so the operator's real
# .env never takes part and no real build starts.
set -euo pipefail

cd "$(dirname "$0")/.."

SENTINEL="DUMMY-CLUBLOG-KEY-buildboundary-9f3c1a2b4d6e"
KEY_LD="github.com/ColonelBlimp/station-manager/internal/forwarding/clublog.InjectedAPIKey"
SCOPE_LD="github.com/ColonelBlimp/station-manager/internal/buildinfo.BuildScope"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "BUILD-BOUNDARY FAIL: $*" >&2; exit 1; }

echo "── [1/6] private binary bakes the key ──"
go build -ldflags="-X ${SCOPE_LD}=private -X ${KEY_LD}=${SENTINEL}" -o "$tmp/private_smd" ./cmd/smd
grep -qa -- "$SENTINEL" "$tmp/private_smd" || fail "sentinel key not found in the private binary"

echo "── [2/6] a bare keyless build carries no key ──"
go build -ldflags="-X ${SCOPE_LD}=public" -o "$tmp/public_smd" ./cmd/smd
if grep -qa -- "$SENTINEL" "$tmp/public_smd"; then
  fail "sentinel key found in a keyless build"
fi

# A scratch repo root for the release scripts: they cd to their own parent and load its
# .env, so the operator's real .env never takes part. A missing container engine stops
# release.sh right after its key guard, before any build.
fake="$tmp/fake"
mkdir -p "$fake/scripts"
cp scripts/release.sh scripts/release-rpm.sh "$fake/scripts/"

echo "── [3/6] the release path refuses to build without a key ──"
: >"$fake/.env"
out="$tmp/nokey.txt"
for script in release.sh release-rpm.sh; do
  if env -u CLUBLOG_API_KEY SM_CONTAINER_ENGINE=no-such-engine bash "$fake/scripts/$script" 2.0.0-boundary-test >"$out" 2>&1; then
    fail "$script built without a ClubLog key (every release must carry it)"
  fi
  grep -qa "must carry the ClubLog application key" "$out" \
    || fail "$script failed, but not at its key guard: $(cat "$out")"
done
[ -z "$(ls "$fake"/build 2>/dev/null)" ] || fail "a refused release left build output"

echo "── [4/6] with a key the release path passes its guard and injects it ──"
for source in dotenv environment; do
  if [ "$source" = dotenv ]; then
    printf 'CLUBLOG_API_KEY=%s\n' "$SENTINEL" >"$fake/.env"
    run=(env -u CLUBLOG_API_KEY SM_CONTAINER_ENGINE=no-such-engine)
  else
    : >"$fake/.env"
    run=(env CLUBLOG_API_KEY="$SENTINEL" SM_CONTAINER_ENGINE=no-such-engine)
  fi
  out="$tmp/key-$source.txt"
  "${run[@]}" bash "$fake/scripts/release.sh" 2.0.0-boundary-test >"$out" 2>&1 || true
  grep -qa "container engine 'no-such-engine' not found" "$out" \
    || fail "release.sh (key from $source) did not pass its key guard: $(cat "$out")"
  if grep -qa -- "$SENTINEL" "$out"; then
    fail "release.sh echoed the sentinel key (key from $source)"
  fi
done
grep -qE '^[[:space:]]*-e CLUBLOG_API_KEY \\$' scripts/release.sh \
  || fail "release.sh does not hand the key to the container by name (-e CLUBLOG_API_KEY)"
if grep -qE -- '-e CLUBLOG_API_KEY=' scripts/release.sh; then
  fail "release.sh puts the key value on the container command line"
fi
grep -q -- "-X ${KEY_LD}=\${CLUBLOG_API_KEY}" scripts/release-rpm.sh \
  || fail "release-rpm.sh does not inject the key with -X ${KEY_LD}"

echo "── [5/6] .env (where the key lives) is ignored by Git ──"
git check-ignore -q .env || fail ".env is not git-ignored: the key could be committed"

echo "── [6/6] the do-not-distribute marker is private-only and key-free ──"
grep -q 'PRIVATE-BUILD-DO-NOT-DISTRIBUTE' nfpm.private.yaml || fail "private nfpm spec lacks the marker"
if grep -q 'PRIVATE-BUILD-DO-NOT-DISTRIBUTE' nfpm.yaml; then
  fail "public nfpm spec contains the private marker"
fi
if grep -qa -- "$SENTINEL" nfpm.private.yaml packaging/PRIVATE-BUILD-DO-NOT-DISTRIBUTE; then
  fail "a spec/marker contains a key-like sentinel"
fi

echo "build-boundary: OK — dogfood binary keyed, keyless build clean, release path requires and injects the key, .env ignored, marker private-only"
