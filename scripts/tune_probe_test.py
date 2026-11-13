"""Tests for scripts/tune-duration-probe.sh:  python3 -I scripts/tune_probe_test.py

W-0027 C2c: the probe asks smd through scripts/smd_client.py, so it keeps working with the
operating-client check on. The probe sends a tune request, so these tests first deny themselves
every TCP connection (scripts/confined.py): whatever the script under test does, the kernel
lets this process and everything it starts connect to nothing. The probe is run against a
recording stand-in for the client command (a `python3` first on PATH that writes down what it
was asked and prints a canned status). There is no listener, no smd, no rig and no transmitter
here: nothing is keyed.

What this file proves is what the probe asks for, in what order, and what it makes of the
answers. That the client command then reaches smd, verified and with the credential, is proved
where no tune script is executed: scripts/smd_client_test.py and scripts/smd_tools_smd_test.py.
The two halves meet at the command line, which ClientContract checks against the client's own
parser.

On 2026-11-13 this file's first run executed the then-current script, whose default address
was the station's: four tune requests reached the live daemon, which refused them all. The
confinement is what makes that impossible; the text check below is only an early diagnostic.
"""
import importlib.util
import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest
import unittest.mock

sys.dont_write_bytecode = True  # loading a sibling by path must not leave scripts/__pycache__
_here = os.path.dirname(os.path.abspath(__file__))


def _load(name):
    spec = importlib.util.spec_from_file_location(name, os.path.join(_here, name + ".py"))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


confined = _load("confined")
sc = _load("smd_client")
Confinement = confined.Confinement  # the evidence that this run can connect to nothing
SCRIPT = os.path.join(_here, "tune-duration-probe.sh")
CLIENT = os.path.join(_here, "smd_client.py")
VERSION, TUNE_ON, TUNE_OFF = "GET /v1/version", 'POST /v1/rig/tune --json {"active":true}', 'POST /v1/rig/tune --json {"active":false}'
_made = []

# The stand-in: records its arguments, prints the status the test chose. It opens no socket.
_STUB = """#!%s -I
import json, os, sys
args = sys.argv[1:]
with open(os.environ["PROBE_RECORD"], "a", encoding="utf-8") as f:
    f.write(json.dumps(args) + "\\n")
asked = " ".join(args[2:])
status = next((s for prefix, s in json.loads(os.environ["PROBE_ANSWERS"]) if asked.startswith(prefix)), "404")
print(status)
sys.exit(1 if status == "000" else 0)
""" % sys.executable


def tmp(prefix):
    d = tempfile.mkdtemp(prefix=prefix)
    _made.append(d)
    return d


def tearDownModule():
    for d in _made:  # exactly the directories this run made
        shutil.rmtree(d, ignore_errors=True)


def script_text_diagnostic(script=None):
    """An early, readable failure for the commonest mistake: a probe with an address of its
    own, or one that does not go through the client. It is a diagnostic and nothing more: a
    script can build an address this does not see (codex-114). What keeps a test from a real
    daemon is the confinement (confined.require)."""
    with open(script or SCRIPT, encoding="utf-8") as f:
        text = f.read()
    code = [l for l in text.splitlines() if not l.lstrip().startswith("#")]
    for suspicious in ("8080", "8443", "http://", "https://", "curl ", "localhost", "127.0.0.1"):
        hit = [l for l in code if suspicious in l]
        if hit:
            raise AssertionError(f"tune-duration-probe.sh contains {suspicious!r} outside a comment: "
                                 f"{hit[0].strip()}")
    if not any("smd_client.py" in l for l in code):
        raise AssertionError("tune-duration-probe.sh does not ask through smd_client.py")


def execute(argv, **kw):
    """The one place this file executes the probe: only inside the confinement."""
    confined.require()
    return subprocess.run(argv, capture_output=True, text=True, timeout=60, **kw)


def run(answer_prompt, answers, *, extra_env=None):
    """Run the probe once (0 seconds, 1 trial) against the stand-in. Returns the exit code, the
    output, and every command line the probe gave the client."""
    script_text_diagnostic()
    d = tmp("sm-probe-")
    stub = os.path.join(d, "bin", "python3")
    os.mkdir(os.path.dirname(stub))
    with open(stub, "w", encoding="utf-8") as f:
        f.write(_STUB)
    os.chmod(stub, 0o700)
    record, log = os.path.join(d, "asked"), os.path.join(d, "smd.log")
    for path in (record, log):
        open(path, "w").close()
    env = {"PATH": os.path.dirname(stub) + os.pathsep + os.environ["PATH"], "HOME": "/nonexistent",
           "SM_WORKING_DIR": os.path.join(d, "no-install"), "SM_LOG": log, "SM_SETTLE": "0",
           "PROBE_RECORD": record, "PROBE_ANSWERS": json.dumps(answers), **(extra_env or {})}
    p = execute(["bash", SCRIPT, "0", "1"], input=answer_prompt, env=env)
    with open(record, encoding="utf-8") as f:
        asked = [json.loads(line) for line in f]
    return p.returncode, p.stdout + p.stderr, asked


def requests(asked):
    return [" ".join(a[2:]) for a in asked]


SERVED = [[VERSION, "200"], ["POST /v1/rig/tune", "202"]]


class TextDiagnostic(unittest.TestCase):
    """The diagnostic, on text only: nothing is executed."""

    def flags(self, text):
        path = os.path.join(tmp("sm-probe-text-"), "probe.sh")
        with open(path, "w", encoding="utf-8") as f:
            f.write(text)
        with self.assertRaises(AssertionError):
            script_text_diagnostic(path)

    def test_it_names_a_script_with_an_address_of_its_own(self):
        ok = 'smd() { python3 -I "$HERE/smd_client.py" "$@"; }\n'
        self.flags('HOST="${SM_HOST:-127.0.0.1:8080}"\n' + ok)
        self.flags('curl -s "http://${HOST}/v1/rig/tune"\n' + ok)
        self.flags(ok + 'smd GET https://localhost/v1/version\n')
        self.flags('echo no client here\n')

    def test_the_script_in_the_tree_passes(self):
        script_text_diagnostic()


class ExecutionIsConfined(unittest.TestCase):
    """Nothing is executed unconfined, and confined, a script that does what the diagnostic
    cannot see still connects to nothing. These run stand-in scripts, never the probe."""

    def test_outside_the_confinement_nothing_is_executed(self):
        marker = os.path.join(tmp("sm-probe-exec-"), "ran")
        with unittest.mock.patch.object(confined, "inside", lambda: False), self.assertRaises(RuntimeError):
            execute(["sh", "-c", f'touch "{marker}"'])
        self.assertFalse(os.path.exists(marker), "something was executed outside the confinement")

    def test_a_script_the_diagnostic_cannot_see_through_still_connects_to_nothing(self):
        # codex-114's counterexample, aimed at the synthetic endpoints on the host's loopback:
        # the address is assembled so that no forbidden text appears, and it is run for real,
        # once at each address (codex-115: a rule that named only a port would let one through).
        script = os.path.join(tmp("sm-probe-evade-"), "probe.sh")
        with open(script, "w", encoding="utf-8") as f:
            f.write('# asks through smd_client.py, or says so\n'
                    'H=127.0.0.$(($1)); P=$2\n'
                    'smd() { python3 -I /nonexistent/smd_client.py "$@"; }\n'
                    'exec 3<>"/dev/tcp/$H/$P" && echo REACHED || echo refused\n')
        script_text_diagnostic(script)  # the text check accepts it: that is the point
        for host, port in confined.outside():
            p = execute(["bash", script, host.rsplit(".", 1)[1], str(port)])
            self.assertIn("refused", p.stdout + p.stderr, host)
            self.assertNotIn("REACHED", p.stdout, host)
        self.assertEqual(confined.hits(), 0)


class TuneProbe(unittest.TestCase):
    def test_a_run_checks_the_daemon_then_tunes_on_and_off_through_the_client(self):
        code, out, asked = run("y\n", SERVED)
        self.assertEqual(code, 0, out)
        self.assertIn("clean", out)
        self.assertIn("RESULT: 0 of 1 trials stuck", out)
        self.assertEqual(requests(asked), [VERSION, TUNE_ON, TUNE_OFF])
        # Every ask goes through the client in the tree, in Python's isolated mode.
        self.assertEqual([a[:2] for a in asked], [["-I", CLIENT]] * 3)

    def test_declining_the_prompt_sends_no_tune(self):
        code, out, asked = run("n\n", SERVED)
        self.assertEqual(code, 0, out)
        self.assertIn("aborted", out)
        self.assertEqual(requests(asked), [VERSION])

    def test_a_daemon_that_was_not_reached_is_sent_nothing_more(self):
        # 000 is what the client prints for no answer, an impostor included (smd_client_test).
        code, out, asked = run("y\n", [[VERSION, "000"], ["POST /v1/rig/tune", "202"]])
        self.assertEqual(code, 1, out)
        self.assertIn("not reachable", out)
        self.assertIn("got 000", out)
        self.assertEqual(requests(asked), [VERSION])

    def test_a_refused_start_is_not_a_result(self):
        code, out, asked = run("y\n", [[VERSION, "200"], ["POST /v1/rig/tune", "409"]])
        self.assertIn("NOT STARTED", out)
        self.assertIn("RESULT: 0 of 0 trials stuck", out)
        self.assertEqual(requests(asked), [VERSION, TUNE_ON])

    def test_another_install_is_named_with_sm_config(self):
        code, out, asked = run("n\n", SERVED, extra_env={"SM_CONFIG": "/nonexistent/other/config.json"})
        self.assertEqual(code, 0, out)
        self.assertEqual(requests(asked), [VERSION + " --sm-config /nonexistent/other/config.json"])

    def test_the_old_address_variable_is_refused_not_ignored(self):
        code, out, asked = run("y\n", SERVED, extra_env={"SM_HOST": "127.0.0.1:8080"})
        self.assertEqual(code, 2, out)
        self.assertIn("SM_HOST", out)
        self.assertEqual(asked, [])


class ClientContract(unittest.TestCase):
    """What the probe passes is a command line the real client accepts and means the same by.
    Parsed only: nothing is sent."""

    def test_every_command_line_the_probe_gives_is_one_the_client_takes(self):
        _, _, asked = run("y\n", SERVED, extra_env={"SM_CONFIG": "/nonexistent/other/config.json"})
        self.assertEqual(len(asked), 3)
        self.assertTrue(os.path.isfile(CLIENT))
        got = []
        for argv in asked:
            self.assertEqual(argv[:2], ["-I", CLIENT])
            a = sc.parser().parse_args(argv[2:])
            self.assertEqual(a.sm_config, "/nonexistent/other/config.json")
            got.append((a.method, a.path, None if a.json is None else json.loads(a.json)))
        self.assertEqual(got, [("GET", "/v1/version", None), ("POST", "/v1/rig/tune", {"active": True}),
                               ("POST", "/v1/rig/tune", {"active": False})])


if __name__ == "__main__":
    confined.main()
