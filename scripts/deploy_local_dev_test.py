"""Tests for scripts/deploy-local-dev.sh:  python3 -I scripts/deploy_local_dev_test.py

The deploy stops the station's daemon, so every way the install can be refused has to be found
BEFORE the stop, and a failure after it must not leave the daemon down. The desktop package
requires exactly the daemon version it was built with (ADR 0102 ruling Q4), so once it is
installed the two packages can only be upgraded in one transaction.

Nothing here touches the station: the script is copied into a temporary "repository" (it finds
its root from its own location), the build is a stub that writes a fake RPM, and systemctl,
sudo, rpm, ss and sleep are stand-ins first on PATH that record every call and keep their
state in that directory. The stand-in rpm models the one rule that matters: an installed
desktop package refuses a daemon of another version unless the desktop RPM of that version is
in the same transaction.
"""
import os
import shutil
import stat
import subprocess
import sys
import tempfile
import unittest

sys.dont_write_bytecode = True  # no __pycache__ beside the scripts

_here = os.path.dirname(os.path.abspath(__file__))
SCRIPT = os.path.join(_here, "deploy-local-dev.sh")

DAEMON_RPM = "build/private/station-manager-dev.x86_64.rpm"
DESKTOP_RPM = "build/release/station-manager-desktop.x86_64.rpm"
OLD, NEW = "2.0.0~alpha.5.229.g79c4a285", "2.0.0~alpha.5.230.gecdbceb3"

STUBS = {
    # The unit's state is a file; every call is logged.
    "systemctl": r'''#!/bin/sh
echo "systemctl $*" >> "$SM_T/calls.log"
[ "$1" = "--user" ] && shift
case "$1" in
  is-active) [ -f "$SM_T/active" ] ;;
  stop) rm -f "$SM_T/active" ;;
  start) [ -f "$SM_T/start-fails" ] && exit 1; : > "$SM_T/active" ;;
  *) exit 0 ;;
esac
''',
    # :8080 is held exactly while the unit is active.
    "ss": r'''#!/bin/sh
[ -f "$SM_T/active" ] && echo "LISTEN 0 4096 127.0.0.1:8080 0.0.0.0:*"
exit 0
''',
    "sudo": r'''#!/bin/sh
echo "sudo $*" >> "$SM_T/calls.log"
exec "$@"
''',
    "sleep": "#!/bin/sh\nexit 0\n",
    # A fake RPM file holds its version on its first line and its package name on its second.
    "rpm": r'''#!/bin/sh
echo "rpm $*" >> "$SM_T/calls.log"
version_of() { sed -n 1p "$1"; }
name_of() { sed -n 2p "$1"; }
case "$1" in
  -q)
    shift
    [ "$1" = "--qf" ] && shift 2
    f="$SM_T/installed-$1"
    [ -f "$f" ] || { echo "package $1 is not installed"; exit 1; }
    cat "$f"; exit 0 ;;
  -qp)
    shift
    [ "$1" = "--qf" ] && shift 2
    version_of "$1"; exit 0 ;;
esac
# A transaction: -U --test --replacepkgs FILES, or -Uvh --replacepkgs FILES.
mode="$1"; shift
test_only=no
[ "$1" = "--test" ] && { test_only=yes; shift; }
[ "$1" = "--replacepkgs" ] && shift
daemon=""; desktop=""
for f in "$@"; do
  case "$(name_of "$f")" in
    station-manager) daemon=$(version_of "$f") ;;
    station-manager-desktop) desktop=$(version_of "$f") ;;
  esac
done
# The dependency rule: the desktop package, installed or in this transaction, needs exactly
# its own version of the daemon.
have=""; [ -f "$SM_T/installed-station-manager-desktop" ] && have=$(cat "$SM_T/installed-station-manager-desktop")
after_desktop="${desktop:-$have}"
if [ -n "$after_desktop" ] && [ -n "$daemon" ] && [ "$after_desktop" != "$daemon" ]; then
  echo "error: Failed dependencies:" >&2
  echo "	station-manager = $after_desktop-1 is needed by station-manager-desktop-$after_desktop-1.x86_64" >&2
  exit 1
fi
if [ -f "$SM_T/transaction-fails" ]; then cat "$SM_T/transaction-fails" >&2; exit 1; fi
[ "$test_only" = yes ] && exit 0
if [ -f "$SM_T/install-fails" ]; then echo "error: the install failed" >&2; exit 1; fi
[ -n "$daemon" ] && printf '%s' "$daemon" > "$SM_T/installed-station-manager"
[ -n "$desktop" ] && printf '%s' "$desktop" > "$SM_T/installed-station-manager-desktop"
exit 0
''',
}

DEV_RPM_STUB = r'''#!/bin/sh
echo "dev-rpm" >> "$SM_T/calls.log"
mkdir -p build/private
printf '%s\nstation-manager\n' "$SM_T_VERSION" > build/private/station-manager-dev.x86_64.rpm
'''
VERSION_STUB = r'''sm_git_version() { printf '%s' "$SM_T_VERSION"; }
sm_rpm_version() { printf '%s' "$1"; }
'''


class Deploy(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp(prefix="sm-deploy-test-")
        self.addCleanup(shutil.rmtree, self.tmp)  # exactly the directory this test made
        self.repo = os.path.join(self.tmp, "repo")
        self.state = os.path.join(self.tmp, "state")
        self.bin = os.path.join(self.tmp, "bin")
        for d in (os.path.join(self.repo, "scripts"), self.state, self.bin):
            os.makedirs(d)
        shutil.copy(SCRIPT, os.path.join(self.repo, "scripts", "deploy-local-dev.sh"))
        self._write(os.path.join(self.repo, "scripts", "dev-rpm.sh"), DEV_RPM_STUB)
        self._write(os.path.join(self.repo, "scripts", "version.sh"), VERSION_STUB)
        for name, body in STUBS.items():
            self._write(os.path.join(self.bin, name), body, executable=True)

    def _write(self, path, body, executable=False):
        with open(path, "w") as fh:
            fh.write(body)
        if executable:
            os.chmod(path, os.stat(path).st_mode | stat.S_IXUSR)

    # ── the station's state ──────────────────────────────────────────────────────

    def given(self, daemon=None, desktop=None, active=False, desktop_rpm=None):
        """Installed versions (None = not installed), the unit's state, and a desktop RPM file."""
        for name, version in (("station-manager", daemon), ("station-manager-desktop", desktop)):
            if version:
                self._write(os.path.join(self.state, "installed-" + name), version)
        if active:
            self._write(os.path.join(self.state, "active"), "")
        if desktop_rpm:
            path = os.path.join(self.repo, DESKTOP_RPM)
            os.makedirs(os.path.dirname(path))
            self._write(path, desktop_rpm + "\nstation-manager-desktop\n")

    def flag(self, name, text=""):
        self._write(os.path.join(self.state, name), text)

    def deploy(self, version=NEW):
        env = dict(os.environ, PATH=self.bin + os.pathsep + os.environ["PATH"], SM_T=self.state,
                   SM_T_VERSION=version)
        return subprocess.run(["bash", os.path.join(self.repo, "scripts", "deploy-local-dev.sh")],
                              env=env, capture_output=True, text=True, timeout=60)

    def calls(self):
        path = os.path.join(self.state, "calls.log")
        return open(path).read().splitlines() if os.path.exists(path) else []

    def installed(self, name):
        path = os.path.join(self.state, "installed-" + name)
        return open(path).read() if os.path.exists(path) else None

    def active(self):
        return os.path.exists(os.path.join(self.state, "active"))

    def index(self, calls, needle):
        hits = [i for i, c in enumerate(calls) if needle in c]
        self.assertTrue(hits, f"no call containing {needle!r} in {calls}")
        return hits[0]

    def assertNeverStopped(self, r):
        self.assertFalse(any("systemctl --user stop" in c for c in self.calls()),
                         f"the daemon was stopped:\n{r.stdout}{r.stderr}")
        self.assertFalse(any(c.startswith("sudo ") for c in self.calls()), "something was installed")

    # ── the tests ─────────────────────────────────────────────────────────────────

    def test_daemon_alone_when_no_desktop_package_is_installed(self):
        self.given(daemon=OLD, active=True)
        r = self.deploy()
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        calls = self.calls()
        install = self.index(calls, "sudo rpm -Uvh --replacepkgs")
        self.assertIn(DAEMON_RPM, calls[install])
        self.assertNotIn(DESKTOP_RPM, calls[install])
        self.assertEqual(self.installed("station-manager"), NEW)
        self.assertTrue(self.active())
        # The same transaction was tried, without installing, before the daemon was stopped.
        self.assertLess(self.index(calls, "rpm -U --test --replacepkgs " + DAEMON_RPM),
                        self.index(calls, "systemctl --user stop smd"))
        self.assertLess(self.index(calls, "systemctl --user stop smd"), install)

    def test_the_pair_is_upgraded_in_one_transaction(self):
        # The operator's incident, 2026-11-12: both installed at one version, both rebuilt at the
        # next. The daemon alone is refused; the pair is not.
        self.given(daemon=OLD, desktop=OLD, active=True, desktop_rpm=NEW)
        r = self.deploy()
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        calls = self.calls()
        installs = [c for c in calls if c.startswith("sudo rpm -Uvh")]
        self.assertEqual(len(installs), 1, calls)
        self.assertIn(DAEMON_RPM, installs[0])
        self.assertIn(DESKTOP_RPM, installs[0])
        self.assertEqual((self.installed("station-manager"), self.installed("station-manager-desktop")),
                         (NEW, NEW))
        self.assertTrue(self.active())
        test = self.index(calls, "rpm -U --test --replacepkgs")
        self.assertIn(DESKTOP_RPM, calls[test])
        self.assertLess(test, self.index(calls, "systemctl --user stop smd"))

    def test_no_desktop_rpm_is_refused_before_the_build_and_the_stop(self):
        self.given(daemon=OLD, desktop=OLD, active=True)
        r = self.deploy()
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertNotIn("dev-rpm", self.calls())
        self.assertNeverStopped(r)
        self.assertTrue(self.active())
        self.assertIn("task rpm:desktop", r.stderr)
        self.assertIn(NEW, r.stderr)

    def test_a_desktop_rpm_of_another_version_is_refused_before_the_build_and_the_stop(self):
        self.given(daemon=OLD, desktop=OLD, active=True, desktop_rpm=OLD)
        r = self.deploy()
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertNotIn("dev-rpm", self.calls())
        self.assertNeverStopped(r)
        self.assertTrue(self.active())
        self.assertIn("task rpm:desktop", r.stderr)
        self.assertIn(OLD, r.stderr)
        self.assertIn(NEW, r.stderr)

    def test_the_same_version_again_needs_no_desktop_rpm(self):
        # Rebuilding the commit already installed: the daemon alone satisfies the desktop package.
        self.given(daemon=NEW, desktop=NEW, active=True)
        r = self.deploy()
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        install = [c for c in self.calls() if c.startswith("sudo rpm -Uvh")]
        self.assertEqual(len(install), 1)
        self.assertNotIn(DESKTOP_RPM, install[0])
        self.assertTrue(self.active())

    def test_a_refused_transaction_stops_nothing(self):
        # Whatever rpm's reason, it is found by the dry run: the daemon keeps running.
        self.given(daemon=OLD, active=True)
        self.flag("transaction-fails", "error: file /usr/bin/smd conflicts with another package\n")
        r = self.deploy()
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertNeverStopped(r)
        self.assertTrue(self.active())
        self.assertIn("conflicts with another package", r.stdout + r.stderr)
        self.assertIn("still running", r.stderr)
        self.assertEqual(self.installed("station-manager"), OLD)

    def test_a_failed_install_restarts_the_daemon_that_was_running(self):
        self.given(daemon=OLD, active=True)
        self.flag("install-fails")
        r = self.deploy()
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        calls = self.calls()
        self.assertLess(self.index(calls, "systemctl --user stop smd"),
                        self.index(calls, "systemctl --user start smd"))
        self.assertTrue(self.active(), "the station's daemon was left stopped")
        self.assertIn("restarted", r.stderr)
        self.assertEqual(self.installed("station-manager"), OLD)

    def test_a_failed_install_does_not_start_a_daemon_that_was_not_running(self):
        self.given(daemon=OLD, active=False)
        self.flag("install-fails")
        r = self.deploy()
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertFalse(any("systemctl --user start" in c for c in self.calls()))
        self.assertFalse(self.active())

    def test_a_first_install_works(self):
        self.given()
        r = self.deploy()
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertEqual(self.installed("station-manager"), NEW)
        self.assertTrue(self.active())
        self.assertIn("systemctl --user enable smd", self.calls())


if __name__ == "__main__":
    unittest.main()
