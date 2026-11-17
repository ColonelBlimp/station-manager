#!/usr/bin/env bash
# Build the Station Manager desktop app RPM (W-0027 D1): build/release/station-manager-desktop.x86_64.rpm.
#
# The shell holds no secret, so this is the one, public build path (no key, no private marker).
# Electron comes from the lockfile-pinned npm install (operator ruling Q5): a clean `npm ci` of
# desktop/package-lock.json in a temporary directory, then Electron's own install.js, which
# verifies the archive against the SHA-256 in Electron's bundled checksums.json; this script
# then asserts that SHA-256 of the archive it used (the same trust source, not independent
# provenance). Overrides of that source or check are refused, and the downloader runs with a
# controlled environment. The version is SM's own, from scripts/version.sh, exactly as the
# daemon RPM built from the same commit (operator ruling Q4).
#
# The archive is kept between builds, in build/desktop/electron-cache (2026-11-17): it is
# downloaded once for each Electron version the lockfile pins, not at every build. Keeping it
# changes what is fetched, not what is trusted. A kept archive is used only if it has the
# pinned SHA-256: the downloader checks that and downloads again if it does not
# (desktop/test/runtime-cache.test.mjs measures both), and this script's own assertion below
# stands behind it. A new pinned version has another file name, so it is downloaded. Earlier
# versions' archives stay in the cache until someone removes them.
#
# SM_DESKTOP_RUNTIME_CACHE puts the cache elsewhere (an absolute path; the tests use it). It
# is not one of the refused overrides because it cannot change which archive is accepted.
#
# Usage: scripts/desktop-rpm.sh        (needs node, npm, nfpm; network when the pinned
#                                       Electron is not in the cache)
# Check: python3 -I scripts/desktop-rpm-check.py <rpm> --version <v> --app-version <v>
#        (task rpm:desktop runs both)

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

for tool in node npm nfpm; do
  command -v "$tool" >/dev/null 2>&1 || { echo "error: $tool not in PATH" >&2; exit 1; }
done

# Refuse every way to change where Electron comes from, which file or version is fetched, or how
# it is checked (codex-079). The pinned downloader (electron/install.js, @electron/get) reads
# ELECTRON_*, electron_*, npm_config_electron_*, NPM_CONFIG_ELECTRON_* and
# npm_package_config_electron_* in several spellings, plus npm_config_platform/arch and
# force_no_cache: so refuse by pattern, case-insensitively, not by a list of names. npm
# configuration is refused the same way. The downloader then runs with a controlled
# environment (below), so an input not named here still cannot reach it.
refused=()
while IFS= read -r v; do
  lc=${v,,}
  case "$lc" in
    *electron*|npm_config_platform|npm_config_arch|force_no_cache) refused+=("$v") ;;
  esac
done < <(compgen -e)
npmcfg=$(cd desktop && npm config ls --json 2>/dev/null) || { echo "error: npm config ls failed" >&2; exit 1; }
while IFS= read -r k; do
  [ -n "$k" ] && refused+=("npm config $k")
done < <(node -e '
const cfg = JSON.parse(process.argv[1]);
for (const k of Object.keys(cfg)) {
  const n = k.toLowerCase().replace(/-/g, "_");
  if (n.includes("electron") || n === "platform" || n === "arch") console.log(k);
}' "$npmcfg")
if [ "${#refused[@]}" -gt 0 ]; then
  echo "error: refusing to build with an Electron source or checksum override: ${refused[*]}" >&2
  exit 1
fi

# shellcheck source=scripts/version.sh
. "$(dirname "$0")/version.sh"
VERSION=$(sm_git_version)
RPM_VERSION=$(sm_rpm_version "$VERSION")
OUTPUT="build/release/station-manager-desktop.x86_64.rpm"
STAGE="build/desktop/stage"
CACHE="${SM_DESKTOP_RUNTIME_CACHE:-$REPO_ROOT/build/desktop/electron-cache}"
case "$CACHE" in
  /*) ;;
  *) echo "error: SM_DESKTOP_RUNTIME_CACHE must be an absolute path, not '$CACHE'" >&2; exit 1 ;;
esac

WORK=$(mktemp -d "${TMPDIR:-/tmp}/sm-desktop-rpm-XXXXXX")
trap 'rm -rf "$WORK"' EXIT # exactly the directory this run made

echo "── [1/3] Clean npm ci (lockfile-pinned Electron; its archive verified, downloaded when not kept) ──"
cp desktop/package.json desktop/package-lock.json "$WORK/"
(cd "$WORK" && npm ci --ignore-scripts --no-audit --no-fund --loglevel=error) # the lockfile has no install scripts
DIST="$WORK/node_modules/electron/dist"
EVER=$(node -p "require('$WORK/node_modules/electron/package.json').version")
ZIPNAME="electron-v${EVER}-linux-x64.zip"
WANT=$(node -p "require('$WORK/node_modules/electron/checksums.json')['${ZIPNAME}']")

# kept_archives lists every archive of the pinned version in the cache, into ARCHIVES. The
# downloader keeps it in a directory it names itself, so it is found by name. More than one
# is refused: which of them the downloader used cannot be told from outside it.
kept_archives() {
  ARCHIVES=()
  [ -d "$CACHE" ] || return 0
  mapfile -t ARCHIVES < <(find "$CACHE" -type f -name "$ZIPNAME")
  if [ "${#ARCHIVES[@]}" -gt 1 ]; then
    echo "error: more than one $ZIPNAME in $CACHE; remove that directory and build again" >&2
    exit 1
  fi
}

# Say what this build will do, from this script's own look at the cache.
kept_archives
if [ "${#ARCHIVES[@]}" -eq 0 ]; then
  echo "Electron ${EVER}: not in the cache; downloading it (kept in $CACHE for later builds)."
elif [ "$(sha256sum "${ARCHIVES[0]}" | cut -d' ' -f1)" = "$WANT" ]; then
  echo "Electron ${EVER}: the kept archive has the pinned SHA-256; nothing to download."
else
  echo "Electron ${EVER}: the kept archive is not the pinned one; downloading it again."
fi

# Electron 44 has no install script: its install.js downloads on first use. Run it here. It
# uses the kept archive only after checking it against checksums.json, and downloads otherwise.
# Its environment is exactly PATH, HOME (an empty one) and the cache: nothing else the caller
# set can reach the downloader.
mkdir "$WORK/home"
(cd "$WORK/node_modules/electron" &&
  env -i PATH="$PATH" HOME="$WORK/home" electron_config_cache="$CACHE" node install.js)

# Kept or just downloaded, the one archive of this version must have the pinned SHA-256.
kept_archives
[ "${#ARCHIVES[@]}" -eq 1 ] || { echo "error: the Electron archive $ZIPNAME is not in the cache $CACHE" >&2; exit 1; }
SUM=$(sha256sum "${ARCHIVES[0]}" | cut -d' ' -f1)
[ "$SUM" = "$WANT" ] || { echo "error: Electron archive SHA-256 $SUM is not the pinned $WANT" >&2; exit 1; }
echo "Electron ${EVER}: ${ZIPNAME} sha256 ${SUM} (matches checksums.json)"

echo "── [2/3] Staging ${STAGE} (version ${VERSION}) ──"
rm -rf "$STAGE" # this script's own output directory
mkdir -p "$STAGE"
cp -a "$DIST" "$STAGE/lib"
mv "$STAGE/lib/electron" "$STAGE/lib/station-manager-desktop"
mv "$STAGE/lib/LICENSE" "$STAGE/LICENSE.electron"
mv "$STAGE/lib/LICENSES.chromium.html" "$STAGE/LICENSES.chromium.html"
rm -f "$STAGE/lib/resources/default_app.asar" # a broken app must fail, not open Electron's demo
APPDIR="$STAGE/lib/resources/app"
mkdir -p "$APPDIR"
cp -a desktop/src desktop/assets "$APPDIR/"
node -e '
const [src, out, version] = process.argv.slice(1);
const p = require(src);
const prod = { name: p.name, version, description: p.description, license: p.license,
               type: p.type, main: p.main };
require("fs").writeFileSync(out, JSON.stringify(prod, null, 4) + "\n");
' "$REPO_ROOT/desktop/package.json" "$APPDIR/package.json" "$VERSION"
chmod -R u+rwX,go+rX,go-w "$STAGE"
printf 'electron %s\nsha256 %s\n' "$EVER" "$SUM" > build/desktop/electron-source.txt

echo "── [3/3] Packaging ${OUTPUT} (RPM version ${RPM_VERSION}) ──"
mkdir -p build/release
VERSION="$RPM_VERSION" nfpm pkg -f nfpm-desktop.yaml -p rpm -t "$OUTPUT"

echo
echo "Built station-manager-desktop ${VERSION} (Electron ${EVER}):"
ls -lh "$OUTPUT"
