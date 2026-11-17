"""The package version reaches the RPM exactly as it was given (CI run 44033161733).

    python3 -I scripts/nfpm_version_test.py

nfpm reads a version made only of digits as a number and pads it: 7104471 became
7104471.0.0, while the desktop package's requirement on the daemon stayed 7104471-1, so the
pair could not be installed. That is the version of any build from a checkout with no tags
whose short commit hash has no letter in it, as on CI. Each nfpm spec therefore says
`version_schema: none`, and this test packages a three-line stand-in carrying the spec's own
version lines to show what nfpm then writes. Nothing of the real packages is built.
"""
import os
import re
import shutil
import subprocess
import sys
import tempfile
import unittest

sys.dont_write_bytecode = True  # no __pycache__ beside the scripts

_root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SPECS = ["nfpm.yaml", "nfpm-desktop.yaml", "nfpm-smcloud.yaml", "nfpm.private.yaml"]

# The forms a build's version takes: a short hash with no letter (the failure), one with a
# leading zero, an ordinary short hash, and the tagged forms the operator's builds have.
VERSIONS = ["7104471", "0123456", "41d2546", "2.0.0~alpha.5.257.g7104471c", "2.0.0~alpha.5", "2.0.0"]


def top_level(spec, key):
    """The spec's own top-level `key: value` line, or None."""
    with open(os.path.join(_root, spec), encoding="utf-8") as fh:
        for line in fh:
            if re.match(rf"{re.escape(key)}:\s", line):
                return line.rstrip("\n")
    return None


def versioned_depends(spec):
    """The spec's `depends` items that name the version, as written."""
    with open(os.path.join(_root, spec), encoding="utf-8") as fh:
        return [m.group(0) for m in re.finditer(r'^  - "[^"\n]*\$\{VERSION\}[^"\n]*"$', fh.read(), re.M)]


class Version(unittest.TestCase):
    def test_every_spec_takes_the_version_as_written(self):
        for spec in SPECS:
            with self.subTest(spec=spec):
                self.assertEqual(top_level(spec, "version"), 'version: "${VERSION}"')
                self.assertEqual(top_level(spec, "version_schema"), "version_schema: none",
                                 f"{spec} lets nfpm rewrite an all-digit version")

    def test_no_other_spec_exists_unlisted(self):
        found = sorted(f for f in os.listdir(_root) if re.fullmatch(r"nfpm.*\.ya?ml", f))
        self.assertEqual(found, sorted(SPECS), "an nfpm spec is not covered by this test")

    def test_the_packaged_version_is_the_one_given(self):
        nfpm, rpm = shutil.which("nfpm"), shutil.which("rpm")
        if not nfpm or not rpm:
            self.fail("NOT RUN: this test needs nfpm and rpm on PATH")
        for spec in SPECS:
            # The spec's own lines: its release too when it states one, and none when it
            # leaves the default, as the daemon's spec does.
            lines = [l for l in (top_level(spec, k) for k in ("version", "version_schema", "release")) if l]
            depends = versioned_depends(spec)
            with tempfile.TemporaryDirectory() as work:
                with open(os.path.join(work, "f.txt"), "w", encoding="utf-8") as fh:
                    fh.write("x\n")
                with open(os.path.join(work, "spec.yaml"), "w", encoding="utf-8") as fh:
                    fh.write("name: nfpm-version-test\narch: amd64\nplatform: linux\n"
                             'maintainer: "t <t@example.invalid>"\n'
                             'description: "t"\nlicense: "GPL-3.0-only"\n')
                    fh.write("\n".join(lines) + "\n")
                    if depends:
                        fh.write("depends:\n" + "\n".join(depends) + "\n")
                    fh.write("contents:\n  - src: ./f.txt\n    dst: /opt/nfpm-version-test/f.txt\n")
                for version in VERSIONS:
                    with self.subTest(spec=spec, version=version):
                        out = os.path.join(work, "out.rpm")
                        if os.path.exists(out):
                            os.remove(out)
                        built = subprocess.run(
                            [nfpm, "pkg", "-f", "spec.yaml", "-p", "rpm", "-t", out], cwd=work,
                            env={**os.environ, "VERSION": version}, capture_output=True, text=True)
                        self.assertEqual(built.returncode, 0, built.stderr)
                        got = subprocess.run([rpm, "-qp", "--qf", "%{VERSION}|%{RELEASE}", out],
                                             capture_output=True, text=True).stdout
                        self.assertEqual(got, f"{version}|1")
                        requires = subprocess.run([rpm, "-qp", "--requires", out],
                                                  capture_output=True, text=True).stdout.splitlines()
                        for item in depends:
                            want = item.strip()[3:-1].replace("${VERSION}", version)
                            self.assertIn(want, requires, "a requirement does not name the version given")


if __name__ == "__main__":
    unittest.main()
