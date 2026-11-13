"""The operator's tools against a REAL smd with its operating-client check on (W-0027 C2c).

    SM_DESKTOP_SMD=build/bin/smd python3 -I scripts/smd_tools_smd_test.py

Never the station: smd is the binary named by SM_DESKTOP_SMD, started with --config only, in a
temporary SM_WORKING_DIR and HOME, a synthetic config with a placeholder callsign, free
loopback ports, and the rig, FT8 and evidence capture all disabled.

Nothing here can send a rig command, and that is by what is run, not by a network boundary:
this file does not execute the tune probe and names no rig route (the probe's own tests run
with every TCP connection denied: scripts/tune_probe_test.py). It runs the client and the
Cabrillo reader, which only read, against the temporary smd, as the desktop app's real-smd
checks do. It is not network-confined, since it has to reach that smd. Skipped when
SM_DESKTOP_SMD is not set.
"""
import importlib.util
import io
import json
import os
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import time
import unittest
import unittest.mock
import urllib.error
import urllib.request
from contextlib import redirect_stderr, redirect_stdout

sys.dont_write_bytecode = True  # loading a sibling by path must not leave scripts/__pycache__
_here = os.path.dirname(os.path.abspath(__file__))


def _load(name, filename):
    spec = importlib.util.spec_from_file_location(name, os.path.join(_here, filename))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


sc = _load("smd_client", "smd_client.py")
cab = _load("cab", "ukeicc-cabrillo.py")
SMD = os.environ.get("SM_DESKTOP_SMD", "")


def free_port():
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


def accepts(port):
    try:
        with socket.create_connection(("127.0.0.1", port), timeout=0.5):
            return True
    except OSError:
        return False


@unittest.skipUnless(SMD, "set SM_DESKTOP_SMD to an smd binary to run the tools against it")
class ToolsAgainstSmd(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.base = tempfile.mkdtemp(prefix="sm-tools-smd-")
        cls.wd = os.path.join(cls.base, "wd")
        os.mkdir(cls.wd, 0o700)
        cls.browser_port, cls.app_port = free_port(), free_port()
        cfg = {
            "version": 6,
            "socket_path": f"127.0.0.1:{cls.browser_port}",
            "server": {"protocol": "tcp", "serve_spa": True, "app_port": cls.app_port,
                       "require_desktop_app": True},
            "datastore": {"driver": "sqlite", "path": os.path.join(cls.wd, "db", "station-manager.db")},
            "setup_complete": True,
            "logging_station": {"station_callsign": "N0CALL"},  # a placeholder, never a real station
            "bridge": {"enabled": False},
            "ft8": {"enabled": False},
            "evidence": {"capture": False},
        }
        cls.config = os.path.join(cls.wd, "config.json")
        with open(os.open(cls.config, os.O_WRONLY | os.O_CREAT, 0o600), "w") as f:
            json.dump(cfg, f)
        # An explicit environment: nothing of the caller's, so nothing of the station's.
        cls.env = {"PATH": os.environ["PATH"], "HOME": cls.base, "SM_WORKING_DIR": cls.wd}
        cls.log = open(os.path.join(cls.base, "smd.out"), "w")
        cls.proc = subprocess.Popen([os.path.abspath(SMD), "--config", cls.config], env=cls.env,
                                    stdout=cls.log, stderr=subprocess.STDOUT, cwd=cls.base)
        deadline = time.time() + 30
        while time.time() < deadline and not (accepts(cls.browser_port) and accepts(cls.app_port)):
            if cls.proc.poll() is not None:
                break
            time.sleep(0.2)
        if cls.proc.poll() is not None or not accepts(cls.app_port):
            cls.tearDownClass()
            raise RuntimeError("the temporary smd did not start both listeners")

    @classmethod
    def tearDownClass(cls):
        if cls.proc.poll() is None:
            cls.proc.send_signal(signal.SIGTERM)
            try:
                cls.proc.wait(timeout=20)
            except subprocess.TimeoutExpired:
                cls.proc.kill()
                cls.proc.wait()
        cls.log.close()
        shutil.rmtree(cls.base, ignore_errors=True)  # exactly the directory this run made

    def test_the_check_is_on_a_plain_client_is_refused_and_a_page_gets_the_notice(self):
        def plain(path):
            try:
                with urllib.request.urlopen(f"http://127.0.0.1:{self.browser_port}{path}", timeout=5) as r:
                    return r.status, r.headers.get("Content-Type", ""), r.read().decode()
            except urllib.error.HTTPError as e:
                with e:
                    return e.code, e.headers.get("Content-Type", ""), e.read().decode()
        status, _, body = plain("/v1/version")
        self.assertEqual((status, json.loads(body)["code"]), (403, "desktop_app_required"))
        status, ctype, body = plain("/")
        self.assertEqual((status, ctype), (403, "text/html; charset=utf-8"))
        self.assertTrue(body.startswith("<!doctype html>"), body[:60])
        self.assertIn("desktop app", body)
        self.assertNotIn("<script", body)
        self.assertEqual(plain("/v1/healthz")[0], 200)

    def test_the_client_is_served_with_the_credential_smd_made(self):
        client = sc.local_client(env=self.env)
        status, body = client.request("GET", "/v1/version")
        self.assertEqual(status, 200, body)
        self.assertIn("daemon", json.loads(body))
        self.assertEqual(client.request("GET", "/v1/logbook")[0], 200)

    def test_the_command_prints_200(self):
        p = subprocess.run([sys.executable, "-I", os.path.join(_here, "smd_client.py"), "GET", "/v1/version"],
                           env=self.env, capture_output=True, text=True, timeout=30)
        self.assertEqual((p.returncode, p.stdout.strip()), (0, "200"), p.stderr)

    def test_the_cabrillo_reader_reads_the_logbook_through_the_check(self):
        out = os.path.join(self.base, "test.cbr")
        err = io.StringIO()
        with unittest.mock.patch.dict(os.environ, self.env, clear=True), redirect_stdout(io.StringIO()), redirect_stderr(err):
            code = cab.main(["--from-daemon", "-o", out, "--contest", "UKEIDXSSB", "--callsign", "N0CALL",
                             "--start", "2026-10-31T12:00", "--power", "LOW", "--assisted", "NON-ASSISTED",
                             "--time", "24-HOURS"])
        text = err.getvalue()
        # A new install has an empty default logbook: the reader got through the check, read
        # it, and wrote a Cabrillo with no QSO lines. A transport or refusal error would say so.
        self.assertNotIn("could not read the contacts", text)
        self.assertNotIn("desktop app", text)
        self.assertEqual(code, 0, text)
        self.assertIn("0 QSO line(s)", text)


if __name__ == "__main__":
    unittest.main()
