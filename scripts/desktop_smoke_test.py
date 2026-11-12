"""Tests for the production smoke check's verdict (scripts/desktop-smoke.py; W-0027 D1 part 3).

    python3 -I scripts/desktop_smoke_test.py

Synthetic observations only: a valid run, then one fault per rule, each of which must be
reported. A missing, vanished, unrelated or uninspectable process fails; nothing skips. The
launches themselves are proved by running the script on the built RPM, and its reversions.
"""
import copy
import importlib.util
import contextlib
import io
import os
import shutil
import stat
import sys
import tempfile
import threading
import time
import unittest

sys.dont_write_bytecode = True  # no __pycache__ beside the scripts

_here = os.path.dirname(os.path.abspath(__file__))
_spec = importlib.util.spec_from_file_location("smoke", os.path.join(_here, "desktop-smoke.py"))
smoke = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(smoke)

BIN = "/x/root/usr/lib/station-manager-desktop/station-manager-desktop"
P = "station-manager-desktop: "
TRACE = (P + "waiting: smd has not created its identity yet /x/run/wd/identity\n"
         + "[123:ERROR:dbus] Failed to connect to the bus\n"
         + P + "loaded waiting page renderer 40\n")
HOST_USER, HOST_PID = "user:[4026531837]", "pid:[4026531836]"


def proc(ppid, argv, seccomp=0, user=HOST_USER, pid=HOST_PID):
    return {"ppid": ppid, "argv": argv, "seccomp": seccomp, "ns_user": user, "ns_pid": pid}


def valid():
    return {
        "binary": BIN,
        "trace": TRACE,
        "main": 10,
        "procs": {
            10: proc(9, [BIN]),
            20: proc(10, [BIN, "--type=zygote", "--no-zygote-sandbox"]),
            30: proc(20, [BIN, "--type=zygote"], seccomp=0, user="user:[1]", pid="pid:[2]"),
            40: proc(30, [BIN, "--type=renderer", "--enable-sandbox"], seccomp=2,
                     user="user:[1]", pid="pid:[2]"),
        },
        "alive": True,
        "config_unchanged": True,
        "hostile_exists": False,
        "profile_exists": True,
        "leftover": [],
    }


class Verdict(unittest.TestCase):
    def assertReported(self, run, needle):
        p = smoke.judge(run)
        self.assertTrue(any(needle in x for x in p), f"not reported ({needle!r}): {p}")

    def test_valid_run_passes(self):
        self.assertEqual(smoke.judge(valid()), [])

    def test_the_trace_must_report_waiting(self):
        r = valid()
        r["trace"] = P + "loaded waiting page renderer 40\n"
        self.assertReported(r, "waiting:")

    def test_the_waiting_page_must_have_loaded(self):
        r = valid()
        r["trace"] = P + "waiting: x y\n"
        self.assertReported(r, "loaded waiting page")

    def test_a_later_other_document_fails(self):
        r = valid()
        r["trace"] = TRACE + P + "loaded other renderer 40\n"
        self.assertReported(r, "last load")

    def test_only_prefixed_lines_at_line_start_count(self):
        r = valid()
        r["trace"] = (P + "waiting: x y\n" + "console: " + P + "loaded waiting page renderer 40\n")
        self.assertReported(r, "loaded waiting page")

    def test_trace_lines_are_only_those_starting_with_the_prefix(self):
        text = ("console: " + P + "loaded waiting page renderer 40\n" + P + "waiting: a b\n"
                + "  " + P + "renderer gone crashed\n")
        self.assertEqual(smoke._trace(text), ["waiting: a b"])

    def test_load_failures_and_lost_renderers_fail(self):
        for line, needle in [("load failed waiting page -6", "load failed"),
                             ("renderer gone crashed", "renderer gone")]:
            r = valid()
            r["trace"] = TRACE + P + line + "\n"
            self.assertReported(r, needle)

    def test_the_named_renderer_is_the_last_waiting_load(self):
        r = valid()
        r["trace"] = (P + "waiting: x y\n" + P + "loaded waiting page renderer 99\n"
                      + P + "loaded waiting page renderer 40\n")
        self.assertEqual(smoke.judge(r), [])  # 99 is not inspected: 40 is named last
        r["trace"] = (P + "waiting: x y\n" + P + "loaded waiting page renderer 40\n"
                      + P + "loaded waiting page renderer 99\n")
        self.assertReported(r, "99")

    def test_a_vanished_or_uninspectable_renderer_fails(self):
        r = valid()
        del r["procs"][40]
        self.assertReported(r, "renderer 40")

    def test_the_renderer_must_be_a_renderer(self):
        r = valid()
        r["procs"][40]["argv"] = [BIN, "--type=gpu-process"]
        self.assertReported(r, "--type=renderer")

    def test_the_renderer_must_descend_from_this_launch(self):
        r = valid()
        r["procs"][40]["ppid"] = 1  # unrelated: reparented or another launch
        self.assertReported(r, "descendant")
        r = valid()
        del r["procs"][30]  # an uninspectable intermediate: never assumed
        self.assertReported(r, "descendant")
        r = valid()
        r["procs"][20]["ppid"] = 40  # a cycle: must end, not hang
        self.assertReported(r, "descendant")

    def test_the_renderer_must_run_this_binary(self):
        r = valid()
        r["procs"][40]["argv"] = ["/usr/lib/chromium/chromium", "--type=renderer"]
        self.assertReported(r, "binary")

    def test_seccomp_filter_mode(self):
        for value in (0, 1, None):
            r = valid()
            r["procs"][40]["seccomp"] = value
            self.assertReported(r, "Seccomp")

    def test_namespaces_differ_from_the_main_process(self):
        for key, needle in [("ns_user", "user namespace"), ("ns_pid", "PID namespace")]:
            r = valid()
            r["procs"][40][key] = r["procs"][10][key]
            self.assertReported(r, needle)
            r = valid()
            r["procs"][40][key] = None
            self.assertReported(r, needle)

    def test_the_main_process_is_this_binary_and_inspectable(self):
        r = valid()
        del r["procs"][10]
        self.assertReported(r, "main process")
        r = valid()
        r["procs"][10]["argv"] = ["/usr/bin/electron"]
        self.assertReported(r, "main process")

    def test_outcomes(self):
        for key, value, needle in [("alive", False, "not running"),
                                   ("config_unchanged", False, "config"),
                                   ("hostile_exists", True, "hostile"),
                                   ("profile_exists", False, "profile"),
                                   ("leftover", [41], "left running")]:
            r = valid()
            r[key] = value
            self.assertReported(r, needle)

    def test_cmdline_forms(self):
        # A real process: NUL-separated. A Chromium child: its title rewritten as one
        # space-joined string, measured on this host (2026-11-12).
        self.assertEqual(smoke.split_cmdline(b"/x/bin\0--type=zygote\0"), ["/x/bin", "--type=zygote"])
        self.assertEqual(smoke.split_cmdline(b"/x/bin --type=renderer --enable-sandbox"),
                         ["/x/bin", "--type=renderer", "--enable-sandbox"])
        self.assertEqual(smoke.split_cmdline(b"/x/bin\0"), ["/x/bin"])
        self.assertEqual(smoke.split_cmdline(b""), [])

    def test_mutations_do_not_leak_between_cases(self):
        a = valid()
        b = copy.deepcopy(a)
        a["procs"][40]["seccomp"] = 0
        self.assertEqual(smoke.judge(b), [])



def _raised(fn):
    try:
        fn()
    except BaseException as e:  # noqa: BLE001 -- the test inspects whatever was raised
        return e
    return None


def _fds():
    return len(os.listdir("/proc/self/fd"))


class XvfbStartup(unittest.TestCase):
    """codex-081: the start is bounded, and every unsuccessful start leaves nothing behind."""

    def py(self, code):
        return lambda fd: [sys.executable, "-I", "-c", code.replace("FD", str(fd))]

    def owned(self, x):
        # Whatever the code under test does, the synthetic server this test made is stopped.
        def stop():
            if x.proc is not None and x.proc.poll() is None:
                x.proc.kill()
                x.proc.wait(5)
        self.addCleanup(stop)
        return x

    def test_a_reported_display_is_used_and_the_server_reaped(self):
        x = self.owned(smoke.Xvfb(cmd=self.py("import os,time; os.write(FD, b'97\\n'); time.sleep(60)"),
                       timeout=5))
        with x:
            self.assertEqual(x.display, ":97")
            self.assertIsNone(x.proc.poll())
        self.assertIsNotNone(x.proc.poll())

    def test_a_live_but_silent_server_times_out_and_is_reaped(self):
        before = _fds()
        x = self.owned(smoke.Xvfb(cmd=self.py("import time; time.sleep(60)"), timeout=1))
        # In a thread, so an unbounded start fails here instead of hanging the suite; the
        # cleanup kills the server, which ends a blocked read.
        got = []
        t = threading.Thread(target=lambda: got.append(_raised(x.__enter__)), daemon=True)
        t.start()
        t.join(5)
        self.assertFalse(t.is_alive(), "the start was not bounded: still waiting after 5 s")
        self.assertIsInstance(got[0], RuntimeError)
        self.assertIsNotNone(x.proc.poll(), "the silent server was left running")
        self.assertEqual(_fds(), before, "a descriptor was left open")

    def test_a_server_that_exits_without_a_display_is_reported(self):
        before = _fds()
        x = self.owned(smoke.Xvfb(cmd=self.py("pass"), timeout=5))
        with self.assertRaises(RuntimeError):
            x.__enter__()
        self.assertIsNotNone(x.proc.poll())
        self.assertEqual(_fds(), before)

    def test_a_start_exception_closes_its_descriptors(self):
        before = _fds()
        x = self.owned(smoke.Xvfb(cmd=lambda fd: ["/nonexistent/sm-smoke-Xvfb"], timeout=5))
        with self.assertRaises(OSError):
            x.__enter__()
        self.assertEqual(_fds(), before)


class FakeXvfb:
    display = ":0"

    def __enter__(self):
        return self

    def __exit__(self, *exc):
        return False


class KeepOnException(unittest.TestCase):
    """codex-081: an exception keeps the run directory (its evidence, and anything whose
    termination is unconfirmed), reports it, and still fails; only success removes it."""

    def setUp(self):
        self.saved = {k: getattr(smoke, k) for k in ("unpack", "Xvfb", "launch", "reap")}
        self.addCleanup(lambda: [setattr(smoke, k, v) for k, v in self.saved.items()])
        self.work = None
        smoke.Xvfb = FakeXvfb

    def remember(self, dest):
        self.work = os.path.dirname(dest)
        self.addCleanup(shutil.rmtree, self.work, True)  # exactly the directory main made
        os.makedirs(dest)
        with open(os.path.join(self.work, "evidence.log"), "w") as fh:
            fh.write("synthetic evidence\n")

    def run_main(self, *extra):
        err = io.StringIO()
        with contextlib.redirect_stderr(err), contextlib.redirect_stdout(io.StringIO()):
            with self.assertRaises(RuntimeError):
                smoke.main(["unused.rpm", "--selectors", "a1", *extra])
        return err.getvalue()

    def assertKept(self, err):
        self.assertIsNotNone(self.work)
        self.assertTrue(os.path.isfile(os.path.join(self.work, "evidence.log")),
                        "the run directory was removed on an exception")
        self.assertIn(self.work, err, "the kept directory was not reported")

    def test_an_unpack_failure_keeps_the_run_directory(self):
        def unpack(rpm, dest):
            self.remember(dest)
            raise RuntimeError("synthetic unpack failure")
        smoke.unpack = unpack
        self.assertKept(self.run_main())

    def test_an_observation_failure_keeps_the_run_directory(self):
        smoke.unpack = lambda rpm, dest: self.remember(dest)

        def launch(*a, **k):
            raise RuntimeError("synthetic observation failure")
        smoke.launch = launch
        self.assertKept(self.run_main())

    def test_a_reap_failure_keeps_the_run_directory_and_the_child_is_gone(self):
        # A synthetic "runtime" (a shell sleeping), launched by the real launch(); the real
        # reap stops it, then the wrapper fails, as an unconfirmed cleanup would.
        def unpack(rpm, dest):
            self.remember(dest)
            lib = os.path.join(dest, smoke.LIB)
            os.makedirs(lib)
            b = os.path.join(lib, smoke.NAME)
            with open(b, "w") as fh:
                fh.write("#!/bin/sh\nexec /usr/bin/sleep 30\n")
            os.chmod(b, stat.S_IRWXU)
        smoke.unpack = unpack
        procs = []
        real_reap = self.saved["reap"]

        def reap(proc, root):
            procs.append(proc)
            real_reap(proc, root)
            raise RuntimeError("synthetic reap failure")
        smoke.reap = reap
        saved_deadline = smoke.DEADLINE_S
        smoke.DEADLINE_S = smoke.SETTLE_S + 1
        self.addCleanup(setattr, smoke, "DEADLINE_S", saved_deadline)
        self.assertKept(self.run_main())
        self.assertEqual(len(procs), 1)
        self.assertIsNotNone(procs[0].poll(), "the synthetic child was left running")

    def test_success_removes_the_run_directory(self):
        smoke.unpack = lambda rpm, dest: self.remember(dest)
        smoke.launch = lambda *a, **k: {**valid(), "log": "-"}
        smoke.judge = lambda run: []
        self.addCleanup(setattr, smoke, "judge", Verdict.judge_saved)
        with contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(smoke.main(["unused.rpm", "--selectors", "a1"]), 0)
        self.assertFalse(os.path.exists(self.work), "a successful run left its directory")

    def test_a_passing_run_with_unconfirmed_termination_is_kept(self):
        smoke.unpack = lambda rpm, dest: self.remember(dest)
        smoke.launch = lambda *a, **k: {**valid(), "log": "-"}
        smoke.judge = lambda run: []
        self.addCleanup(setattr, smoke, "judge", Verdict.judge_saved)
        saved_owned = smoke.owned
        smoke.owned = lambda root: [4242]  # a process of this root still listed
        self.addCleanup(setattr, smoke, "owned", saved_owned)
        err = io.StringIO()
        with contextlib.redirect_stderr(err), contextlib.redirect_stdout(io.StringIO()):
            rc = smoke.main(["unused.rpm", "--selectors", "a1"])
        self.assertNotEqual(rc, 0, "unconfirmed termination returned success (codex-082)")
        self.assertKept(err.getvalue())
        self.assertIn("4242", err.getvalue(), "the surviving process was not named")


Verdict.judge_saved = smoke.judge


if __name__ == "__main__":
    unittest.main()
