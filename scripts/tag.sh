#!/usr/bin/env bash
# Create an annotated release tag, guarded so a tag always marks a clean,
# reproducible commit:
#
#   1. The working tree must be clean. We use `git status --porcelain`
#      (tracked AND untracked) rather than `git describe --dirty` — the
#      latter ignores untracked files, so a stray un-added file would still
#      let you tag a state you can't reproduce from a fresh clone.
#   2. The tag must not already exist (clear refusal instead of git's error,
#      and never a silent clobber).
#   3. The version must look like a release version (vMAJOR.MINOR.PATCH with
#      an optional -prerelease).
#
# It does NOT push — pushing a tag stays a deliberate step. The push command
# is printed on success.
#
# Usage:
#   scripts/tag.sh <version> [message...]
#   scripts/tag.sh v2.0.0-alpha.2
#   scripts/tag.sh 2.0.0 "first stable release"   (leading 'v' added)

set -euo pipefail

if [ $# -lt 1 ]; then
  echo "usage: $0 <version> [message...]" >&2
  exit 2
fi

raw="$1"
shift
msg="$*" # remaining args = annotation message (optional)

# Normalise to a leading 'v' — existing tags are v-prefixed (v1.0.0, v2.0.0-alpha.1).
ver="v${raw#v}"

# Light semver-ish validation: vMAJOR.MINOR.PATCH with an optional -prerelease.
if ! printf '%s' "$ver" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$'; then
  echo "error: '$ver' is not a vMAJOR.MINOR.PATCH[-prerelease] version" >&2
  exit 1
fi

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

# Guard 1: clean working tree (tracked changes AND untracked files).
if [ -n "$(git status --porcelain)" ]; then
  echo "error: working tree is not clean — commit or stash before tagging:" >&2
  git status --short >&2
  exit 1
fi

# Guard 2: tag must not already exist.
if git rev-parse -q --verify "refs/tags/$ver" >/dev/null 2>&1; then
  echo "error: tag $ver already exists" >&2
  exit 1
fi

[ -n "$msg" ] || msg="Release $ver"
git tag -a "$ver" -m "$msg"
echo "Created annotated tag $ver at $(git rev-parse --short HEAD)."
echo "Push it with:  git push origin $ver"
