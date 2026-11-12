"""Offline tests for the Electron source guard in scripts/desktop-rpm.sh (W-0027 D1 part 2; codex-079).

    python3 -I scripts/desktop_rpm_guard_test.py

The build refuses every supplied input that could change where Electron comes from, which file
or version is fetched, or how it is checked, before npm ci; and it runs the downloader with a
controlled environment, so an input the refusal does not name still cannot reach it. Nothing is
downloaded: `npm ci` is a stub that writes a fake install.js reporting the environment it was
given, and `nfpm` is a stub that is never reached. The stage and the RPM are never touched.
"""
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
SCRIPT = os.path.join(_here, "desktop-rpm.sh")
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

# What the downloader is allowed to see.
DOWNLOADER_ENV = {"PATH", "HOME", "electron_config_cache"}


class Guard(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp(prefix="sm-desktop-guard-test-")
        self.addCleanup(shutil.rmtree, self.tmp)  # exactly the directory this test made
        self.bin = os.path.join(self.tmp, "bin")
        os.mkdir(self.bin)
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
        pkg = json.dumps({"version": EVER})
        sums = json.dumps({ZIP: PINNED_SHA})
        with open(os.path.join(self.bin, "npm"), "w") as fh:
            fh.write(f"""#!/bin/sh
if [ "$1" = ci ]; then
  : > {shlex.quote(self.reached)}
  mkdir -p node_modules/electron
  cp "${{SM_GUARD_INSTALL:-{self.exits}}}" node_modules/electron/install.js
  printf '%s' {shlex.quote(pkg)} > node_modules/electron/package.json
  printf '%s' {shlex.quote(sums)} > node_modules/electron/checksums.json
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

    def run_build(self, extra=None):
        env = {k: v for k, v in os.environ.items()
               if "electron" not in k.lower() and k.lower() not in
               ("npm_config_platform", "npm_config_arch", "force_no_cache")}
        env.update({"PATH": self.bin + os.pathsep + env.get("PATH", ""), "TMPDIR": self.tmp,
                    "NPM_CONFIG_USERCONFIG": self.userconfig})
        env.update(extra or {})
        return subprocess.run(["bash", SCRIPT], env=env, capture_output=True, text=True,
                              timeout=120)

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
        self.assertTrue(os.path.exists(self.seen), r.stderr)
        with open(self.seen) as fh:
            seen = json.load(fh)
        self.assertLessEqual(set(seen), DOWNLOADER_ENV, f"the downloader saw {sorted(set(seen) - DOWNLOADER_ENV)}")
        self.assertTrue(seen.get("electron_config_cache", "").startswith(self.tmp))
        self.assertEqual(r.returncode, 77, r.stderr)  # the fake install.js's own exit, not nfpm's

    def test_an_archive_without_the_pinned_sha256_is_refused(self):
        # The build's own assertion on the archive it used (codex-079 answer 1), offline: the
        # stubbed downloader succeeds but leaves a different archive. Refused before staging.
        r = self.run_build({"SM_GUARD_INSTALL": self.tampers})
        self.assertTrue(os.path.exists(self.seen), r.stderr)
        self.assertEqual(r.returncode, 1, r.stderr)
        self.assertIn(f"is not the pinned {PINNED_SHA}", r.stderr)
        self.assertNotIn("Staging", r.stdout)


if __name__ == "__main__":
    unittest.main()
