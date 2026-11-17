"""Offline tests for the Electron source guard and archive cache in scripts/desktop-rpm.sh
(W-0027 D1 part 2; codex-079; the cache, 2026-11-17).

    python3 -I scripts/desktop_rpm_guard_test.py

The build refuses every supplied input that could change where Electron comes from, which file
or version is fetched, or how it is checked, before npm ci; and it runs the downloader with a
controlled environment, so an input the refusal does not name still cannot reach it. The
archive is kept between builds, and whichever archive is used, kept or just downloaded, must be
the one archive of the pinned version in the cache and have the pinned SHA-256. Nothing is
downloaded: `npm ci` is a stub that writes a fake install.js reporting the environment it was
given, and `nfpm` is a stub that is never reached.

Each test runs a COPY of the script in a temporary repository of its own (the script, the
version helper and the desktop manifest and lockfile). The script works in the repository it
is in, so its stage, its RPM and its default cache are that copy's: this repository's build
directory is never touched, whatever the script under test does. (A build whose archive is
accepted goes on to clear and fill its stage; run in place, such a test emptied
build/desktop/stage, 2026-11-17.)

What the real downloader does with a cache (uses a matching archive without a download,
replaces one that does not match) is measured in desktop/test/runtime-cache.test.mjs.
"""
import hashlib
import json
import os
import shlex
import shutil
import subprocess
import sys
import tempfile
import unittest

sys.dont_write_bytecode = True  # no __pycache__ beside the scripts

_here = os.path.dirname(os.path.abspath(__file__))
_repo = os.path.dirname(_here)
# What the script needs of the repository to run as far as these tests take it.
COPIED = ["scripts/desktop-rpm.sh", "scripts/version.sh", "desktop/package.json",
          "desktop/package-lock.json"]
REAL_NPM = shutil.which("npm")

# The consumer surface of the pinned downloader (electron/install.js and @electron/get's
# mirrorVar, proxy and getEnv readers), in the spellings each source accepts.
OVERRIDES = [
    "ELECTRON_MIRROR", "ELECTRON_NIGHTLY_MIRROR", "ELECTRON_CUSTOM_DIR",
    "ELECTRON_CUSTOM_FILENAME", "ELECTRON_CUSTOM_VERSION", "ELECTRON_SKIP_BINARY_DOWNLOAD",
    "ELECTRON_OVERRIDE_DIST_PATH", "ELECTRON_GET_USE_PROXY", "ELECTRON_INSTALL_PLATFORM",
    "ELECTRON_INSTALL_ARCH", "electron_config_cache", "electron_use_remote_checksums",
    "npm_config_electron_mirror", "NPM_CONFIG_ELECTRON_MIRROR", "npm_config_electron_nightlymirror",
    "npm_config_electron_nightly_mirror", "npm_config_electron_customdir",
    "npm_config_electron_custom_dir", "npm_config_electron_customfilename",
    "npm_config_electron_custom_filename", "NPM_CONFIG_ELECTRON_CUSTOM_FILENAME",
    "npm_config_electron_customversion", "npm_config_electron_custom_version",
    "npm_config_electron_use_remote_checksums",
    "npm_package_config_electron_mirror", "npm_package_config_electron_customDir",
    "npm_package_config_electron_custom_dir", "npm_package_config_electron_customFilename",
    "npm_package_config_electron_custom_filename", "npm_package_config_electron_customVersion",
    "npm_package_config_electron_custom_version",
    "npm_config_platform", "npm_config_arch", "NPM_CONFIG_PLATFORM", "force_no_cache",
]

EVER = "44.9.3"
ZIP = f"electron-v{EVER}-linux-x64.zip"
PINNED_SHA = "6e7fd2d450d442507b846ace4ae6e2bd17e1b1653e86a033b5231abfc23e6df4"

# A stand-in archive, and where the stand-in downloader keeps it: a directory of its own
# choosing under the cache, as the real one's is.
GOOD = "the pinned archive"
AT = "a/" + ZIP

# What the downloader is allowed to see.
DOWNLOADER_ENV = {"PATH", "HOME", "electron_config_cache"}


class Build(unittest.TestCase):
    """One stubbed build per test: see the module comment."""

    def setUp(self):
        self.tmp = tempfile.mkdtemp(prefix="sm-desktop-guard-test-")
        self.addCleanup(shutil.rmtree, self.tmp)  # exactly the directory this test made
        self.bin = os.path.join(self.tmp, "bin")
        os.mkdir(self.bin)
        self.repo = os.path.join(self.tmp, "repo")
        for rel in COPIED:
            os.makedirs(os.path.dirname(os.path.join(self.repo, rel)), exist_ok=True)
            shutil.copy(os.path.join(_repo, rel), os.path.join(self.repo, rel))
        self.script = os.path.join(self.repo, "scripts", "desktop-rpm.sh")
        self.default_cache = os.path.join(self.repo, "build", "desktop", "electron-cache")
        self.stage = os.path.join(self.repo, "build", "desktop", "stage")
        self.cache = os.path.join(self.tmp, "kept-cache")
        self.reached = os.path.join(self.tmp, "reached-npm-ci")
        self.seen = os.path.join(self.tmp, "downloader-env.json")
        # npm: `ci` is the stub. It records that it ran and writes a fake electron package: a
        # package.json, the pinned checksums.json, and an install.js (chosen by the test through
        # SM_GUARD_INSTALL) that reports the environment it was given. Everything else (config
        # queries) goes to the real npm.
        report = "require('fs').writeFileSync(" + json.dumps(self.seen) + ", JSON.stringify(process.env));"
        self.exits = os.path.join(self.tmp, "install-exits.js")
        with open(self.exits, "w") as fh:
            fh.write(report + " process.exit(77);")
        # A downloader that "succeeds" but leaves an archive whose SHA-256 is not the pinned one.
        self.tampers = os.path.join(self.tmp, "install-tampers.js")
        with open(self.tampers, "w") as fh:
            fh.write(report + " const d = process.env.electron_config_cache + '/x';"
                     " require('fs').mkdirSync(d, {recursive: true});"
                     f" require('fs').writeFileSync(d + '/{ZIP}', 'tampered');")
        # A downloader that finds what it needs in the cache: it writes nothing.
        self.finds = os.path.join(self.tmp, "install-finds.js")
        with open(self.finds, "w") as fh:
            fh.write(report)
        # A downloader that downloads: it leaves SM_GUARD_SERVED at SM_GUARD_AT in the cache.
        self.downloads = os.path.join(self.tmp, "install-downloads.js")
        with open(self.downloads, "w") as fh:
            fh.write(report + " const fs = require('fs'), path = require('path');"
                     " const at = path.join(process.env.electron_config_cache, " + json.dumps(AT) + ");"
                     " fs.mkdirSync(path.dirname(at), {recursive: true});"
                     " fs.writeFileSync(at, " + json.dumps(GOOD) + ");")
        pkg = json.dumps({"version": EVER})
        sums = json.dumps({ZIP: PINNED_SHA})
        self.sums = os.path.join(self.tmp, "checksums.json")
        with open(self.sums, "w") as fh:
            fh.write(sums)
        with open(os.path.join(self.bin, "npm"), "w") as fh:
            fh.write(f"""#!/bin/sh
if [ "$1" = ci ]; then
  : > {shlex.quote(self.reached)}
  mkdir -p node_modules/electron
  cp "${{SM_GUARD_INSTALL:-{self.exits}}}" node_modules/electron/install.js
  printf '%s' {shlex.quote(pkg)} > node_modules/electron/package.json
  cp {shlex.quote(self.sums)} node_modules/electron/checksums.json
  exit 0
fi
exec {shlex.quote(REAL_NPM)} "$@"
""")
        with open(os.path.join(self.bin, "nfpm"), "w") as fh:
            fh.write("#!/bin/sh\nexit 99\n")
        for n in ("npm", "nfpm"):
            os.chmod(os.path.join(self.bin, n), 0o755)
        self.userconfig = os.path.join(self.tmp, "npmrc")
        open(self.userconfig, "w").close()

    def run_build(self, extra=None, cache=True):
        env = {k: v for k, v in os.environ.items()
               if "electron" not in k.lower() and k.lower() not in
               ("npm_config_platform", "npm_config_arch", "force_no_cache")
               and k != "SM_DESKTOP_RUNTIME_CACHE"}
        env.update({"PATH": self.bin + os.pathsep + env.get("PATH", ""), "TMPDIR": self.tmp,
                    "NPM_CONFIG_USERCONFIG": self.userconfig})
        if cache:
            env["SM_DESKTOP_RUNTIME_CACHE"] = self.cache
        env.update(extra or {})
        return subprocess.run(["bash", self.script], env=env, capture_output=True, text=True,
                              timeout=120)

    def seen_env(self, r):
        self.assertTrue(os.path.exists(self.seen), r.stderr)
        with open(self.seen) as fh:
            return json.load(fh)


class Guard(Build):
    def test_control_without_overrides_reaches_npm_ci(self):
        r = self.run_build()
        self.assertTrue(os.path.exists(self.reached), r.stderr)
        self.assertNotIn("refusing", r.stderr)

    def test_every_downloader_override_is_refused_before_npm_ci(self):
        for name in OVERRIDES:
            with self.subTest(name=name):
                if os.path.exists(self.reached):
                    os.remove(self.reached)
                r = self.run_build({name: "https://example.invalid/x"})
                self.assertEqual(r.returncode, 1, r.stderr)
                self.assertIn("refusing", r.stderr)
                self.assertIn(name, r.stderr)
                self.assertFalse(os.path.exists(self.reached), f"{name} reached npm ci")

    def test_npm_configuration_overrides_are_refused(self):
        for key in ("electron_mirror", "electron_custom_filename", "electron-custom-version",
                    "ELECTRON_CUSTOM_DIR", "electron_use_remote_checksums", "platform", "arch"):
            with self.subTest(key=key):
                with open(self.userconfig, "w") as fh:
                    fh.write(f"{key}=https://example.invalid/x\n")
                r = self.run_build()
                self.assertEqual(r.returncode, 1, r.stderr)
                self.assertIn("npm config", r.stderr)
                self.assertFalse(os.path.exists(self.reached), f"npm config {key} reached npm ci")

    def test_the_downloader_sees_only_a_controlled_environment(self):
        # Inputs the refusal does not name (a future reader, a loader hook) still cannot reach it.
        r = self.run_build({"SM_GUARD_UNNAMED": "1", "NODE_OPTIONS": "--no-deprecation",
                            "HTTPS_PROXY": "http://example.invalid:1"})
        seen = self.seen_env(r)
        self.assertLessEqual(set(seen), DOWNLOADER_ENV, f"the downloader saw {sorted(set(seen) - DOWNLOADER_ENV)}")
        self.assertEqual(seen.get("electron_config_cache"), self.cache)
        # Its HOME is still an empty one of the build's own, not the caller's.
        self.assertTrue(seen.get("HOME", "").startswith(self.tmp), seen.get("HOME"))
        self.assertNotEqual(seen.get("HOME"), os.environ.get("HOME"))
        self.assertEqual(r.returncode, 77, r.stderr)  # the fake install.js's own exit, not nfpm's

    def test_an_archive_without_the_pinned_sha256_is_refused(self):
        # The build's own assertion on the archive it used (codex-079 answer 1), offline: the
        # stubbed downloader succeeds but leaves a different archive. Refused before staging.
        r = self.run_build({"SM_GUARD_INSTALL": self.tampers})
        self.assertTrue(os.path.exists(self.seen), r.stderr)
        self.assertEqual(r.returncode, 1, r.stderr)
        self.assertIn(f"is not the pinned {PINNED_SHA}", r.stderr)
        self.assertNotIn("Staging", r.stdout)
        self.assertFalse(os.path.exists(self.stage), "a refused archive reached the stage")


class Cache(Build):
    """The archive is kept between builds, and only the pinned one is ever used."""

    def keep(self, content, at=None):
        path = os.path.join(self.cache, at or AT)
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w") as fh:
            fh.write(content)
        return path

    def pin(self, content):
        with open(self.sums, "w") as fh:
            json.dump({ZIP: hashlib.sha256(content.encode()).hexdigest()}, fh)

    def assert_accepted(self, r):
        # An accepted archive is staged from. (The stubs leave no runtime to stage, so the
        # build then fails, in the copy's own stage.)
        self.assertIn("(matches checksums.json)", r.stdout, r.stderr)
        self.assertIn("Staging", r.stdout, r.stderr)

    def assert_refused(self, r):
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertNotIn("(matches checksums.json)", r.stdout)
        self.assertNotIn("Staging", r.stdout)
        self.assertFalse(os.path.exists(self.stage), "a refused archive reached the stage")

    def test_the_cache_is_the_builds_own_directory_in_the_repository(self):
        r = self.run_build(cache=False)
        self.assertEqual(self.seen_env(r).get("electron_config_cache"), self.default_cache)
        self.assertEqual(r.returncode, 77, r.stderr)

    def test_a_cache_given_by_a_relative_path_is_refused(self):
        r = self.run_build({"SM_DESKTOP_RUNTIME_CACHE": "kept-cache"})
        self.assertEqual(r.returncode, 1, r.stderr)
        self.assertIn("SM_DESKTOP_RUNTIME_CACHE", r.stderr)
        self.assertFalse(os.path.exists(self.reached), "a relative cache reached npm ci")

    def test_a_kept_archive_with_the_pinned_sha256_is_used_and_no_download_is_announced(self):
        self.pin(GOOD)
        kept = self.keep(GOOD)
        r = self.run_build({"SM_GUARD_INSTALL": self.finds})
        self.assertIn("nothing to download", r.stdout, r.stderr)
        self.assertNotIn("downloading", r.stdout)
        # The cache the build looked in is the one the downloader was given: an announcement
        # about one cache says nothing about a download made with another.
        self.assertEqual(self.seen_env(r).get("electron_config_cache"), self.cache)
        self.assert_accepted(r)
        with open(kept) as fh:
            self.assertEqual(fh.read(), GOOD, "the build changed the archive it kept")

    def test_an_archive_that_is_not_kept_is_downloaded_and_kept_for_the_next_build(self):
        self.pin(GOOD)
        other = self.keep("an older version", at="a/electron-v44.9.2-linux-x64.zip")
        r = self.run_build({"SM_GUARD_INSTALL": self.downloads})
        self.assertIn("not in the cache; downloading", r.stdout, r.stderr)
        self.assert_accepted(r)
        # It outlives the build, which removes only its own temporary directory.
        with open(os.path.join(self.cache, AT)) as fh:
            self.assertEqual(fh.read(), GOOD)
        self.assertTrue(os.path.exists(other), "another version's archive was removed")
        # The next build finds it.
        again = self.run_build({"SM_GUARD_INSTALL": self.finds})
        self.assertIn("nothing to download", again.stdout, again.stderr)

    def test_a_kept_archive_that_is_not_the_pinned_one_is_announced_and_downloaded_again(self):
        self.pin(GOOD)
        kept = self.keep("changed after it was kept")
        r = self.run_build({"SM_GUARD_INSTALL": self.downloads})
        self.assertIn("not the pinned one; downloading it again", r.stdout, r.stderr)
        self.assertNotIn("nothing to download", r.stdout)
        self.assert_accepted(r)
        with open(kept) as fh:
            self.assertEqual(fh.read(), GOOD)

    def test_a_kept_archive_that_is_not_the_pinned_one_and_is_not_replaced_is_refused(self):
        # The build's own assertion still stands behind the downloader's: if the downloader
        # leaves the wrong archive where it was, nothing is staged from it.
        self.pin(GOOD)
        self.keep("changed after it was kept")
        r = self.run_build({"SM_GUARD_INSTALL": self.finds})
        self.assertIn("is not the pinned", r.stderr)
        self.assert_refused(r)

    def test_two_archives_of_the_pinned_version_are_refused(self):
        # Which one the downloader used cannot be told from outside it, so neither is trusted,
        # even though one of them is the pinned archive.
        self.pin(GOOD)
        self.keep(GOOD)
        self.keep("a second one", at="b/" + ZIP)
        r = self.run_build({"SM_GUARD_INSTALL": self.finds})
        self.assertIn("more than one", r.stderr)
        self.assertIn(self.cache, r.stderr)
        self.assertNotIn("nothing to download", r.stdout)
        self.assert_refused(r)

    def test_a_download_that_leaves_no_archive_is_refused(self):
        self.pin(GOOD)
        r = self.run_build({"SM_GUARD_INSTALL": self.finds})
        self.assertIn("not in the cache; downloading", r.stdout, r.stderr)
        self.assertIn("is not in the cache", r.stderr)
        self.assert_refused(r)


if __name__ == "__main__":
    unittest.main()
