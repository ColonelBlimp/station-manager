#!/usr/bin/env bash
# Build the Station Manager desktop app RPM (W-0027 D1): build/release/station-manager-desktop.x86_64.rpm.
#
# The shell holds no secret, so this is the one, public build path (no key, no private marker).
# Electron comes from the lockfile-pinned npm install (operator ruling Q5): a clean `npm ci` of
# desktop/package-lock.json in a temporary directory, then Electron's own install.js with a
# fresh, private download cache, so the archive is downloaded and verified against the SHA-256
# in Electron's bundled checksums.json; this script then asserts that SHA-256 of the archive it
# used (the same trust source, not independent provenance). Overrides of that source or check are
# refused, and the downloader runs with a controlled environment. The version is SM's
# own, from scripts/version.sh, exactly as the daemon RPM built from the same commit
# (operator ruling Q4).
#
# Usage: scripts/desktop-rpm.sh        (needs node, npm, nfpm, network for the download)
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

WORK=$(mktemp -d "${TMPDIR:-/tmp}/sm-desktop-rpm-XXXXXX")
trap 'rm -rf "$WORK"' EXIT # exactly the directory this run made

echo "── [1/3] Clean npm ci (lockfile-pinned Electron, fresh verified download) ──"
cp desktop/package.json desktop/package-lock.json "$WORK/"
mkdir "$WORK/cache"
(cd "$WORK" && npm ci --ignore-scripts --no-audit --no-fund --loglevel=error) # the lockfile has no install scripts
# Electron 44 has no install script: its install.js downloads on first use. Run it here, with
# this run's own empty cache, so the archive is always downloaded and checked now.
# Its environment is exactly PATH, HOME (an empty one) and the cache: nothing else the caller
# set can reach the downloader.
mkdir "$WORK/home"
(cd "$WORK/node_modules/electron" &&
  env -i PATH="$PATH" HOME="$WORK/home" electron_config_cache="$WORK/cache" node install.js)
DIST="$WORK/node_modules/electron/dist"
EVER=$(node -p "require('$WORK/node_modules/electron/package.json').version")
ZIP=$(find "$WORK/cache" -type f -name "electron-v${EVER}-linux-x64.zip" | head -n 1)
[ -n "$ZIP" ] || { echo "error: the downloaded Electron archive is not in the private cache" >&2; exit 1; }
SUM=$(sha256sum "$ZIP" | cut -d' ' -f1)
WANT=$(node -p "require('$WORK/node_modules/electron/checksums.json')['electron-v${EVER}-linux-x64.zip']")
[ "$SUM" = "$WANT" ] || { echo "error: Electron archive SHA-256 $SUM is not the pinned $WANT" >&2; exit 1; }
echo "Electron ${EVER}: electron-v${EVER}-linux-x64.zip sha256 ${SUM} (matches checksums.json)"

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
